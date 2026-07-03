package app.discopanel.agent.core;

/**
 * Callbacks the loader shim provides to the core. Implementations must hop to
 * the server thread themselves; the core invokes these from IO threads.
 */
public interface PlatformAdapter {
    /** Broadcast a chat line in game (panel/Discord originated). */
    void broadcastChat(String sender, String message);
}
