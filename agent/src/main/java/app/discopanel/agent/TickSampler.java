package app.discopanel.agent;

import app.discopanel.agent.proto.AgentProto;

import java.util.concurrent.locks.LockSupport;

/**
 * Measures the server tick thread at the JVM scheduling level. Every
 * Minecraft version runs its tick loop on one thread that alternates
 * between tick work and waiting out each 50ms slot, so sampling that
 * thread's state yields the busy share and the worst busy run with no
 * game code, mappings, or version checks involved.
 */
final class TickSampler {
    /** Vanilla name of the tick thread on every version and fork. */
    private static final String SERVER_THREAD_NAME = "Server thread";

    private static final long SAMPLE_PERIOD_NANOS = 1_000_000L;
    /** Gaps this large mean the JVM was suspended, skip them. */
    private static final long MAX_GAP_NANOS = 1_000_000_000L;
    /** Samples between rescans while the tick thread is missing. */
    private static final int RESCAN_INTERVAL = 1024;

    // Window state shared with drain, guarded by lock
    private final Object lock = new Object();
    private long busyNanos;
    private long windowNanos;
    private long longestRunNanos;
    private long runStartNanos = -1;

    // Sampler thread state, no locking needed
    private Thread target;
    private long lastSampleNanos;
    private int samplesUntilRescan;

    void start() {
        Thread t = new Thread(new Runnable() {
            @Override
            public void run() {
                sampleLoop();
            }
        }, "disco-agent-tick");
        t.setDaemon(true);
        t.start();
    }

    private void sampleLoop() {
        lastSampleNanos = System.nanoTime();
        while (true) {
            LockSupport.parkNanos(SAMPLE_PERIOD_NANOS);
            long now = System.nanoTime();
            long elapsed = now - lastSampleNanos;
            lastSampleNanos = now;

            if (target == null || !target.isAlive()) {
                target = null;
                if (samplesUntilRescan-- <= 0) {
                    samplesUntilRescan = RESCAN_INTERVAL;
                    target = findServerThread();
                }
                continue;
            }
            if (elapsed <= 0 || elapsed > MAX_GAP_NANOS) {
                continue;
            }

            Thread.State state = target.getState();
            boolean busy = state == Thread.State.RUNNABLE || state == Thread.State.BLOCKED;

            synchronized (lock) {
                windowNanos += elapsed;
                if (busy) {
                    busyNanos += elapsed;
                    if (runStartNanos < 0) {
                        runStartNanos = now - elapsed;
                    }
                    long run = now - runStartNanos;
                    if (run > longestRunNanos) {
                        longestRunNanos = run;
                    }
                } else {
                    runStartNanos = -1;
                }
            }
        }
    }

    /** Returns the window measurement, null before the tick thread exists. */
    AgentProto.TickThreadSample drain() {
        synchronized (lock) {
            if (windowNanos <= 0) {
                return null;
            }
            double fraction = (double) busyNanos / windowNanos;
            double longestMs = longestRunNanos / 1_000_000.0;
            busyNanos = 0;
            windowNanos = 0;
            longestRunNanos = 0;
            // A run crossing the window boundary keeps counting next window
            if (runStartNanos >= 0) {
                runStartNanos = System.nanoTime();
            }
            return AgentProto.TickThreadSample.newBuilder()
                    .setBusyFraction(fraction)
                    .setLongestBusyMs(longestMs)
                    .build();
        }
    }

    private static Thread findServerThread() {
        ThreadGroup group = Thread.currentThread().getThreadGroup();
        while (group.getParent() != null) {
            group = group.getParent();
        }
        Thread[] threads = new Thread[group.activeCount() + 16];
        int count = group.enumerate(threads, true);
        for (int i = 0; i < count; i++) {
            if (threads[i] != null && SERVER_THREAD_NAME.equals(threads[i].getName())) {
                return threads[i];
            }
        }
        return null;
    }
}
