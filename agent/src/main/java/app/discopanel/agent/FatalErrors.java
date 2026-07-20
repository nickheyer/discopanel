package app.discopanel.agent;

import app.discopanel.agent.proto.AgentProto;

import java.io.DataOutputStream;
import java.lang.annotation.Annotation;
import java.lang.instrument.Instrumentation;
import java.lang.management.ManagementFactory;
import java.lang.management.ThreadInfo;
import java.lang.reflect.Method;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.net.URL;
import java.security.CodeSource;
import java.security.ProtectionDomain;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashMap;
import java.util.HashSet;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.atomic.AtomicReference;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

// Encodes live throwables into structured fatal error reports
final class FatalErrors {
    private static final int MAX_CAUSES = 8;
    private static final int MAX_FRAMES = 32;
    private static final int MAX_FAILED_MODS = 32;
    private static final int MAX_DUMP_THREADS = 8;
    private static final int SOCKET_TIMEOUT_MS = 2000;
    private static final long LOCATIONS_TTL_MS = 10000;

    private static final String MIXIN_MERGED =
            "org.spongepowered.asm.mixin.transformer.meta.MixinMerged";

    private static final Object indexLock = new Object();
    private static ClassIndex cachedIndex;
    private static long cachedIndexAt;
    private static final AtomicReference<AgentProto.FatalError> lastSent =
            new AtomicReference<AgentProto.FatalError>();

    private FatalErrors() {
    }

    static void installUncaughtHandler(Instrumentation inst, int port) {
        Thread.UncaughtExceptionHandler previous = Thread.getDefaultUncaughtExceptionHandler();
        Thread.setDefaultUncaughtExceptionHandler(new UncaughtReporter(inst, port, previous));
    }

    private static final class UncaughtReporter implements Thread.UncaughtExceptionHandler {
        private final Instrumentation inst;
        private final int port;
        private final Thread.UncaughtExceptionHandler previous;

        UncaughtReporter(Instrumentation inst, int port, Thread.UncaughtExceptionHandler previous) {
            this.inst = inst;
            this.port = port;
            this.previous = previous;
        }

        @Override
        public void uncaughtException(Thread thread, Throwable error) {
            try {
                send(port, build(inst, thread.getName(), error, true));
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
        ClassIndex index = classIndex(inst);
        AgentProto.FatalError.Builder fatal = AgentProto.FatalError.newBuilder()
                .setThread(thread == null ? "" : thread)
                .setUncaught(uncaught);

        Set<Throwable> seen = Collections.newSetFromMap(new IdentityHashMap<Throwable, Boolean>());
        List<Throwable> ordered = new ArrayList<Throwable>();
        addCauseChain(error, seen, ordered);
        // Suppressed failures often hide the real crash
        for (int i = 0; i < ordered.size() && ordered.size() < MAX_CAUSES; i++) {
            for (Throwable sup : ordered.get(i).getSuppressed()) {
                addCauseChain(sup, seen, ordered);
            }
        }
        for (Throwable cause : ordered) {
            AgentProto.CrashCause.Builder cb = AgentProto.CrashCause.newBuilder()
                    .setType(cause.getClass().getName());
            if (cause.getMessage() != null) {
                cb.setMessage(cause.getMessage());
            }
            StackTraceElement[] frames = cause.getStackTrace();
            for (int i = 0; i < frames.length && i < MAX_FRAMES; i++) {
                cb.addFrames(encodeFrame(frames[i], index));
            }
            fatal.addCauses(cb);
        }

        for (AgentProto.FailedMod mod : failedMods(error, index)) {
            fatal.addFailedMods(mod);
        }
        return fatal.build();
    }

    /** Walks one cause chain into the ordered list, cycle safe */
    private static void addCauseChain(Throwable error, Set<Throwable> seen, List<Throwable> ordered) {
        Throwable cause = error;
        while (cause != null && seen.add(cause) && ordered.size() < MAX_CAUSES) {
            ordered.add(cause);
            cause = cause.getCause();
        }
    }

    /** Reports stuck threads as a boot stall fatal error */
    static AgentProto.FatalError stallDump(Instrumentation inst) {
        ClassIndex index = classIndex(inst);
        String self = ownLocation();
        List<ThreadInfo> picked = new ArrayList<ThreadInfo>();
        for (ThreadInfo info : ManagementFactory.getThreadMXBean().dumpAllThreads(false, false)) {
            if (info != null && hasForeignFrame(info, index, self)) {
                picked.add(info);
            }
        }
        Collections.sort(picked, new Comparator<ThreadInfo>() {
            @Override
            public int compare(ThreadInfo a, ThreadInfo b) {
                return suspicion(b) - suspicion(a);
            }
        });
        if (picked.size() > MAX_DUMP_THREADS) {
            picked = picked.subList(0, MAX_DUMP_THREADS);
        }
        // Cause chains read root cause last, strongest thread goes last
        Collections.reverse(picked);

        AgentProto.FatalError.Builder fatal = AgentProto.FatalError.newBuilder();
        for (ThreadInfo info : picked) {
            AgentProto.CrashCause.Builder cause = AgentProto.CrashCause.newBuilder()
                    .setType("BootStall")
                    .setMessage(info.getThreadName() + " is "
                            + info.getThreadState().name().toLowerCase(Locale.ROOT));
            StackTraceElement[] frames = info.getStackTrace();
            for (int i = 0; i < frames.length && i < MAX_FRAMES; i++) {
                cause.addFrames(encodeFrame(frames[i], index));
            }
            fatal.addCauses(cause);
        }
        if (!picked.isEmpty()) {
            fatal.setThread(picked.get(picked.size() - 1).getThreadName());
        }
        return fatal.build();
    }

    /** Lock and wait states outrank plain runnable threads */
    private static int suspicion(ThreadInfo info) {
        switch (info.getThreadState()) {
            case BLOCKED:
                return 3;
            case WAITING:
                return 2;
            case TIMED_WAITING:
                return 1;
            default:
                return 0;
        }
    }

    /** True when any frame runs code beyond the JDK and this agent */
    private static boolean hasForeignFrame(ThreadInfo info, ClassIndex index, String self) {
        for (StackTraceElement f : info.getStackTrace()) {
            String location = index.location(f.getClassName());
            if (location != null && !location.equals(self)) {
                return true;
            }
        }
        return false;
    }

    /** CodeSource URL of the agent jar itself */
    private static String ownLocation() {
        try {
            CodeSource source = FatalErrors.class.getProtectionDomain().getCodeSource();
            if (source != null && source.getLocation() != null) {
                return source.getLocation().toString();
            }
        } catch (Throwable ignored) {
        }
        return "";
    }

    /** Reads the loader's per-mod failure list off the exception object */
    static List<AgentProto.FailedMod> failedMods(Throwable error, ClassIndex index) {
        List<AgentProto.FailedMod> mods = new ArrayList<AgentProto.FailedMod>();
        Set<Throwable> seen = Collections.newSetFromMap(new IdentityHashMap<Throwable, Boolean>());
        Throwable cause = error;
        while (cause != null && seen.add(cause)) {
            for (Object issue : loaderIssues(cause)) {
                if (mods.size() >= MAX_FAILED_MODS) {
                    return mods;
                }
                AgentProto.FailedMod mod = failedModOf(issue, index);
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
    private static AgentProto.FailedMod failedModOf(Object issue, ClassIndex index) {
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
        Throwable root = rootOf(failure);
        if (root != null) {
            mod.setErrorType(root.getClass().getName());
            StackTraceElement[] frames = root.getStackTrace();
            for (int i = 0; i < frames.length && i < MAX_FRAMES; i++) {
                mod.addFrames(encodeFrame(frames[i], index));
            }
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

    /** Walks to the deepest cause, cycle safe */
    private static Throwable rootOf(Throwable error) {
        Set<Throwable> seen = Collections.newSetFromMap(new IdentityHashMap<Throwable, Boolean>());
        Throwable cause = error;
        while (cause != null && seen.add(cause) && cause.getCause() != null) {
            cause = cause.getCause();
        }
        return cause;
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

    /** Loaded classes and their CodeSource URLs by name */
    static final class ClassIndex {
        private final Map<String, Class<?>> classes = new HashMap<String, Class<?>>();
        private final Map<String, String> locations = new HashMap<String, String>();

        Class<?> type(String name) {
            return classes.get(name);
        }

        String location(String name) {
            return locations.get(name);
        }
    }

    /** Indexes every loaded class, cached briefly for burst reuse */
    private static ClassIndex classIndex(Instrumentation inst) {
        synchronized (indexLock) {
            long now = System.currentTimeMillis();
            if (cachedIndex != null && now - cachedIndexAt < LOCATIONS_TTL_MS) {
                return cachedIndex;
            }
            ClassIndex index = new ClassIndex();
            if (inst == null) {
                return index;
            }
            for (Class<?> clazz : inst.getAllLoadedClasses()) {
                try {
                    if (clazz.isArray() || clazz.isPrimitive()) {
                        continue;
                    }
                    String name = clazz.getName();
                    if (!index.classes.containsKey(name)) {
                        index.classes.put(name, clazz);
                    }
                    ProtectionDomain domain = clazz.getProtectionDomain();
                    if (domain == null) {
                        continue;
                    }
                    CodeSource source = domain.getCodeSource();
                    if (source == null || source.getLocation() == null) {
                        continue;
                    }
                    if (!index.locations.containsKey(name)) {
                        index.locations.put(name, source.getLocation().toString());
                    }
                } catch (Throwable ignored) {
                    // Exotic classes may refuse introspection
                }
            }
            cachedIndex = index;
            cachedIndexAt = now;
            return index;
        }
    }

    /** Encodes one frame, mapped to the code that owns it */
    private static AgentProto.CrashFrame encodeFrame(StackTraceElement f, ClassIndex index) {
        AgentProto.CrashFrame.Builder fb = AgentProto.CrashFrame.newBuilder()
                .setClassName(f.getClassName())
                .setMethodName(f.getMethodName())
                .setLine(f.getLineNumber());
        if (f.getFileName() != null) {
            fb.setFileName(f.getFileName());
        }
        String location = mixinOwnerLocation(index.type(f.getClassName()), f.getMethodName());
        if (location == null) {
            location = index.location(f.getClassName());
        }
        if (location != null) {
            fb.setSourceLocation(location);
        }
        return fb.build();
    }

    /** Maps a mixin merged frame onto the jar shipping the mixin */
    private static String mixinOwnerLocation(Class<?> cls, String methodName) {
        if (cls == null || methodName == null) {
            return null;
        }
        try {
            for (Method m : cls.getDeclaredMethods()) {
                if (!m.getName().equals(methodName)) {
                    continue;
                }
                for (Annotation a : m.getDeclaredAnnotations()) {
                    if (!MIXIN_MERGED.equals(a.annotationType().getName())) {
                        continue;
                    }
                    Object mixin = call(a, "mixin");
                    ClassLoader loader = cls.getClassLoader();
                    if (!(mixin instanceof String) || loader == null) {
                        return null;
                    }
                    URL url = loader.getResource(((String) mixin).replace('.', '/') + ".class");
                    return url == null ? null : url.toString();
                }
            }
        } catch (Throwable ignored) {
            // Frame classes may refuse method introspection
        }
        return null;
    }

    /** Newest error this JVM reported, answers stall dump requests */
    static AgentProto.FatalError lastSent() {
        return lastSent.get();
    }

    /** Dedicated blocking socket, survives a dying telemetry thread */
    static void send(int port, AgentProto.FatalError fatal) throws Exception {
        lastSent.set(fatal);
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
