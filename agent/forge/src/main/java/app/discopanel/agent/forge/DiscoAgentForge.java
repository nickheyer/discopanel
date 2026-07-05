package app.discopanel.agent.forge;

import app.discopanel.agent.core.AgentCore;
import app.discopanel.agent.core.PlatformAdapter;
import app.discopanel.agent.core.PlatformInfo;
import app.discopanel.agent.proto.AgentProto;
import com.mojang.brigadier.tree.CommandNode;
import net.minecraft.advancements.DisplayInfo;
import net.minecraft.network.chat.Component;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.world.entity.Entity;
import net.minecraftforge.common.MinecraftForge;
import net.minecraftforge.event.ServerChatEvent;
import net.minecraftforge.event.TickEvent;
import net.minecraftforge.event.entity.living.LivingDeathEvent;
import net.minecraftforge.event.entity.player.AdvancementEvent;
import net.minecraftforge.event.entity.player.PlayerEvent;
import net.minecraftforge.event.server.ServerStartedEvent;
import net.minecraftforge.event.server.ServerStoppingEvent;
import net.minecraftforge.eventbus.api.SubscribeEvent;
import net.minecraftforge.fml.ModList;
import net.minecraftforge.fml.common.Mod;
import net.minecraftforge.fml.loading.FMLLoader;

import java.util.ArrayList;
import java.util.List;

/** Legacy Forge (1.20.1) shim: wires Forge server events into the agent core. */
@Mod("disco_agent")
public final class DiscoAgentForge {
    private static final int WORLD_STATS_TICK_INTERVAL = 600; // 30s at 20 TPS

    private final AgentCore core;
    private volatile MinecraftServer server;
    private int tickCounter;

    public DiscoAgentForge() {
        PlatformAdapter adapter = (sender, message) -> {
            MinecraftServer current = server;
            if (current == null) {
                return;
            }
            current.execute(() -> current.getPlayerList()
                    .broadcastSystemMessage(Component.literal("<" + sender + "> " + message), false));
        };
        core = AgentCore.start(new PlatformInfo("forge",
                FMLLoader.versionInfo().mcVersion(), modVersion()), adapter);
        if (core != null) {
            MinecraftForge.EVENT_BUS.register(this);
        }
    }

    @SubscribeEvent
    public void onServerStarted(ServerStartedEvent event) {
        server = event.getServer();
        core.sendReady();
        List<String> commands = new ArrayList<>();
        for (CommandNode<?> node : event.getServer().getCommands().getDispatcher().getRoot().getChildren()) {
            commands.add(node.getName());
        }
        core.sendCommandList(commands);
    }

    @SubscribeEvent
    public void onServerStopping(ServerStoppingEvent event) {
        core.sendStopping();
    }

    @SubscribeEvent
    public void onServerTick(TickEvent.ServerTickEvent event) {
        if (event.phase == TickEvent.Phase.START) {
            core.tickStart();
            return;
        }
        core.tickEnd();
        if (++tickCounter % WORLD_STATS_TICK_INTERVAL == 0 && server != null) {
            sendWorldStats(server);
        }
    }

    @SubscribeEvent
    public void onLogin(PlayerEvent.PlayerLoggedInEvent event) {
        if (event.getEntity() instanceof ServerPlayer player) {
            core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_JOIN,
                    player.getGameProfile().getName(), player.getUUID().toString(), "",
                    player.getServer() != null ? player.getServer().getPlayerList().getPlayerCount() : -1);
        }
    }

    @SubscribeEvent
    public void onLogout(PlayerEvent.PlayerLoggedOutEvent event) {
        if (event.getEntity() instanceof ServerPlayer player) {
            core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_LEAVE,
                    player.getGameProfile().getName(), player.getUUID().toString(), "", -1);
        }
    }

    @SubscribeEvent
    public void onDeath(LivingDeathEvent event) {
        if (event.getEntity() instanceof ServerPlayer player) {
            core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_DEATH,
                    player.getGameProfile().getName(), player.getUUID().toString(),
                    event.getSource().getLocalizedDeathMessage(player).getString(), -1);
        }
    }

    @SubscribeEvent
    public void onAdvancement(AdvancementEvent.AdvancementEarnEvent event) {
        DisplayInfo display = event.getAdvancement().getDisplay();
        // Only real advancements have display info; recipe unlocks do not.
        if (display != null && event.getEntity() instanceof ServerPlayer player) {
            core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_ADVANCEMENT,
                    player.getGameProfile().getName(), player.getUUID().toString(),
                    display.getTitle().getString(), -1);
        }
    }

    @SubscribeEvent
    public void onChat(ServerChatEvent event) {
        core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_CHAT,
                event.getPlayer().getGameProfile().getName(), event.getPlayer().getUUID().toString(),
                event.getMessage().getString(), -1);
    }

    private void sendWorldStats(MinecraftServer server) {
        List<AgentProto.DimensionStats> dimensions = new ArrayList<>();
        for (ServerLevel level : server.getAllLevels()) {
            int entities = 0;
            for (Entity ignored : level.getAllEntities()) {
                entities++;
            }
            dimensions.add(AgentProto.DimensionStats.newBuilder()
                    .setDimension(level.dimension().location().toString())
                    .setEntities(entities)
                    .setChunks(level.getChunkSource().getLoadedChunksCount())
                    .setPlayers(level.players().size())
                    .build());
        }
        List<String> names = new ArrayList<>();
        for (ServerPlayer player : server.getPlayerList().getPlayers()) {
            names.add(player.getGameProfile().getName());
        }
        core.sendWorldStats(dimensions, names);
    }

    private static String modVersion() {
        return ModList.get().getModContainerById("disco_agent")
                .map(container -> container.getModInfo().getVersion().toString())
                .orElse("unknown");
    }
}
