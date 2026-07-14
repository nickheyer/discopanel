package app.discopanel.agent;

import app.discopanel.agent.proto.AgentProto;

import java.io.DataOutputStream;
import java.lang.instrument.Instrumentation;
import java.lang.reflect.Method;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.security.CodeSource;
import java.security.ProtectionDomain;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

// Encodes live throwables into structured fatal error reports
final class FatalErrors {
    private static final int MAX_CAUSES = 8;
    private static final int MAX_FRAMES = 32;
    private static final int MAX_FAILED_MODS = 32;
    private static final int SOCKET_TIMEOUT_MS = 2000;
    private static final long LOCATIONS_TTL_MS = 10000;

    private static final Object locationsLock = new Object();
    private static Map<String, String> cachedLocations;
    private static long cachedLocationsAt;

    private FatalErrors() {
    }

    static void installUncaughtHandler(Instrumentation inst, int port) {
        Thread.UncaughtExceptionHandler previous = Thread.getDefaultUncaughtExceptionHandler();
        Thread.setDefaultUncaughtExceptionHandler(new UncaughtReporter(inst, port, previous));
    }

    private static final class UncaughtReporter implements Thread.UncaughtExceptionHandler {
        private static final int MAX_REPORTS = 4;

        private final Instrumentation inst;
        private final int port;
        private final Thread.UncaughtExceptionHandler previous;
        private final AtomicInteger reports = new AtomicInteger();

        UncaughtReporter(Instrumentation inst, int port, Thread.UncaughtExceptionHandler previous) {
            this.inst = inst;
            this.port = port;
            this.previous = previous;
        }

        @Override
        public void uncaughtException(Thread thread, Throwable error) {
            try {
                if (reports.incrementAndGet() <= MAX_REPORTS) {
                    send(port, build(inst, thread.getName(), error, true));
                }
            } catch (Throwable ignored) {
            }
            delegate(thread, error);
        }

        private void delegate(Thread thread, Throwable error) {
            if (previous != null) {
                try {
                    previous.uncaughtException(thread, error);
                    return;
                } catch (Throwable ignored) {
                }
            }
            System.err.print("Exception in thread \"" + thread.getName() + "\" ");
            error.printStackTrace();
        }
    }

    static AgentProto.FatalError build(Instrumentation inst, String thread, Throwable error, boolean uncaught) {
        Map<String, String> locations = classLocations(inst);
        AgentProto.FatalError.Builder fatal = AgentProto.FatalError.newBuilder()
                .setThread(thread == null ? "" : thread)
                .setUncaught(uncaught);

        Set<Throwable> seen = Collections.newSetFromMap(new IdentityHashMap<Throwable, Boolean>());
        Throwable cause = error;
        while (cause != null && seen.add(cause) && fatal.getCausesCount() < MAX_CAUSES) {
            AgentProto.CrashCause.Builder cb = AgentProto.CrashCause.newBuilder()
                    .setType(cause.getClass().getName());
            if (cause.getMessage() != null) {
                cb.setMessage(cause.getMessage());
            }
            StackTraceElement[] frames = cause.getStackTrace();
            for (int i = 0; i < frames.length && i < MAX_FRAMES; i++) {
                StackTraceElement f = frames[i];
                AgentProto.CrashFrame.Builder fb = AgentProto.CrashFrame.newBuilder()
                        .setClassName(f.getClassName())
                        .setMethodName(f.getMethodName())
                        .setLine(f.getLineNumber());
                if (f.getFileName() != null) {
                    fb.setFileName(f.getFileName());
                }
                String location = locations.get(f.getClassName());
                if (location != null) {
                    fb.setSourceLocation(location);
                }
                cb.addFrames(fb);
            }
            fatal.addCauses(cb);
            cause = cause.getCause();
        }

        for (AgentProto.FailedMod mod : failedMods(error)) {
            fatal.addFailedMods(mod);
        }
        return fatal.build();
    }

    /** Reads the loader's per-mod failure list off the exception object */
    static List<AgentProto.FailedMod> failedMods(Throwable error) {
        List<AgentProto.FailedMod> mods = new ArrayList<AgentProto.FailedMod>();
        Set<Throwable> seen = Collections.newSetFromMap(new IdentityHashMap<Throwable, Boolean>());
        Throwable cause = error;
        while (cause != null && seen.add(cause)) {
            for (Object issue : loaderIssues(cause)) {
                if (mods.size() >= MAX_FAILED_MODS) {
                    return mods;
                }
                AgentProto.FailedMod mod = failedModOf(issue);
                if (mod != null) {
                    mods.add(mod);
                }
            }
            if (mods.isEmpty()) {
                fabricFailedMods(cause, mods);
            }
            if (mods.isEmpty()) {
                fabricEntrypointFailure(cause, mods);
            }
            if (!mods.isEmpty()) {
                return mods;
            }
            cause = cause.getCause();
        }
        return mods;
    }

    /** Forge exposes getErrors, NeoForge getIssues, both are collections */
    private static List<Object> loaderIssues(Throwable error) {
        String[] accessors = {"getErrors", "getIssues"};
        for (String name : accessors) {
            Object value = call(error, name);
            if (value instanceof Collection) {
                return new ArrayList<Object>((Collection<?>) value);
            }
        }
        return Collections.emptyList();
    }

    /** Quoted display name then parenthesized id, fabric's mod reference */
    private static final Pattern FABRIC_MOD_REF = Pattern.compile("'([^']+)' \\(([a-z][a-z0-9_-]*)\\)");

    /** Fabric resolution failures carry their result in the message */
    private static void fabricFailedMods(Throwable error, List<AgentProto.FailedMod> mods) {
        String type = error.getClass().getName();
        if (!type.endsWith("ModResolutionException") && !type.endsWith("ModSolvingException")) {
            return;
        }
        String message = error.getMessage();
        if (message == null) {
            return;
        }
        Set<String> seenIds = new HashSet<String>();
        for (String rawLine : message.split("\n")) {
            if (mods.size() >= MAX_FAILED_MODS) {
                return;
            }
            String line = rawLine.trim();
            while (line.startsWith("-") || line.startsWith("\t")) {
                line = line.substring(1).trim();
            }
            if (!isFabricIssueLine(line)) {
                continue;
            }
            Matcher m = FABRIC_MOD_REF.matcher(line);
            if (!m.find()) {
                continue;
            }
            String modId = m.group(2);
            if (!seenIds.add(modId)) {
                continue;
            }
            AgentProto.FailedMod.Builder mod = AgentProto.FailedMod.newBuilder()
                    .setModId(modId)
                    .setErrorType(type)
                    .setErrorMessage(line);
            if (isFabricMissingDep(line)) {
                mod.setReason("fabric.modresolution.missingdependency");
            }
            while (m.find() && mod.getReasonArgsCount() < MAX_REASON_ARGS) {
                mod.addReasonArgs(m.group(2));
            }
            mods.add(mod.build());
        }
    }

    /** Providing mod id quoted inside the entrypoint wrap */
    private static final Pattern FABRIC_ENTRYPOINT_PROVIDER = Pattern.compile("provided by '([a-z][a-z0-9_-]*)'");

    /** Entrypoint wrap messages blame the providing mod */
    private static void fabricEntrypointFailure(Throwable error, List<AgentProto.FailedMod> mods) {
        String message = error.getMessage();
        if (message == null || !message.startsWith("Could not execute entrypoint stage")) {
            return;
        }
        Matcher m = FABRIC_ENTRYPOINT_PROVIDER.matcher(message);
        if (!m.find()) {
            return;
        }
        mods.add(AgentProto.FailedMod.newBuilder()
                .setModId(m.group(1))
                .setReason("fabric.entrypoint")
                .setErrorType(error.getClass().getName())
                .setErrorMessage(message)
                .build());
    }

    /** Dependency relation lines blame their first named mod */
    private static boolean isFabricIssueLine(String line) {
        return line.contains(" requires ") || line.contains(" is incompatible with ")
                || line.contains(" breaks ") || line.contains(" conflicts with ")
                || line.contains(" depends on ");
    }

    /** Missing and unmatched dependency phrasings from the fabric result */
    private static boolean isFabricMissingDep(String line) {
        return line.contains("which is missing") || line.contains("is not present")
                || line.contains("wrong version is present");
    }

    /** Pulls mod id and owning jar out of one loader issue */
    private static AgentProto.FailedMod failedModOf(Object issue) {
        if (issue == null || isWarning(issue)) {
            return null;
        }
        Object modInfo = firstOf(issue, "getModInfo", "getAffectedMod", "affectedMod");
        Object modId = call(modInfo, "getModId");
        Object owningFile = call(modInfo, "getOwningFile");
        Object modFile = call(owningFile, "getFile");
        if (modFile == null) {
            modFile = firstOf(issue, "affectedModFile", "getAffectedModFile");
        }
        Object fileName = call(modFile, "getFileName");

        if (!(modId instanceof String) && !(fileName instanceof String)) {
            return null;
        }
        AgentProto.FailedMod.Builder mod = AgentProto.FailedMod.newBuilder();
        if (modId instanceof String) {
            mod.setModId((String) modId);
        }
        if (fileName instanceof String) {
            mod.setFileName((String) fileName);
        }

        // Forge speaks getI18NMessage, NeoForge translationKey
        Object reason = firstOf(issue, "translationKey", "getI18NMessage");
        if (reason instanceof String) {
            mod.setReason((String) reason);
        }
        for (String arg : reasonArgs(issue)) {
            mod.addReasonArgs(arg);
        }

        Throwable failure = issue instanceof Throwable ? (Throwable) issue : asThrowable(firstOf(issue, "cause", "getCause"));
        if (failure != null) {
            mod.setErrorType(failure.getClass().getName());
        }
        Object message = firstOf(issue, "getCleanMessage", "getMessage");
        if (!(message instanceof String) && failure != null) {
            message = failure.getMessage();
        }
        if (message instanceof String) {
            mod.setErrorMessage((String) message);
        }
        return mod.build();
    }

    /** NeoForge issue lists mix warnings in with errors */
    private static boolean isWarning(Object issue) {
        Object severity = firstOf(issue, "severity", "getSeverity");
        return severity != null && "WARNING".equals(String.valueOf(severity));
    }

    private static final int MAX_REASON_ARGS = 8;
    private static final int MAX_REASON_ARG_LEN = 160;

    /** Forge speaks getContext, NeoForge translationArgs */
    private static List<String> reasonArgs(Object issue) {
        List<String> args = new ArrayList<String>();
        Object raw = firstOf(issue, "translationArgs", "getContext");
        Object[] values;
        if (raw instanceof Collection) {
            values = ((Collection<?>) raw).toArray();
        } else if (raw instanceof Object[]) {
            values = (Object[]) raw;
        } else {
            return args;
        }
        for (Object value : values) {
            if (args.size() >= MAX_REASON_ARGS) {
                break;
            }
            if (value == null) {
                continue;
            }
            String text;
            try {
                text = String.valueOf(value);
            } catch (Throwable ignored) {
                continue;
            }
            if (text.length() > MAX_REASON_ARG_LEN) {
                text = text.substring(0, MAX_REASON_ARG_LEN);
            }
            args.add(text);
        }
        return args;
    }

    private static Throwable asThrowable(Object value) {
        return value instanceof Throwable ? (Throwable) value : null;
    }

    private static Object firstOf(Object target, String... names) {
        for (String name : names) {
            Object value = call(target, name);
            if (value != null) {
                return value;
            }
        }
        return null;
    }

    /** Invokes a public no-arg method, null on any failure */
    private static Object call(Object target, String name) {
        if (target == null) {
            return null;
        }
        try {
            Method m = target.getClass().getMethod(name);
            try {
                m.setAccessible(true);
            } catch (Throwable ignored) {
                // Module may refuse, public invoke can still work
            }
            Object value = m.invoke(target);
            if (value instanceof Optional) {
                return ((Optional<?>) value).orElse(null);
            }
            return value;
        } catch (Throwable ignored) {
            return null;
        }
    }

    /** Maps every loaded class name to its CodeSource URL */
    private static Map<String, String> classLocations(Instrumentation inst) {
        if (inst == null) {
            return Collections.emptyMap();
        }
        synchronized (locationsLock) {
            long now = System.currentTimeMillis();
            if (cachedLocations != null && now - cachedLocationsAt < LOCATIONS_TTL_MS) {
                return cachedLocations;
            }
            Map<String, String> locations = new HashMap<String, String>();
            for (Class<?> clazz : inst.getAllLoadedClasses()) {
                try {
                    if (clazz.isArray() || clazz.isPrimitive()) {
                        continue;
                    }
                    ProtectionDomain domain = clazz.getProtectionDomain();
                    if (domain == null) {
                        continue;
                    }
                    CodeSource source = domain.getCodeSource();
                    if (source == null || source.getLocation() == null) {
                        continue;
                    }
                    String name = clazz.getName();
                    if (!locations.containsKey(name)) {
                        locations.put(name, source.getLocation().toString());
                    }
                } catch (Throwable ignored) {
                    // Exotic classes may refuse introspection
                }
            }
            cachedLocations = locations;
            cachedLocationsAt = now;
            return locations;
        }
    }

    /** Dedicated blocking socket, survives a dying telemetry thread */
    static void send(int port, AgentProto.FatalError fatal) throws Exception {
        sendMessage(port, AgentProto.AgentMessage.newBuilder().setFatalError(fatal).build());
    }

    /** Writes one framed message over a fresh loopback socket */
    static void sendMessage(int port, AgentProto.AgentMessage msg) throws Exception {
        byte[] data = msg.toByteArray();
        Socket socket = new Socket();
        try {
            socket.connect(new InetSocketAddress(InetAddress.getByName("127.0.0.1"), port), SOCKET_TIMEOUT_MS);
            socket.setSoTimeout(SOCKET_TIMEOUT_MS);
            DataOutputStream out = new DataOutputStream(socket.getOutputStream());
            out.writeInt(data.length);
            out.write(data);
            out.flush();
        } finally {
            try {
                socket.close();
            } catch (Exception ignored) {
            }
        }
    }
}
