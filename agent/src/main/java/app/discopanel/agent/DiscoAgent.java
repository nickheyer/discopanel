package app.discopanel.agent;

import app.discopanel.agent.proto.AgentProto;

import java.lang.instrument.Instrumentation;
import java.lang.management.ManagementFactory;
import java.lang.management.MemoryUsage;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ScheduledFuture;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.concurrent.atomic.AtomicReference;

public final class DiscoAgent {
    public static final String PORT_PROPERTY = "discopanel.agent.port";

    private static final long TELEMETRY_PERIOD_SECONDS = 10;
    private static final int LOG_HOOK_MAX_ATTEMPTS = 120;

    private DiscoAgent() {
    }

    public static void premain(String args, Instrumentation inst) {
        String property = System.getProperty(PORT_PROPERTY);
        if (property == null || property.isEmpty()) {
            return;
        }
        int port;
        try {
            port = Integer.parseInt(property.trim());
        } catch (NumberFormatException e) {
            return;
        }
        try {
            start(port, inst);
        } catch (Throwable t) {
        }
    }

    private static void start(int port, Instrumentation inst) {
        AgentProto.Hello hello = AgentProto.Hello.newBuilder()
                .setSource(AgentProto.HelloSource.HELLO_SOURCE_JVM)
                .setVersion(version())
                .build();
        final AgentConnection connection = new AgentConnection(port, hello);
        final TickSampler ticks = new TickSampler();
        connection.start();
        ticks.start();
        FatalErrors.installUncaughtHandler(inst, port);

        ScheduledExecutorService scheduler = Executors.newSingleThreadScheduledExecutor(new ThreadFactory() {
            @Override
            public Thread newThread(Runnable r) {
                Thread t = new Thread(r, "disco-agent-telemetry");
                t.setDaemon(true);
                return t;
            }
        });
        watchLogErrors(scheduler, inst, port);
        scheduler.scheduleAtFixedRate(new Runnable() {
            @Override
            public void run() {
                AgentProto.TickThreadSample tick = ticks.drain();
                if (tick != null) {
                    connection.enqueue(AgentProto.AgentMessage.newBuilder().setTickThreadSample(tick).build());
                }
                connection.enqueue(AgentProto.AgentMessage.newBuilder().setJvmSample(jvmSample()).build());
            }
        }, TELEMETRY_PERIOD_SECONDS, TELEMETRY_PERIOD_SECONDS, TimeUnit.SECONDS);
    }

    private static AgentProto.JvmSample jvmSample() {
        MemoryUsage heap = ManagementFactory.getMemoryMXBean().getHeapMemoryUsage();
        return AgentProto.JvmSample.newBuilder()
                .setHeapUsedMb(heap.getUsed() / 1024.0 / 1024.0)
                .setHeapMaxMb(heap.getMax() > 0 ? heap.getMax() / 1024.0 / 1024.0 : 0)
                .setThreadCount(ManagementFactory.getThreadMXBean().getThreadCount())
                .setClassCount(ManagementFactory.getClassLoadingMXBean().getLoadedClassCount())
                .build();
    }

    private static void watchLogErrors(ScheduledExecutorService scheduler, Instrumentation inst, int port) {
        final LogErrorWatcher watcher = new LogErrorWatcher(inst, port);
        final AtomicInteger attempts = new AtomicInteger();
        final AtomicReference<ScheduledFuture<?>> handle = new AtomicReference<ScheduledFuture<?>>();
        handle.set(scheduler.scheduleWithFixedDelay(new Runnable() {
            @Override
            public void run() {
                boolean done;
                try {
                    done = watcher.tryInstall();
                } catch (Throwable t) {
                    done = false;
                }
                if (done || attempts.incrementAndGet() >= LOG_HOOK_MAX_ATTEMPTS) {
                    ScheduledFuture<?> f = handle.get();
                    if (f != null) {
                        f.cancel(false);
                    }
                }
            }
        }, 1, 1, TimeUnit.SECONDS));
    }

    private static String version() {
        String v = DiscoAgent.class.getPackage().getImplementationVersion();
        return v == null ? "dev" : v;
    }
}
