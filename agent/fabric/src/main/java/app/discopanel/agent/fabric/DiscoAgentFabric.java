package app.discopanel.agent.fabric;

import app.discopanel.agent.core.AgentCore;
import app.discopanel.agent.core.PlatformInfo;
import app.discopanel.agent.proto.AgentProto;
import com.mojang.brigadier.tree.CommandNode;
import net.fabricmc.api.DedicatedServerModInitializer;
import net.fabricmc.fabric.api.entity.event.v1.ServerLivingEntityEvents;
import net.fabricmc.fabric.api.event.lifecycle.v1.ServerLifecycleEvents;
import net.fabricmc.fabric.api.event.lifecycle.v1.ServerTickEvents;
import net.fabricmc.fabric.api.message.v1.ServerMessageEvents;
import net.fabricmc.fabric.api.networking.v1.ServerPlayConnectionEvents;
import net.fabricmc.loader.api.FabricLoader;
import net.fabricmc.loader.api.ModContainer;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.world.entity.Entity;

import java.util.ArrayList;
import java.util.List;

/**
 * Fabric shim: wires Fabric API server events into the shared agent core.
 * Dormant unless launched by the discopanel-runtime supervisor (which sets
 * the loopback port system property).
 */
public final class DiscoAgentFabric implements DedicatedServerModInitializer {
    private static final int WORLD_STATS_TICK_INTERVAL = 600; // 30s at 20 TPS

    private AgentCore core;
    private int tickCounter;

    @Override
    public void onInitializeServer() {
        FabricAdapter adapter = new FabricAdapter();
        core = AgentCore.start(new PlatformInfo("fabric", metadataVersion("minecraft"), metadataVersion("disco-agent")), adapter);
        if (core == null) {
            return;
        }

        ServerLifecycleEvents.SERVER_STARTED.register(server -> {
            adapter.server = server;
            core.sendReady();
            core.sendCommandList(rootCommands(server));
        });
        ServerLifecycleEvents.SERVER_STOPPING.register(server -> core.sendStopping());

        ServerTickEvents.START_SERVER_TICK.register(server -> core.tickStart());
        ServerTickEvents.END_SERVER_TICK.register(server -> {
            core.tickEnd();
            if (++tickCounter % WORLD_STATS_TICK_INTERVAL == 0) {
                sendWorldStats(server);
            }
        });

        ServerPlayConnectionEvents.JOIN.register((handler, sender, server) -> {
            ServerPlayer player = handler.player;
            core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_JOIN,
                    player.getGameProfile().getName(), player.getUUID().toString(), "",
                    server.getPlayerList().getPlayerCount());
        });
        ServerPlayConnectionEvents.DISCONNECT.register((handler, server) -> {
            ServerPlayer player = handler.player;
            core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_LEAVE,
                    player.getGameProfile().getName(), player.getUUID().toString(), "", -1);
        });

        ServerMessageEvents.CHAT_MESSAGE.register((message, sender, params) ->
                core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_CHAT,
                        sender.getGameProfile().getName(), sender.getUUID().toString(),
                        message.signedContent(), -1));

        ServerLivingEntityEvents.AFTER_DEATH.register((entity, source) -> {
            if (entity instanceof ServerPlayer) {
                ServerPlayer player = (ServerPlayer) entity;
                core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_DEATH,
                        player.getGameProfile().getName(), player.getUUID().toString(),
                        source.getLocalizedDeathMessage(player).getString(), -1);
            }
        });
        // Advancement events have no Fabric API hook; fabric servers report
        // everything else and the panel treats advancements as optional.
    }

    private static List<String> rootCommands(MinecraftServer server) {
        List<String> commands = new ArrayList<>();
        for (CommandNode<?> node : server.getCommands().getDispatcher().getRoot().getChildren()) {
            commands.add(node.getName());
        }
        return commands;
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

    private static String metadataVersion(String modId) {
        return FabricLoader.getInstance().getModContainer(modId)
                .map(ModContainer::getMetadata)
                .map(meta -> meta.getVersion().getFriendlyString())
                .orElse("unknown");
    }
}
