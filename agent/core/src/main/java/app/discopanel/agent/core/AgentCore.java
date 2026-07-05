package app.discopanel.agent.core;

import app.discopanel.agent.proto.AgentProto;

import java.lang.management.ManagementFactory;
import java.util.Collection;
import java.util.List;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * Loader-independent heart of the disco-agent mod. Owns the loopback
 * connection to the discopanel-runtime supervisor, the telemetry scheduler,
 * and the tick/JVM recorders. Shims feed game events in through the send
 * methods; the supervisor relays everything to the panel.
 */
public final class AgentCore {
    /** Set by the runtime supervisor on the server JVM command line. */
    public static final String PORT_PROPERTY = "discopanel.agent.port";

    private static final long TELEMETRY_PERIOD_SECONDS = 10;

    private final AgentConnection connection;
    private final TickRecorder ticks = new TickRecorder();
    private final JvmSampler jvm = new JvmSampler();
    private final ScheduledExecutorService scheduler;
    private final AtomicBoolean readySent = new AtomicBoolean(false);

    /**
     * Starts the agent when the runtime supervisor advertised a loopback port,
     * else returns null and the mod stays dormant (e.g. server started outside
     * a discopanel-runtime container).
     */
    public static AgentCore start(PlatformInfo info, PlatformAdapter adapter) {
        String property = System.getProperty(PORT_PROPERTY);
        if (property == null || property.isEmpty()) {
            return null;
        }
        int port;
        try {
            port = Integer.parseInt(property.trim());
        } catch (NumberFormatException e) {
            return null;
        }
        return new AgentCore(port, info, adapter);
    }

    private AgentCore(int port, PlatformInfo info, PlatformAdapter adapter) {
        AgentProto.Hello hello = AgentProto.Hello.newBuilder()
                .setSource(AgentProto.HelloSource.HELLO_SOURCE_MOD)
                .setVersion(info.agentVersion)
                .setLoader(info.loader)
                .setMcVersion(info.mcVersion)
                .build();
        this.connection = new AgentConnection(port, hello, adapter);
        this.scheduler = Executors.newSingleThreadScheduledExecutor(new ThreadFactory() {
            @Override
            public Thread newThread(Runnable r) {
                Thread t = new Thread(r, "disco-agent-telemetry");
                t.setDaemon(true);
                return t;
            }
        });
        this.connection.start();
        this.scheduler.scheduleAtFixedRate(new Runnable() {
            @Override
            public void run() {
                sendPeriodicTelemetry();
            }
        }, TELEMETRY_PERIOD_SECONDS, TELEMETRY_PERIOD_SECONDS, TimeUnit.SECONDS);
    }

    private void sendPeriodicTelemetry() {
        AgentProto.TickSample tick = ticks.drain();
        if (tick != null) {
            send(AgentProto.AgentMessage.newBuilder().setTickSample(tick).build());
        }
        send(AgentProto.AgentMessage.newBuilder().setJvmSample(jvm.sample()).build());
    }

    // -- game-thread hooks (cheap, lock-light) ---------------------------

    public void tickStart() {
        ticks.tickStart();
    }

    public void tickEnd() {
        ticks.tickEnd();
    }

    // -- shim-driven events ----------------------------------------------

    /** Reports server readiness; startup time is the JVM uptime. */
    public void sendReady() {
        if (!readySent.compareAndSet(false, true)) {
            return;
        }
        double uptimeSeconds = ManagementFactory.getRuntimeMXBean().getUptime() / 1000.0;
        send(AgentProto.AgentMessage.newBuilder()
                .setReady(AgentProto.Ready.newBuilder().setStartupSeconds(uptimeSeconds))
                .build());
    }

    public void sendStopping() {
        send(AgentProto.AgentMessage.newBuilder()
                .setStopping(AgentProto.Stopping.getDefaultInstance())
                .build());
    }

    /**
     * Reports a player event. playersOnline may be -1 when the platform hook
     * has no reliable post-event count; the panel then derives it from the
     * roster it maintains.
     */
    public void sendPlayerEvent(AgentProto.PlayerEventType type, String player, String uuid,
                                String detail, int playersOnline) {
        AgentProto.PlayerEvent.Builder event = AgentProto.PlayerEvent.newBuilder()
                .setType(type)
                .setPlayer(player == null ? "" : player)
                .setUuid(uuid == null ? "" : uuid)
                .setDetail(detail == null ? "" : detail)
                .setPlayersOnline(playersOnline);
        send(AgentProto.AgentMessage.newBuilder().setPlayerEvent(event).build());
    }

    /** Reports world state collected by the shim on the server thread. */
    public void sendWorldStats(List<AgentProto.DimensionStats> dimensions, Collection<String> onlinePlayers) {
        AgentProto.WorldStats.Builder stats = AgentProto.WorldStats.newBuilder()
                .addAllDimensions(dimensions)
                .addAllOnlinePlayers(onlinePlayers);
        send(AgentProto.AgentMessage.newBuilder().setWorldStats(stats).build());
    }

    /** Reports the dispatcher's root command names for console autocomplete. */
    public void sendCommandList(Collection<String> commands) {
        send(AgentProto.AgentMessage.newBuilder()
                .setCommandList(AgentProto.CommandList.newBuilder().addAllCommands(commands))
                .build());
    }

    private void send(AgentProto.AgentMessage message) {
        connection.enqueue(message);
    }

    /** Stops IO and telemetry threads (server shutdown). */
    public void shutdown() {
        sendStopping();
        scheduler.shutdown();
        connection.stop();
    }
}
