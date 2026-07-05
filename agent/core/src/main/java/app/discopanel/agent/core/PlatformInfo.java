package app.discopanel.agent.core;

/** Identity of the platform shim, reported in the hello message. */
public final class PlatformInfo {
    public final String loader;
    public final String mcVersion;
    public final String agentVersion;

    public PlatformInfo(String loader, String mcVersion, String agentVersion) {
        this.loader = loader;
        this.mcVersion = mcVersion;
        this.agentVersion = agentVersion;
    }
}
