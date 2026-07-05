package app.discopanel.agent;

import app.discopanel.agent.proto.AgentProto;

import java.lang.instrument.Instrumentation;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;

/**
 * DiscoPanel telemetry javaagent. Attached by the runtime supervisor via
 * -javaagent on any server JVM. Uses only standard JVM APIs, so one jar
 * covers every loader, plugin server, and vanilla alike.
 */
public final class DiscoAgent {
    /** Set by the runtime supervisor on the java command line. */
    public static final String PORT_PROPERTY = "discopanel.agent.port";

    private static final long TELEMETRY_PERIOD_SECONDS = 10;

    private DiscoAgent() {
    }

    /** Starts telemetry when supervised, else stays dormant. */
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
            start(port);
        } catch (Throwable t) {
            // Telemetry must never break the server JVM
        }
    }

    private static void start(int port) {
        AgentProto.Hello hello = AgentProto.Hello.newBuilder()
                .setSource(AgentProto.HelloSource.HELLO_SOURCE_JVM)
                .setVersion(version())
                .build();
        final AgentConnection connection = new AgentConnection(port, hello);
        final JvmSampler jvm = new JvmSampler();
        final TickSampler ticks = new TickSampler();
        connection.start();
        ticks.start();

        ScheduledExecutorService scheduler = Executors.newSingleThreadScheduledExecutor(new ThreadFactory() {
            @Override
            public Thread newThread(Runnable r) {
                Thread t = new Thread(r, "disco-agent-telemetry");
                t.setDaemon(true);
                return t;
            }
        });
        scheduler.scheduleAtFixedRate(new Runnable() {
            @Override
            public void run() {
                AgentProto.TickThreadSample tick = ticks.drain();
                if (tick != null) {
                    connection.enqueue(AgentProto.AgentMessage.newBuilder().setTickThreadSample(tick).build());
                }
                connection.enqueue(AgentProto.AgentMessage.newBuilder().setJvmSample(jvm.sample()).build());
            }
        }, TELEMETRY_PERIOD_SECONDS, TELEMETRY_PERIOD_SECONDS, TimeUnit.SECONDS);
    }

    private static String version() {
        String v = DiscoAgent.class.getPackage().getImplementationVersion();
        return v == null ? "dev" : v;
    }
}
