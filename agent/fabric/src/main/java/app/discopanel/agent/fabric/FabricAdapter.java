package app.discopanel.agent.fabric;

import app.discopanel.agent.core.PlatformAdapter;
import net.minecraft.network.chat.Component;
import net.minecraft.server.MinecraftServer;

/** Fabric-side callbacks; hops onto the server thread for game mutations. */
final class FabricAdapter implements PlatformAdapter {
    volatile MinecraftServer server;

    @Override
    public void broadcastChat(String sender, String message) {
        MinecraftServer current = server;
        if (current == null) {
            return;
        }
        current.execute(() -> current.getPlayerList()
                .broadcastSystemMessage(Component.literal("<" + sender + "> " + message), false));
    }
}
