package app.discopanel.agent;

import app.discopanel.agent.proto.AgentProto;

import java.lang.management.GarbageCollectorMXBean;
import java.lang.management.ManagementFactory;
import java.lang.management.MemoryUsage;

/**
 * In-process JVM telemetry from the standard MX beans. GC counters are
 * cumulative, so each sample reports the delta since the previous one.
 */
final class JvmSampler {
    private long lastGcCount;
    private long lastGcTimeMs;

    AgentProto.JvmSample sample() {
        MemoryUsage heap = ManagementFactory.getMemoryMXBean().getHeapMemoryUsage();

        long gcCount = 0;
        long gcTimeMs = 0;
        for (GarbageCollectorMXBean gc : ManagementFactory.getGarbageCollectorMXBeans()) {
            long count = gc.getCollectionCount();
            long time = gc.getCollectionTime();
            if (count > 0) {
                gcCount += count;
            }
            if (time > 0) {
                gcTimeMs += time;
            }
        }
        long countDelta = Math.max(0, gcCount - lastGcCount);
        long timeDelta = Math.max(0, gcTimeMs - lastGcTimeMs);
        lastGcCount = gcCount;
        lastGcTimeMs = gcTimeMs;

        return AgentProto.JvmSample.newBuilder()
                .setHeapUsedMb(heap.getUsed() / 1024.0 / 1024.0)
                .setHeapMaxMb(heap.getMax() > 0 ? heap.getMax() / 1024.0 / 1024.0 : 0)
                .setGc(AgentProto.GcWindow.newBuilder()
                        .setCount(countDelta)
                        .setTotalMs(timeDelta)
                        .setMaxMs(0))
                .setThreadCount(ManagementFactory.getThreadMXBean().getThreadCount())
                .setClassCount(ManagementFactory.getClassLoadingMXBean().getLoadedClassCount())
                .build();
    }
}
