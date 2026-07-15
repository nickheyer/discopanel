package app.discopanel.agent;

import app.discopanel.agent.proto.AgentProto;

import java.lang.instrument.Instrumentation;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Proxy;
import java.util.Collection;
import java.util.Collections;
import java.util.IdentityHashMap;
import java.util.Set;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;

// Grabs live throwables off log4j error events via a proxy appender
// Startup failures are caught and logged, never rethrown, so the
// uncaught handler alone misses every mod loading crash
final class LogErrorWatcher {
    private static final int MAX_REPORTS = 8;
    private static final int ERROR_INT_LEVEL = 200;

    private final Instrumentation inst;
    private final int port;
    private final AtomicInteger reports = new AtomicInteger();
    private final AtomicBoolean armed = new AtomicBoolean();
    private final Set<Object> hooked =
            Collections.newSetFromMap(new IdentityHashMap<Object, Boolean>());

    LogErrorWatcher(Instrumentation inst, int port) {
        this.inst = inst;
        this.port = port;
    }

    /** Hooks every live log4j context, true when watching can stop */
    boolean tryInstall() {
        if (inst == null) {
            return true;
        }
        for (Class<?> clazz : inst.getAllLoadedClasses()) {
            if (!"org.apache.logging.log4j.core.LoggerContext".equals(clazz.getName())) {
                continue;
            }
            hookContexts(clazz);
        }
        return false;
    }

    /** Adds the proxy appender to each unhooked context config */
    private void hookContexts(Class<?> contextClass) {
        try {
            ClassLoader loader = contextClass.getClassLoader();
            if (loader == null) {
                loader = getClass().getClassLoader();
            }
            Class<?> appenderClass = Class.forName("org.apache.logging.log4j.core.Appender", false, loader);
            Class<?> filterClass = Class.forName("org.apache.logging.log4j.core.Filter", false, loader);
            Class<?> levelClass = Class.forName("org.apache.logging.log4j.Level", false, loader);
            Class<?> configClass = Class.forName("org.apache.logging.log4j.core.config.Configuration", false, loader);
            Class<?> loggerConfigClass = Class.forName("org.apache.logging.log4j.core.config.LoggerConfig", false, loader);
            Class<?> eventClass = Class.forName("org.apache.logging.log4j.core.LogEvent", false, loader);
            Class<?> stateClass = Class.forName("org.apache.logging.log4j.core.LifeCycle$State", false, loader);
            Class<?> logManagerClass = Class.forName("org.apache.logging.log4j.LogManager", false, loader);

            Object errorLevel = levelClass.getField("ERROR").get(null);
            Object startedState = stateOf(stateClass, "STARTED");
            Method getThrown = eventClass.getMethod("getThrown");
            Method getLevel = eventClass.getMethod("getLevel");
            Method getThreadName = eventClass.getMethod("getThreadName");
            Method intLevel = levelClass.getMethod("intLevel");

            Method getConfiguration = contextClass.getMethod("getConfiguration");
            Method updateLoggers = contextClass.getMethod("updateLoggers");
            Method getRootLogger = configClass.getMethod("getRootLogger");
            Method addAppender = loggerConfigClass.getMethod("addAppender", appenderClass, levelClass, filterClass);

            for (Object ctx : selectorContexts(logManagerClass)) {
                try {
                    Object config = getConfiguration.invoke(ctx);
                    Object rootConfig = getRootLogger.invoke(config);
                    synchronized (hooked) {
                        // Keyed by root config, reconfigure gets rehooked
                        if (hooked.contains(rootConfig)) {
                            continue;
                        }
                        Object appender = Proxy.newProxyInstance(loader, new Class<?>[]{appenderClass},
                                new AppenderHandler(startedState, getThrown, getLevel, getThreadName, intLevel));
                        addAppender.invoke(rootConfig, appender, errorLevel, null);
                        updateLoggers.invoke(ctx);
                        hooked.add(rootConfig);
                    }
                    reportArmed();
                } catch (Throwable ignored) {
                }
            }
        } catch (Throwable ignored) {
        }
    }

    /** Tells the supervisor once that error capture is live */
    private void reportArmed() {
        int contexts;
        synchronized (hooked) {
            contexts = hooked.size();
        }
        if (contexts == 0 || !armed.compareAndSet(false, true)) {
            return;
        }
        try {
            FatalErrors.sendMessage(port, AgentProto.AgentMessage.newBuilder()
                    .setCaptureArmed(AgentProto.CaptureArmed.newBuilder()
                            .setContextsHooked(contexts))
                    .build());
        } catch (Throwable ignored) {
            armed.set(false);
        }
    }

    /** Game loggers live in selector contexts, never the agent's own */
    private static Collection<?> selectorContexts(Class<?> logManagerClass) {
        try {
            Object factory = logManagerClass.getMethod("getFactory").invoke(null);
            Object selector = factory.getClass().getMethod("getSelector").invoke(factory);
            Object contexts = selector.getClass().getMethod("getLoggerContexts").invoke(selector);
            if (contexts instanceof Collection) {
                return (Collection<?>) contexts;
            }
        } catch (Throwable ignored) {
        }
        return Collections.emptyList();
    }

    private static Object stateOf(Class<?> stateClass, String name) {
        for (Object constant : stateClass.getEnumConstants()) {
            if (name.equals(String.valueOf(constant))) {
                return constant;
            }
        }
        return null;
    }

    // Answers the whole Appender surface, forwards append events
    private final class AppenderHandler implements InvocationHandler {
        private final Object startedState;
        private final Method getThrown;
        private final Method getLevel;
        private final Method getThreadName;
        private final Method intLevel;

        AppenderHandler(Object startedState, Method getThrown, Method getLevel, Method getThreadName, Method intLevel) {
            this.startedState = startedState;
            this.getThrown = getThrown;
            this.getLevel = getLevel;
            this.getThreadName = getThreadName;
            this.intLevel = intLevel;
        }

        @Override
        public Object invoke(Object proxy, Method method, Object[] args) {
            String name = method.getName();
            if ("append".equals(name)) {
                handle(args[0]);
                return null;
            }
            if ("getName".equals(name) || "toString".equals(name)) {
                return "DiscoPanelAgent";
            }
            if ("isStarted".equals(name) || "ignoreExceptions".equals(name)) {
                return Boolean.TRUE;
            }
            if ("getState".equals(name)) {
                return startedState;
            }
            if ("equals".equals(name)) {
                return proxy == args[0];
            }
            if ("hashCode".equals(name)) {
                return System.identityHashCode(proxy);
            }
            Class<?> ret = method.getReturnType();
            if (ret == boolean.class) {
                return Boolean.FALSE;
            }
            if (ret == int.class) {
                return 0;
            }
            if (ret == long.class) {
                return 0L;
            }
            return null;
        }

        /** Reports error events that carry a live throwable */
        private void handle(Object event) {
            if (event == null) {
                return;
            }
            try {
                Object thrown = getThrown.invoke(event);
                if (!(thrown instanceof Throwable)) {
                    return;
                }
                Object level = getLevel.invoke(event);
                if (level == null || (Integer) intLevel.invoke(level) > ERROR_INT_LEVEL) {
                    return;
                }
                // Loader failure lists bypass the cap, they drive repair
                if (reports.incrementAndGet() > MAX_REPORTS
                        && !FatalErrors.hasLoaderVerdicts((Throwable) thrown)) {
                    return;
                }
                Object thread = getThreadName.invoke(event);
                AgentProto.FatalError fatal = FatalErrors.build(
                        inst, thread instanceof String ? (String) thread : "", (Throwable) thrown, false);
                FatalErrors.send(port, fatal);
            } catch (Throwable ignored) {
                // Reporting must never disturb the logging path
            }
        }
    }
}
