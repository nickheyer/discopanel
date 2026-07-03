package app.discopanel.agent.paper;

import app.discopanel.agent.core.AgentCore;
import app.discopanel.agent.core.PlatformAdapter;
import app.discopanel.agent.core.PlatformInfo;
import app.discopanel.agent.proto.AgentProto;
import com.destroystokyo.paper.event.server.ServerTickEndEvent;
import com.destroystokyo.paper.event.server.ServerTickStartEvent;
import io.papermc.paper.advancement.AdvancementDisplay;
import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.serializer.plain.PlainTextComponentSerializer;
import org.bukkit.Bukkit;
import org.bukkit.World;
import org.bukkit.command.Command;
import org.bukkit.entity.Player;
import org.bukkit.event.EventHandler;
import org.bukkit.event.Listener;
import org.bukkit.event.entity.PlayerDeathEvent;
import org.bukkit.event.player.AsyncPlayerChatEvent;
import org.bukkit.event.player.PlayerAdvancementDoneEvent;
import org.bukkit.event.player.PlayerJoinEvent;
import org.bukkit.event.player.PlayerQuitEvent;
import org.bukkit.plugin.java.JavaPlugin;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/** Paper/Spigot shim: wires Bukkit and Paper server events into the agent core. */
public final class DiscoAgentPaper extends JavaPlugin implements Listener {
    private static final long WORLD_STATS_TICK_INTERVAL = 600L; // 30s at 20 TPS

    private AgentCore core;

    @Override
    public void onEnable() {
        PlatformAdapter adapter = (sender, message) ->
                Bukkit.getScheduler().runTask(this, () ->
                        Bukkit.getServer().sendMessage(Component.text("<" + sender + "> " + message)));
        core = AgentCore.start(new PlatformInfo("paper",
                Bukkit.getMinecraftVersion(), getDescription().getVersion()), adapter);
        if (core == null) {
            return;
        }

        Bukkit.getPluginManager().registerEvents(this, this);

        // The first scheduler tick runs after startup completes ("Done").
        Bukkit.getScheduler().runTask(this, () -> {
            core.sendReady();
            core.sendCommandList(commandNames());
        });
        Bukkit.getScheduler().runTaskTimer(this, this::sendWorldStats,
                WORLD_STATS_TICK_INTERVAL, WORLD_STATS_TICK_INTERVAL);
    }

    @Override
    public void onDisable() {
        if (core != null) {
            core.shutdown();
        }
    }

    @EventHandler
    public void onTickStart(ServerTickStartEvent event) {
        core.tickStart();
    }

    @EventHandler
    public void onTickEnd(ServerTickEndEvent event) {
        core.tickEnd();
    }

    @EventHandler
    public void onJoin(PlayerJoinEvent event) {
        Player player = event.getPlayer();
        core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_JOIN,
                player.getName(), player.getUniqueId().toString(), "",
                Bukkit.getOnlinePlayers().size());
    }

    @EventHandler
    public void onQuit(PlayerQuitEvent event) {
        Player player = event.getPlayer();
        core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_LEAVE,
                player.getName(), player.getUniqueId().toString(), "", -1);
    }

    @EventHandler
    @SuppressWarnings("deprecation") // String death messages for wide version compat
    public void onDeath(PlayerDeathEvent event) {
        Player player = event.getEntity();
        String message = event.getDeathMessage() == null ? "" : event.getDeathMessage();
        core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_DEATH,
                player.getName(), player.getUniqueId().toString(), message, -1);
    }

    @EventHandler
    public void onAdvancement(PlayerAdvancementDoneEvent event) {
        AdvancementDisplay display = event.getAdvancement().getDisplay();
        // Only real advancements have display info; recipe unlocks do not.
        if (display == null) {
            return;
        }
        Player player = event.getPlayer();
        core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_ADVANCEMENT,
                player.getName(), player.getUniqueId().toString(),
                PlainTextComponentSerializer.plainText().serialize(display.title()), -1);
    }

    @EventHandler
    @SuppressWarnings("deprecation") // AsyncPlayerChatEvent works on every Bukkit lineage version
    public void onChat(AsyncPlayerChatEvent event) {
        Player player = event.getPlayer();
        core.sendPlayerEvent(AgentProto.PlayerEventType.PLAYER_EVENT_TYPE_CHAT,
                player.getName(), player.getUniqueId().toString(), event.getMessage(), -1);
    }

    private void sendWorldStats() {
        List<AgentProto.DimensionStats> dimensions = new ArrayList<>();
        for (World world : Bukkit.getWorlds()) {
            dimensions.add(AgentProto.DimensionStats.newBuilder()
                    .setDimension(world.getName())
                    .setEntities(world.getEntities().size())
                    .setChunks(world.getLoadedChunks().length)
                    .setPlayers(world.getPlayers().size())
                    .build());
        }
        List<String> names = new ArrayList<>();
        for (Player player : Bukkit.getOnlinePlayers()) {
            names.add(player.getName());
        }
        core.sendWorldStats(dimensions, names);
    }

    private static List<String> commandNames() {
        List<String> names = new ArrayList<>();
        for (Map.Entry<String, Command> entry : Bukkit.getCommandMap().getKnownCommands().entrySet()) {
            // Skip namespaced duplicates like "minecraft:tp".
            if (!entry.getKey().contains(":")) {
                names.add(entry.getKey());
            }
        }
        return names;
    }
}
