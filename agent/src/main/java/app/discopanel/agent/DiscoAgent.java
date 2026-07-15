package app.discopanel.agent;

import app.discopanel.agent.proto.AgentProto;

import java.lang.instrument.Instrumentation;
import java.lang.management.GarbageCollectorMXBean;
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

    private static void start(final int port, final Instrumentation inst) {
        AgentProto.Hello hello = AgentProto.Hello.newBuilder()
                .setSource(AgentProto.HelloSource.HELLO_SOURCE_JVM)
                .setVersion(version())
                .build();
        final AgentConnection connection = new AgentConnection(port, hello,
                new AgentConnection.PanelHandler() {
                    @Override
                    public AgentProto.AgentMessage onThreadDumpRequest() {
                        // Error that preceded the stall beats parked thread frames
                        AgentProto.FatalError fatal = FatalErrors.lastSent();
                        if (fatal == null) {
                            fatal = FatalErrors.stallDump(inst);
                        }
                        return AgentProto.AgentMessage.newBuilder()
                                .setFatalError(fatal)
                                .build();
                    }
                });
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
        final long[] gcPrev = gcTotals();
        scheduler.scheduleAtFixedRate(new Runnable() {
            @Override
            public void run() {
                AgentProto.TickThreadSample tick = ticks.drain();
                if (tick != null) {
                    connection.enqueue(AgentProto.AgentMessage.newBuilder().setTickThreadSample(tick).build());
                }
                connection.enqueue(AgentProto.AgentMessage.newBuilder().setJvmSample(jvmSample(gcPrev)).build());
            }
        }, TELEMETRY_PERIOD_SECONDS, TELEMETRY_PERIOD_SECONDS, TimeUnit.SECONDS);
    }

    /** Builds one sample, folds GC bean deltas since last call */
    private static AgentProto.JvmSample jvmSample(long[] gcPrev) {
        MemoryUsage heap = ManagementFactory.getMemoryMXBean().getHeapMemoryUsage();
        long[] totals = gcTotals();
        long gcCount = Math.max(0L, totals[0] - gcPrev[0]);
        long gcTimeMs = Math.max(0L, totals[1] - gcPrev[1]);
        gcPrev[0] = totals[0];
        gcPrev[1] = totals[1];
        return AgentProto.JvmSample.newBuilder()
                .setHeapUsedMb(heap.getUsed() / 1024.0 / 1024.0)
                .setHeapMaxMb(heap.getMax() > 0 ? heap.getMax() / 1024.0 / 1024.0 : 0)
                .setThreadCount(ManagementFactory.getThreadMXBean().getThreadCount())
                .setClassCount(ManagementFactory.getClassLoadingMXBean().getLoadedClassCount())
                .setGc(AgentProto.GcWindow.newBuilder()
                        .setCount(gcCount)
                        .setTotalMs(gcTimeMs)
                        .build())
                .build();
    }

    /** Sums collection count and time across collector beans */
    private static long[] gcTotals() {
        long count = 0;
        long timeMs = 0;
        for (GarbageCollectorMXBean bean : ManagementFactory.getGarbageCollectorMXBeans()) {
            long c = bean.getCollectionCount();
            if (c > 0) {
                count += c;
            }
            long t = bean.getCollectionTime();
            if (t > 0) {
                timeMs += t;
            }
        }
        return new long[]{count, timeMs};
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
