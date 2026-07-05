package app.discopanel.agent.core;

import app.discopanel.agent.proto.AgentProto;

import java.util.Arrays;

/**
 * Accumulates tick durations between telemetry flushes. tickStart/tickEnd are
 * called from the server thread; drain runs on the telemetry thread. The
 * critical section is a few field updates, so contention is negligible.
 */
final class TickRecorder {
    private static final int MAX_SAMPLES = 1200; // ~60s of ticks

    private final Object lock = new Object();
    private final double[] samplesMs = new double[MAX_SAMPLES];
    private int sampleCount;
    private long windowStartNanos;
    private long tickStartNanos;

    void tickStart() {
        tickStartNanos = System.nanoTime();
    }

    void tickEnd() {
        long start = tickStartNanos;
        if (start == 0) {
            return;
        }
        double ms = (System.nanoTime() - start) / 1_000_000.0;
        synchronized (lock) {
            if (windowStartNanos == 0) {
                windowStartNanos = start;
            }
            if (sampleCount < MAX_SAMPLES) {
                samplesMs[sampleCount++] = ms;
            }
        }
    }

    /** Returns the tick sample for the window since the last drain, or null when no ticks ran. */
    AgentProto.TickSample drain() {
        double[] window;
        long windowStart;
        long now = System.nanoTime();
        synchronized (lock) {
            if (sampleCount == 0) {
                return null;
            }
            window = Arrays.copyOf(samplesMs, sampleCount);
            windowStart = windowStartNanos;
            sampleCount = 0;
            windowStartNanos = 0;
        }

        double elapsedSeconds = (now - windowStart) / 1_000_000_000.0;
        double tps = elapsedSeconds > 0 ? window.length / elapsedSeconds : 0;
        // A momentarily fast catch-up loop must not report >20 steady-state.
        if (tps > 20.0) {
            tps = 20.0;
        }

        double total = 0;
        double max = 0;
        for (double ms : window) {
            total += ms;
            if (ms > max) {
                max = ms;
            }
        }
        double[] sorted = window.clone();
        Arrays.sort(sorted);
        double p95 = sorted[Math.min(sorted.length - 1, (int) Math.floor(sorted.length * 0.95))];

        return AgentProto.TickSample.newBuilder()
                .setTps(tps)
                .setMsptAvg(total / window.length)
                .setMsptMax(max)
                .setMsptP95(p95)
                .build();
    }
}
