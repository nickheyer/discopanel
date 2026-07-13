package db

import (
	"fmt"
	"time"

	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

type ServerStatus string

const (
	StatusStopped      ServerStatus = "stopped"
	StatusStarting     ServerStatus = "starting"
	StatusRunning      ServerStatus = "running"
	StatusStopping     ServerStatus = "stopping"
	StatusError        ServerStatus = "error"
	StatusUnhealthy    ServerStatus = "unhealthy"
	StatusCreating     ServerStatus = "creating"     // Container is being created/image pulled
	StatusProvisioning ServerStatus = "provisioning" // Server files are being installed/updated
	StatusPaused       ServerStatus = "paused"       // Container paused by autopause, wakes on connect
)

type ModLoader string

const (
	// Vanilla
	ModLoaderVanilla ModLoader = "vanilla"

	// Forge-based
	ModLoaderForge    ModLoader = "forge"
	ModLoaderNeoForge ModLoader = "neoforge"

	// Fabric-based
	ModLoaderFabric ModLoader = "fabric"
	ModLoaderQuilt  ModLoader = "quilt"

	// Bukkit-based
	ModLoaderBukkit ModLoader = "bukkit"
	ModLoaderSpigot ModLoader = "spigot"

	// Paper-based
	ModLoaderPaper      ModLoader = "paper"
	ModLoaderPurpur     ModLoader = "purpur"
	ModLoaderPufferfish ModLoader = "pufferfish"
	ModLoaderFolia      ModLoader = "folia"

	// Hybrids (Forge + Bukkit)
	ModLoaderMagma           ModLoader = "magma"
	ModLoaderMagmaMaintained ModLoader = "magma_maintained"
	ModLoaderKetting         ModLoader = "ketting"
	ModLoaderMohist          ModLoader = "mohist"
	ModLoaderYouer           ModLoader = "youer"
	ModLoaderBanner          ModLoader = "banner"
	ModLoaderCatserver       ModLoader = "catserver"
	ModLoaderArclight        ModLoader = "arclight"

	// Sponge
	ModLoaderSpongeVanilla ModLoader = "spongevanilla"
	ModLoaderSpongeForge   ModLoader = "spongeforge"

	// Others
	ModLoaderLimbo     ModLoader = "limbo"
	ModLoaderNanoLimbo ModLoader = "nanolimbo"
	ModLoaderCrucible  ModLoader = "crucible"
	ModLoaderGlowstone ModLoader = "glowstone"
	ModLoaderCustom    ModLoader = "custom"

	// Modpack Platforms
	ModLoaderAutoCurseForge ModLoader = "auto_curseforge"
	ModLoaderCurseForge     ModLoader = "curseforge"
	ModLoaderFTBA           ModLoader = "ftba"
	ModLoaderModrinth       ModLoader = "modrinth"
)

type Server struct {
	ID              string       `json:"id" gorm:"primaryKey"`
	Name            string       `json:"name" gorm:"not null"`
	Description     string       `json:"description"`
	ModLoader       ModLoader    `json:"mod_loader" gorm:"not null"`
	MCVersion       string       `json:"mc_version" gorm:"not null;column:mc_version"`
	ContainerID     string       `json:"container_id" gorm:"column:container_id"`
	Status          ServerStatus `json:"status" gorm:"not null"`
	Port            int          `json:"port"`
	ProxyPort       int          `json:"proxy_port" gorm:"column:proxy_port"`
	ProxyHostname   string       `json:"proxy_hostname" gorm:"column:proxy_hostname;uniqueIndex:idx_proxy_hostname_listener,where:proxy_hostname != ''"`
	ProxyListenerID string       `json:"proxy_listener_id" gorm:"column:proxy_listener_id;uniqueIndex:idx_proxy_hostname_listener,where:proxy_listener_id != ''"` // Which listener this server uses
	MaxPlayers      int          `json:"max_players" gorm:"default:20;column:max_players"`
	Memory          int          `json:"memory" gorm:"default:4096"`                    // Container memory limit in MB
	MemoryMin       int          `json:"memory_min" gorm:"default:0;column:memory_min"` // JVM initial heap in MB
	MemoryMax       int          `json:"memory_max" gorm:"default:0;column:memory_max"` // JVM max heap in MB
	CreatedAt       time.Time    `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time    `json:"updated_at" gorm:"autoUpdateTime"`
	LastStarted     *time.Time   `json:"last_started" gorm:"column:last_started"`
	JavaVersion     string       `json:"java_version" gorm:"column:java_version"`
	DockerImage     string       `json:"docker_image" gorm:"column:docker_image"`
	DataPath        string       `json:"data_path" gorm:"not null;column:data_path"`
	Detached        bool         `json:"detached" gorm:"default:false;column:detached"`     // Detach server container from DiscoPanel lifecycle
	AutoStart       bool         `json:"auto_start" gorm:"default:false;column:auto_start"` // Start server when DiscoPanel starts
	AgentTokenHash  string       `json:"-" gorm:"column:agent_token_hash"`                  // SHA-256 of the runtime agent's bearer token
	RuntimeDigest   string       `json:"runtime_digest" gorm:"column:runtime_digest"`       // Image digest recorded at container create
	IconSource      string       `json:"icon_source" gorm:"column:icon_source"`             // Who wrote server-icon.png (upload or modpack)

	AdditionalPorts []*v1.AdditionalPort `json:"additional_ports" gorm:"column:additional_ports;serializer:json"`           // Additional port configurations
	DockerOverrides *v1.DockerOverrides  `json:"docker_overrides" gorm:"column:docker_overrides;type:text;serializer:json"` // Docker container overrides

	// Runtime stats (not persisted to DB)
	ContainerPort int     `json:"container_port" gorm:"-"` // In-container listen port, alias computed
	MemoryUsage   float64 `json:"memory_usage" gorm:"-"`   // Current memory usage in MB
	CPUPercent    float64 `json:"cpu_percent" gorm:"-"`    // Current CPU usage percentage (docker stats scale, 100 per core)
	CPUCores      int     `json:"cpu_cores" gorm:"-"`      // CPU cores visible to the container
	DiskUsage     int64   `json:"disk_usage" gorm:"-"`     // Total server data size in bytes
	DiskTotal     int64   `json:"disk_total" gorm:"-"`     // Total disk space available in bytes
	DiskUsed      int64   `json:"disk_used" gorm:"-"`      // Volume used bytes across all data
	WorldSize     int64   `json:"world_size" gorm:"-"`     // World directory size in bytes
	PlayersOnline int     `json:"players_online" gorm:"-"` // Current players online
	TPS           float64 `json:"tps" gorm:"-"`            // Current TPS (20 is optimal)

	// SLP runtime stats (not persisted to DB)
	SLPAvailable    bool     `json:"slp_available" gorm:"-"`
	SLPLatencyMs    int64    `json:"slp_latency_ms" gorm:"-"`
	MOTD            string   `json:"motd" gorm:"-"`
	ServerVersion   string   `json:"server_version" gorm:"-"`
	ProtocolVersion int      `json:"protocol_version" gorm:"-"`
	PlayerSample    []string `json:"player_sample" gorm:"-"`
	MaxPlayersSLP   int      `json:"max_players_slp" gorm:"-"` // Actual from SLP (MaxPlayers field is config)
	Favicon         string   `json:"favicon" gorm:"-"`         // Data URI of server-icon.png

	// Agent-sourced runtime stats (not persisted to DB)
	AgentConnected     bool    `json:"agent_connected" gorm:"-"`
	MSPT               float64 `json:"mspt" gorm:"-"`                 // Mean ms per tick (agent-sourced)
	HeapUsedMB         float64 `json:"heap_used_mb" gorm:"-"`         // JVM used heap
	HeapMaxMB          float64 `json:"heap_max_mb" gorm:"-"`          // JVM max heap
	CPUThrottlePercent float64 `json:"cpu_throttle_percent" gorm:"-"` // Share of CFS periods throttled
	ClassCount         int     `json:"class_count" gorm:"-"`          // Loaded JVM classes
}

// Icon provenance values, uploads always win over pack art
const (
	IconSourceUpload  = "upload"
	IconSourceModpack = "modpack"
)

const MinecraftDefaultPort = 25565

// Port the server listens on inside its container
func (s *Server) InContainerPort() int {
	if s.ProxyHostname != "" {
		return MinecraftDefaultPort
	}
	return s.Port
}

// Returns default JVM heap sizing for a container limit
func DefaultHeapForMemory(memoryMB int) (initMB, maxMB int) {
	return memoryMB / 2, memoryMB * 3 / 4
}

// Mirrors server heap sizing into read-only properties
func (c *ServerProperties) SyncMemoryFromServer(server *Server) {
	initMB, maxMB := server.MemoryMin, server.MemoryMax
	defInit, defMax := DefaultHeapForMemory(server.Memory)
	if initMB <= 0 {
		initMB = defInit
	}
	if maxMB <= 0 {
		maxMB = defMax
	}
	initStr := fmt.Sprintf("%dM", initMB)
	maxStr := fmt.Sprintf("%dM", maxMB)
	c.InitMemory = &initStr
	c.MaxMemory = &maxStr
}

// One telemetry point sampled from the live collector
type MetricsSample struct {
	ID         uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	ServerID   string    `json:"server_id" gorm:"column:server_id;not null;index:idx_metrics_lookup,priority:1"`
	Resolution int       `json:"resolution" gorm:"column:resolution;not null;default:0;index:idx_metrics_lookup,priority:2"` // Bucket seconds, 0 means raw
	Timestamp  time.Time `json:"timestamp" gorm:"column:timestamp;not null;index:idx_metrics_lookup,priority:3"`
	TPS        float64   `json:"tps" gorm:"column:tps"`
	MSPT       float64   `json:"mspt" gorm:"column:mspt"`
	Players    int       `json:"players" gorm:"column:players"`
	CPUPercent float64   `json:"cpu_percent" gorm:"column:cpu_percent"`
	MemoryMB   float64   `json:"memory_mb" gorm:"column:memory_mb"`
	HeapUsedMB float64   `json:"heap_used_mb" gorm:"column:heap_used_mb"`
	DiskBytes  int64     `json:"disk_bytes" gorm:"column:disk_bytes"`

	// Proxy traffic, conns gauge and per window deltas
	ProxyActiveConns int64 `json:"proxy_active_conns" gorm:"column:proxy_active_conns"`
	ProxyBytesIn     int64 `json:"proxy_bytes_in" gorm:"column:proxy_bytes_in"`
	ProxyBytesOut    int64 `json:"proxy_bytes_out" gorm:"column:proxy_bytes_out"`
	ProxyLogins      int64 `json:"proxy_logins" gorm:"column:proxy_logins"`
}

type ServerProperties struct {
	// Global settings reuse this table under a pseudo server id
	ID        string    `json:"id" gorm:"primaryKey"`
	ServerID  string    `json:"server_id" gorm:"not null;index;column:server_id"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`

	// JVM and runtime settings passed as environment variables
	UID             *int    `json:"uid" env:"UID" default:"1000" desc:"The linux user id to run as" input:"number" label:"User ID" category:"jvm"`
	GID             *int    `json:"gid" env:"GID" default:"1000" desc:"The linux group id to run as" input:"number" label:"Group ID" category:"jvm"`
	InitMemory      *string `json:"initMemory" env:"INIT_MEMORY" default:"" desc:"Initial Java heap (Xms), computed from the server memory settings" input:"text" label:"Initial Memory" system:"true" category:"jvm"`
	MaxMemory       *string `json:"maxMemory" env:"MAX_MEMORY" default:"" desc:"Maximum Java heap (Xmx), computed from the server memory settings" input:"text" label:"Maximum Memory" system:"true" category:"jvm"`
	TZ              *string `json:"tz" env:"TZ" default:"UTC" desc:"Timezone configuration" input:"text" label:"Timezone" category:"jvm"`
	EnableJMX       *bool   `json:"enableJmx" env:"ENABLE_JMX" default:"false" desc:"Enable remote JMX for profiling (port 7091)" input:"checkbox" label:"Enable JMX" category:"jvm"`
	EnableAgent     *bool   `json:"enableAgent" default:"true" desc:"Enable the DiscoPanel agent (live telemetry, console during boot, crash reporting; attaches a telemetry javaagent to the server JVM, any loader or version)" input:"checkbox" label:"Enable DiscoPanel Agent" category:"jvm"`
	JMXHost         *string `json:"jmxHost" env:"JMX_HOST" default:"" desc:"IP/host running the Docker container for JMX" input:"text" label:"JMX Host" category:"jvm"`
	UseAikarFlags   *bool   `json:"useAikarFlags" env:"USE_AIKAR_FLAGS" default:"true" desc:"Use Aikar's optimized JVM flags for GC tuning (applied by default unless disabled or MeowIce flags are enabled)" input:"checkbox" label:"Use Aikar Flags" category:"jvm"`
	UseMeowiceFlags *bool   `json:"useMeowiceFlags" env:"USE_MEOWICE_FLAGS" default:"false" desc:"Use MeowIce's JVM flags optimized for Java 17+" input:"checkbox" label:"Use MeowIce Flags" category:"jvm"`
	UseZGCFlags     *bool   `json:"useZgcFlags" env:"USE_ZGC_FLAGS" default:"false" desc:"Use generational ZGC instead of G1 (Java 21+, sub-millisecond pauses; often better for large modpacks)" input:"checkbox" label:"Use ZGC Flags" category:"jvm"`
	UseFlareFlags   *bool   `json:"useFlareFlags" env:"USE_FLARE_FLAGS" default:"false" desc:"Enable JVM flags for Flare profiling suite" input:"checkbox" label:"Use Flare Flags" category:"jvm"`
	UseSimdFlags    *bool   `json:"useSimdFlags" env:"USE_SIMD_FLAGS" default:"false" desc:"Support for optimized SIMD operations (Java 16+)" input:"checkbox" label:"Use SIMD Flags" category:"jvm"`
	JVMOpts         *string `json:"jvmOpts" env:"JVM_OPTS" default:"" desc:"General JVM options" input:"text" label:"JVM Options" category:"jvm"`
	JVMXXOpts       *string `json:"jvmXxOpts" env:"JVM_XX_OPTS" default:"" desc:"JVM -XX options" input:"text" label:"JVM XX Options" category:"jvm"`
	JVMDDOpts       *string `json:"jvmDdOpts" env:"JVM_DD_OPTS" default:"" desc:"Comma separated list of system properties as name=value pairs" input:"text" label:"JVM DD Options" category:"jvm"`
	ExtraArgs       *string `json:"extraArgs" env:"EXTRA_ARGS" default:"" desc:"Arguments passed to the server after the jar/args file" input:"text" label:"Extra Arguments" category:"jvm"`

	// Provisioning (consumed by the DiscoPanel provisioner when preparing server files)
	EULA           *string `json:"eula" default:"TRUE" desc:"This MUST be set to TRUE" input:"checkbox" required:"true" label:"Accept EULA" system:"true" category:"server"`
	CustomServer   *string `json:"customServer" default:"" desc:"URL or data-dir path to a custom server jar" input:"text" label:"Custom Server JAR" category:"server"`
	CustomJarExec  *string `json:"customJarExec" default:"" desc:"Custom jar execution command (e.g. -cp classpath MainClass or -jar file.jar)" input:"text" label:"Custom JAR Execution" category:"server"`
	Icon           *string `json:"icon" default:"" desc:"URL to a server icon image (converted to server-icon.png)" input:"text" label:"Server Icon" category:"server"`
	OverrideIcon   *bool   `json:"overrideIcon" default:"false" desc:"Override existing server icon" input:"checkbox" label:"Override Icon" category:"server"`
	ForceProvision *bool   `json:"forceProvision" default:"false" desc:"Force full re-provisioning of server files on next start (cleared after start)" input:"checkbox" label:"Force Re-Provision" ephemeral:"true" category:"server"`

	// Written to server.properties by the provisioner before start
	MOTD                           *string `json:"motd" prop:"motd" default:"" desc:"Server log in message" input:"text" label:"Message of the Day" category:"server"`
	Difficulty                     *string `json:"difficulty" prop:"difficulty" default:"easy" desc:"Difficulty level (peaceful,easy,normal,hard)" input:"select" label:"Difficulty" category:"game"`
	MaxPlayers                     *int    `json:"maxPlayers" prop:"max-players" default:"20" desc:"Maximum number of players" input:"number" label:"Max Players" system:"true" category:"game"`
	MaxWorldSize                   *int    `json:"maxWorldSize" prop:"max-world-size" default:"0" desc:"Maximum world size in blocks (radius)" input:"number" label:"Max World Size" category:"world"`
	AllowNether                    *bool   `json:"allowNether" prop:"allow-nether" default:"true" desc:"Allow players to travel to the Nether" input:"checkbox" label:"Allow Nether" category:"game"`
	AnnouncePlayerAchievements     *bool   `json:"announcePlayerAchievements" prop:"announce-player-achievements" default:"true" desc:"Announce player achievements" input:"checkbox" label:"Announce Player Achievements" category:"game"`
	EnableCommandBlock             *bool   `json:"enableCommandBlock" prop:"enable-command-block" default:"false" desc:"Enable command blocks" input:"checkbox" label:"Enable Command Blocks" category:"game"`
	ForceGamemode                  *bool   `json:"forceGamemode" prop:"force-gamemode" default:"false" desc:"Force players to join in default game mode" input:"checkbox" label:"Force Gamemode" category:"game"`
	GenerateStructures             *bool   `json:"generateStructures" prop:"generate-structures" default:"true" desc:"Generate structures like villages" input:"checkbox" label:"Generate Structures" category:"world"`
	Hardcore                       *bool   `json:"hardcore" prop:"hardcore" default:"false" desc:"Players set to spectator mode on death" input:"checkbox" label:"Hardcore Mode" category:"game"`
	SnooperEnabled                 *bool   `json:"snooperEnabled" prop:"snooper-enabled" default:"false" desc:"Send data to snoop.minecraft.net (legacy versions)" input:"checkbox" label:"Enable Snooper" category:"game"`
	MaxBuildHeight                 *int    `json:"maxBuildHeight" prop:"max-build-height" default:"256" desc:"Maximum building height (legacy versions)" input:"number" label:"Max Build Height" category:"game"`
	SpawnAnimals                   *bool   `json:"spawnAnimals" prop:"spawn-animals" default:"true" desc:"Allow animals to spawn" input:"checkbox" label:"Spawn Animals" category:"game"`
	SpawnMonsters                  *bool   `json:"spawnMonsters" prop:"spawn-monsters" default:"true" desc:"Allow monsters to spawn" input:"checkbox" label:"Spawn Monsters" category:"game"`
	SpawnNPCs                      *bool   `json:"spawnNpcs" prop:"spawn-npcs" default:"true" desc:"Allow villagers to spawn" input:"checkbox" label:"Spawn NPCs" category:"game"`
	SpawnProtection                *int    `json:"spawnProtection" prop:"spawn-protection" default:"0" desc:"Area that non-ops cannot edit (0 to disable)" input:"number" label:"Spawn Protection" category:"game"`
	ViewDistance                   *int    `json:"viewDistance" prop:"view-distance" default:"0" desc:"Server-side viewing distance in chunks" input:"number" label:"View Distance" category:"game"`
	Seed                           *string `json:"seed" prop:"level-seed" default:"" desc:"World generation seed" input:"text" label:"World Seed" category:"world"`
	Mode                           *string `json:"mode" prop:"gamemode" default:"survival" desc:"Game mode (creative,survival,adventure,spectator)" input:"select" label:"Game Mode" category:"game"`
	PVP                            *bool   `json:"pvp" prop:"pvp" default:"true" desc:"Enable player-vs-player mode" input:"checkbox" label:"Enable PVP" category:"game"`
	LevelType                      *string `json:"levelType" prop:"level-type" default:"minecraft:default" desc:"World generation type" input:"text" label:"Level Type" category:"world"`
	GeneratorSettings              *string `json:"generatorSettings" prop:"generator-settings" default:"" desc:"Additional world generation settings" input:"text" label:"Generator Settings" category:"world"`
	Level                          *string `json:"level" prop:"level-name" default:"world" desc:"World save name" input:"text" label:"World Name" category:"world"`
	OnlineMode                     *bool   `json:"onlineMode" prop:"online-mode" default:"true" desc:"Authenticate players against Minecraft account database" input:"checkbox" label:"Online Mode" category:"game"`
	AllowFlight                    *bool   `json:"allowFlight" prop:"allow-flight" default:"false" desc:"Allow flight in survival mode with mods" input:"checkbox" label:"Allow Flight" category:"game"`
	ServerName                     *string `json:"serverName" prop:"server-name" default:"" desc:"The server name" input:"text" label:"Server Name" category:"server"`
	ServerPort                     *int    `json:"serverPort" prop:"server-port" default:"25565" desc:"Server port (managed by DiscoPanel)" input:"number" label:"Server Port" system:"true" category:"server"`
	PlayerIdleTimeout              *int    `json:"playerIdleTimeout" prop:"player-idle-timeout" default:"0" desc:"Player idle timeout" input:"number" label:"Player Idle Timeout" category:"game"`
	SyncChunkWrites                *bool   `json:"syncChunkWrites" prop:"sync-chunk-writes" default:"true" desc:"Sync chunk writes" input:"checkbox" label:"Sync Chunk Writes" category:"game"`
	EnableStatus                   *bool   `json:"enableStatus" prop:"enable-status" default:"true" desc:"Enable server status" input:"checkbox" label:"Enable Status" category:"game"`
	EntityBroadcastRangePercentage *int    `json:"entityBroadcastRangePercentage" prop:"entity-broadcast-range-percentage" default:"0" desc:"Entity broadcast range percentage" input:"number" label:"Entity Broadcast Range Percentage" category:"game"`
	FunctionPermissionLevel        *int    `json:"functionPermissionLevel" prop:"function-permission-level" default:"0" desc:"Function permission level" input:"number" label:"Function Permission Level" category:"game"`
	NetworkCompressionThreshold    *int    `json:"networkCompressionThreshold" prop:"network-compression-threshold" default:"0" desc:"Network compression threshold" input:"number" label:"Network Compression Threshold" category:"game"`
	OpPermissionLevel              *int    `json:"opPermissionLevel" prop:"op-permission-level" default:"0" desc:"OP permission level" input:"number" label:"OP Permission Level" category:"game"`
	PreventProxyConnections        *bool   `json:"preventProxyConnections" prop:"prevent-proxy-connections" default:"false" desc:"Prevent proxy connections" input:"checkbox" label:"Prevent Proxy Connections" category:"game"`
	UseNativeTransport             *bool   `json:"useNativeTransport" prop:"use-native-transport" default:"true" desc:"Use native transport" input:"checkbox" label:"Use Native Transport" category:"game"`
	SimulationDistance             *int    `json:"simulationDistance" prop:"simulation-distance" default:"0" desc:"Simulation distance" input:"number" label:"Simulation Distance" category:"game"`
	EnableQuery                    *bool   `json:"enableQuery" prop:"enable-query" default:"true" desc:"Enable GameSpy query protocol" input:"checkbox" label:"Enable Query" category:"game"`
	QueryPort                      *int    `json:"queryPort" prop:"query.port" default:"25565" desc:"UDP port for GameSpy query" input:"number" label:"Query Port" category:"game"`
	AcceptsTransfers               *bool   `json:"acceptsTransfers" prop:"accepts-transfers" default:"false" desc:"Allow player transfers between servers" input:"checkbox" label:"Accepts Transfers" category:"game"`
	BroadcastConsoleToOps          *bool   `json:"broadcastConsoleToOps" prop:"broadcast-console-to-ops" default:"true" desc:"Broadcast console messages to ops" input:"checkbox" label:"Broadcast Console to OPs" category:"game"`
	BugReportLink                  *string `json:"bugReportLink" prop:"bug-report-link" default:"" desc:"Custom bug report URL" input:"text" label:"Bug Report Link" category:"server"`
	EnforceSecureProfile           *bool   `json:"enforceSecureProfile" prop:"enforce-secure-profile" default:"true" desc:"Require secure chat/profile" input:"checkbox" label:"Enforce Secure Profile" category:"game"`
	HideOnlinePlayers              *bool   `json:"hideOnlinePlayers" prop:"hide-online-players" default:"false" desc:"Hide online players from the server list" input:"checkbox" label:"Hide Online Players" category:"game"`
	LogIPs                         *bool   `json:"logIps" prop:"log-ips" default:"true" desc:"Log connecting player IPs" input:"checkbox" label:"Log Player IPs" category:"game"`
	MaxChainedNeighborUpdates      *int    `json:"maxChainedNeighborUpdates" prop:"max-chained-neighbor-updates" default:"1000000" desc:"Maximum chained neighbor updates" input:"number" label:"Max Chained Neighbor Updates" category:"game"`
	PauseWhenEmptySeconds          *int    `json:"pauseWhenEmptySeconds" prop:"pause-when-empty-seconds" default:"0" desc:"Pause game loop when server empty (seconds, 1.21.2+)" input:"number" label:"Pause When Empty" category:"game"`
	RateLimit                      *int    `json:"rateLimit" prop:"rate-limit" default:"0" desc:"Rate limit in packets per second" input:"number" label:"Rate Limit" category:"game"`
	RegionFileCompression          *string `json:"regionFileCompression" prop:"region-file-compression" default:"deflate" desc:"Compression type for region files" input:"text" label:"Region File Compression" category:"world"`
	ResourcePackID                 *string `json:"resourcePackId" prop:"resource-pack-id" default:"" desc:"Custom resource pack ID" input:"text" label:"Resource Pack ID" category:"resourcepack"`
	ResourcePackPrompt             *string `json:"resourcePackPrompt" prop:"resource-pack-prompt" default:"" desc:"Prompt shown when resource pack offered" input:"text" label:"Resource Pack Prompt" category:"resourcepack"`
	StatusHeartbeatInterval        *int    `json:"statusHeartbeatInterval" prop:"status-heartbeat-interval" default:"0" desc:"Status heartbeat interval (ms)" input:"number" label:"Status Heartbeat Interval" category:"game"`
	ServerPropertiesEscapeUnicode  *bool   `json:"serverPropertiesEscapeUnicode" default:"false" desc:"Escape unicode in server.properties (pre-1.20 compatibility)" input:"checkbox" label:"Escape Unicode in Server Properties" category:"server"`
	CustomServerProperties         *string `json:"customServerProperties" default:"" desc:"Extra newline delimited name=value pairs to be added to \"server.properties\"" input:"text" label:"Custom Server Properties" category:"server"`

	// Shutdown behavior (panel-side stop flow)
	StopDuration            *int `json:"stopDuration" default:"60" desc:"Seconds to wait for graceful shutdown before force kill" input:"number" label:"Stop Duration" category:"server"`
	StopServerAnnounceDelay *int `json:"stopServerAnnounceDelay" default:"0" desc:"Seconds between in-game shutdown announcement and stop" input:"number" label:"Stop Server Announce Delay" category:"server"`

	// Custom Resource Pack
	ResourcePack        *string `json:"resourcePack" prop:"resource-pack" default:"" desc:"Link to custom resource pack" input:"text" label:"Resource Pack URL" category:"resourcepack"`
	ResourcePackSHA1    *string `json:"resourcePackSha1" prop:"resource-pack-sha1" default:"" desc:"Checksum for custom resource pack" input:"text" label:"Resource Pack SHA1" category:"resourcepack"`
	ResourcePackEnforce *bool   `json:"resourcePackEnforce" prop:"require-resource-pack" default:"false" desc:"Enforce resource pack on clients" input:"checkbox" label:"Enforce Resource Pack" category:"resourcepack"`

	// Management Server (vanilla 1.21.9+ server.properties)
	ManagementServerAllowedOrigins      *string `json:"managementServerAllowedOrigins" prop:"management-server-allowed-origins" default:"" desc:"Allowed CORS origins for management server" input:"text" label:"Management Server Allowed Origins" category:"management"`
	ManagementServerEnabled             *bool   `json:"managementServerEnabled" prop:"management-server-enabled" default:"false" desc:"Enable management server interface" input:"checkbox" label:"Enable Management Server" category:"management"`
	ManagementServerHost                *string `json:"managementServerHost" prop:"management-server-host" default:"0.0.0.0" desc:"Host address for management server" input:"text" label:"Management Server Host" category:"management"`
	ManagementServerPort                *int    `json:"managementServerPort" prop:"management-server-port" default:"0" desc:"Port for management server" input:"number" label:"Management Server Port" category:"management"`
	ManagementServerSecret              *string `json:"managementServerSecret" prop:"management-server-secret" default:"" desc:"Shared secret for management server authentication" input:"password" label:"Management Server Secret" category:"management"`
	ManagementServerTLSEnabled          *bool   `json:"managementServerTlsEnabled" prop:"management-server-tls-enabled" default:"false" desc:"Enable TLS for management server" input:"checkbox" label:"Management Server TLS Enabled" category:"management"`
	ManagementServerTLSKeystore         *string `json:"managementServerTlsKeystore" prop:"management-server-tls-keystore" default:"" desc:"Path to TLS keystore" input:"text" label:"Management Server TLS Keystore" category:"management"`
	ManagementServerTLSKeystorePassword *string `json:"managementServerTlsKeystorePassword" prop:"management-server-tls-keystore-password" default:"" desc:"Password for TLS keystore" input:"password" label:"Management Server TLS Keystore Password" category:"management"`

	// Ops / Admins (provisioner writes ops.json, resolving usernames to UUIDs)
	Ops *string `json:"ops" default:"" desc:"Comma-separated list of operator usernames/UUIDs" input:"text" label:"Operators" category:"ops"`

	// Whitelist (provisioner writes whitelist.json, resolving usernames to UUIDs)
	EnableWhitelist   *bool   `json:"enableWhitelist" prop:"white-list" default:"false" desc:"Enable server whitelist" input:"checkbox" label:"Enable Whitelist" category:"whitelist"`
	Whitelist         *string `json:"whitelist" default:"" desc:"Comma-separated list of usernames/UUIDs" input:"text" label:"Whitelist Players" category:"whitelist"`
	OverrideWhitelist *bool   `json:"overrideWhitelist" default:"false" desc:"Regenerate whitelist on each startup" input:"checkbox" label:"Override Whitelist" category:"whitelist"`
	EnforceWhitelist  *bool   `json:"enforceWhitelist" prop:"enforce-whitelist" default:"false" desc:"Enforce whitelist changes immediately" input:"checkbox" label:"Enforce Whitelist" category:"whitelist"`

	// RCON
	EnableRCON             *bool   `json:"enableRcon" prop:"enable-rcon" default:"true" desc:"Enable RCON support (required for console, metrics and backups)" input:"checkbox" label:"Enable RCON" category:"rcon"`
	RCONPassword           *string `json:"rconPassword" prop:"rcon.password" default:"" desc:"RCON password (MUST be changed)" input:"password" required:"true" label:"RCON Password" category:"rcon"`
	RCONPort               *int    `json:"rconPort" prop:"rcon.port" default:"25575" desc:"RCON port" input:"number" label:"RCON Port" category:"rcon"`
	BroadcastRCONToOps     *bool   `json:"broadcastRconToOps" prop:"broadcast-rcon-to-ops" default:"false" desc:"Broadcast RCON to ops" input:"checkbox" label:"Broadcast RCON to OPs" category:"rcon"`
	RCONCmdsStartup        *string `json:"rconCmdsStartup" default:"" desc:"RCON commands to execute when the server becomes healthy" input:"text" label:"RCON Commands on Startup" category:"rcon"`
	RCONCmdsOnConnect      *string `json:"rconCmdsOnConnect" default:"" desc:"RCON commands to execute on client connect" input:"text" label:"RCON Commands on Connect" category:"rcon"`
	RCONCmdsFirstConnect   *string `json:"rconCmdsFirstConnect" default:"" desc:"RCON commands to execute on first client connect" input:"text" label:"RCON Commands on First Connect" category:"rcon"`
	RCONCmdsOnDisconnect   *string `json:"rconCmdsOnDisconnect" default:"" desc:"RCON commands to execute on client disconnect" input:"text" label:"RCON Commands on Disconnect" category:"rcon"`
	RCONCmdsLastDisconnect *string `json:"rconCmdsLastDisconnect" default:"" desc:"RCON commands to execute on last client disconnect" input:"text" label:"RCON Commands on Last Disconnect" category:"rcon"`

	// Auto-Pause, panel-side, requires server behind the DiscoPanel proxy
	EnableAutopause      *bool `json:"enableAutopause" default:"false" desc:"Pause the container when idle; wakes on player connect (proxied servers only)" input:"checkbox" label:"Enable Auto-Pause" category:"autopause"`
	AutopauseTimeoutEst  *int  `json:"autopauseTimeoutEst" default:"3600" desc:"Time between last disconnect and pausing (seconds)" input:"number" label:"Auto-Pause Timeout (Established)" category:"autopause"`
	AutopauseTimeoutInit *int  `json:"autopauseTimeoutInit" default:"600" desc:"Time between server start and pausing if no client connects (seconds)" input:"number" label:"Auto-Pause Timeout (Initial)" category:"autopause"`

	// Auto-Stop (panel-side)
	EnableAutostop      *bool `json:"enableAutostop" default:"false" desc:"Stop the server when idle" input:"checkbox" label:"Enable Auto-Stop" category:"autostop"`
	AutostopTimeoutEst  *int  `json:"autostopTimeoutEst" default:"3600" desc:"Time between last disconnect and stopping (seconds)" input:"number" label:"Auto-Stop Timeout (Established)" category:"autostop"`
	AutostopTimeoutInit *int  `json:"autostopTimeoutInit" default:"1800" desc:"Time between server start and stopping if no client connects (seconds)" input:"number" label:"Auto-Stop Timeout (Initial)" category:"autostop"`

	// Proxy (panel-side, requires the DiscoPanel proxy)
	EnableWakeOnConnect   *bool `json:"enableWakeOnConnect" default:"false" desc:"Keep this server joinable while stopped; joining starts it up (proxied servers only)" input:"checkbox" label:"Wake on Connect" category:"proxy"`
	EnableProxyProtocol   *bool `json:"enableProxyProtocol" default:"false" desc:"Send PROXY protocol v2 headers so the server sees real client IPs; the server software must expect proxy protocol (e.g. Paper's proxy-protocol setting) or connections will fail" input:"checkbox" label:"Send PROXY Protocol" category:"proxy"`
	ProxyPreserveHostname *bool `json:"proxyPreserveHostname" default:"false" desc:"Forward the hostname players connected with instead of rewriting it to localhost (needed by hostname-aware plugins and Floodgate)" input:"checkbox" label:"Preserve Client Hostname" category:"proxy"`

	// Forge / NeoForge
	ForgeVersion      *string `json:"forgeVersion" default:"" desc:"Specific Forge/NeoForge version to install" input:"text" label:"Forge Version" category:"curseforge"`
	ForgeInstaller    *string `json:"forgeInstaller" default:"" desc:"Data-dir path to a pre-downloaded Forge installer" input:"text" label:"Forge Installer" category:"curseforge"`
	ForgeInstallerURL *string `json:"forgeInstallerUrl" default:"" desc:"URL to download Forge installer" input:"text" label:"Forge Installer URL" category:"curseforge"`

	// CurseForge modpacks
	CFAPIKey           *string `json:"cfApiKey" default:"" desc:"CurseForge API Key (https://console.curseforge.com/#/api-keys)" input:"password" label:"CurseForge API Key" category:"curseforge"`
	CFPageURL          *string `json:"cfPageUrl" default:"" desc:"URL to modpack or specific file" input:"text" label:"CurseForge Page URL" category:"curseforge"`
	CFSlug             *string `json:"cfSlug" default:"" desc:"Modpack slug identifier" input:"text" label:"CurseForge Slug" category:"curseforge"`
	CFFileID           *string `json:"cfFileId" default:"" desc:"Modpack file numerical ID" input:"text" label:"CurseForge File ID" category:"curseforge"`
	CFModpackZip       *string `json:"cfModpackZip" default:"" desc:"Data-dir path to unpublished modpack zip" input:"text" label:"CurseForge Modpack Zip" category:"curseforge"`
	CFExcludeMods      *string `json:"cfExcludeMods" default:"" desc:"Comma/space delimited list of mod slugs/IDs to exclude" input:"text" label:"CurseForge Exclude Mods" category:"curseforge"`
	CFForceIncludeMods *string `json:"cfForceIncludeMods" default:"" desc:"Comma/space delimited list of mod slugs/IDs to include" input:"text" label:"CurseForge Force Include Mods" category:"curseforge"`

	// Modrinth modpacks and projects
	ModrinthModpack                    *string `json:"modrinthModpack" default:"" desc:"Modrinth modpack project slug, ID, or URL" input:"text" label:"Modrinth Modpack" category:"modrinth"`
	ModrinthModpackVersionType         *string `json:"modrinthModpackVersionType" default:"release" desc:"Version type for modpack (release, beta, alpha)" input:"select" label:"Modrinth Modpack Version Type" category:"modrinth"`
	ModrinthVersion                    *string `json:"modrinthVersion" default:"" desc:"Specific version ID or number" input:"text" label:"Modrinth Version" category:"modrinth"`
	ModrinthLoader                     *string `json:"modrinthLoader" default:"" desc:"Mod loader for narrowing versions (forge, neoforge, fabric, quilt)" input:"select" label:"Modrinth Loader" category:"modrinth"`
	ModrinthExcludeFiles               *string `json:"modrinthExcludeFiles" default:"" desc:"Comma or newline delimited list of partial file names to exclude" input:"text" label:"Exclude Files" category:"modrinth"`
	ModrinthForceIncludeFiles          *string `json:"modrinthForceIncludeFiles" default:"" desc:"Comma or newline delimited list of partial file names to force include" input:"text" label:"Force Include Files" category:"modrinth"`
	ModrinthProjects                   *string `json:"modrinthProjects" default:"" desc:"Comma or newline delimited list of Modrinth project slugs or IDs to install as mods" input:"text" label:"Modrinth Projects" category:"modrinth"`
	ModrinthDownloadDependencies       *string `json:"modrinthDownloadDependencies" default:"none" desc:"Dependency download mode (none, required, optional)" input:"select" label:"Modrinth Download Dependencies" category:"modrinth"`
	ModrinthProjectsDefaultVersionType *string `json:"modrinthProjectsDefaultVersionType" default:"release" desc:"Default version type to select (release, beta, alpha)" input:"select" label:"Modrinth Default Version Type" category:"modrinth"`
}

type Mod struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	ServerID    string    `json:"server_id" gorm:"not null;index;column:server_id"`
	Name        string    `json:"name" gorm:"not null"`
	FileName    string    `json:"file_name" gorm:"not null;column:file_name"`
	Version     string    `json:"version"`
	ModID       string    `json:"mod_id" gorm:"column:mod_id"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled" gorm:"default:true"`
	UploadedAt  time.Time `json:"uploaded_at" gorm:"autoCreateTime;column:uploaded_at"`
	FileSize    int64     `json:"file_size" gorm:"column:file_size"`
	Server      *Server   `json:"-" gorm:"foreignKey:ServerID;constraint:OnDelete:CASCADE"`
}

type IndexedModpack struct {
	ID            string    `json:"id" gorm:"primaryKey"`                      // ID format is indexer-originalId
	IndexerID     string    `json:"indexer_id" gorm:"index;column:indexer_id"` // Original ID from indexer
	Indexer       string    `json:"indexer" gorm:"index"`                      // Indexer name such as fuego or modrinth
	Name          string    `json:"name" gorm:"not null;index"`
	Slug          string    `json:"slug" gorm:"index"`
	Summary       string    `json:"summary"`
	Description   string    `json:"description" gorm:"type:text"`
	LogoURL       string    `json:"logo_url" gorm:"column:logo_url"`
	WebsiteURL    string    `json:"website_url" gorm:"column:website_url"`
	DownloadCount int64     `json:"download_count" gorm:"column:download_count"`
	Categories    string    `json:"categories"`    // JSON array stored as string
	GameVersions  string    `json:"game_versions"` // JSON array stored as string
	ModLoaders    string    `json:"mod_loaders"`   // JSON array stored as string
	LatestFileID  string    `json:"latest_file_id" gorm:"column:latest_file_id"`
	DateCreated   time.Time `json:"date_created" gorm:"column:date_created"`
	DateModified  time.Time `json:"date_modified" gorm:"column:date_modified"`
	DateReleased  time.Time `json:"date_released" gorm:"column:date_released"`
	UpdatedAt     time.Time `json:"updated_at" gorm:"autoUpdateTime"`
	IndexedAt     time.Time `json:"indexed_at" gorm:"autoCreateTime"`
	// Computed fields for server creation
	MCVersion      string `json:"mc_version" gorm:"column:mc_version"`           // Primary MC version
	JavaVersion    string `json:"java_version" gorm:"column:java_version"`       // Required Java version
	DockerImage    string `json:"docker_image" gorm:"column:docker_image"`       // Recommended Docker image
	RecommendedRAM int    `json:"recommended_ram" gorm:"column:recommended_ram"` // Recommended RAM in MB
}

type IndexedModpackFile struct {
	ID               string          `json:"id" gorm:"primaryKey"`
	ModpackID        string          `json:"modpack_id" gorm:"index;column:modpack_id"`
	DisplayName      string          `json:"display_name" gorm:"column:display_name"`
	FileName         string          `json:"file_name" gorm:"column:file_name"`
	FileDate         time.Time       `json:"file_date" gorm:"column:file_date"`
	FileLength       int64           `json:"file_length" gorm:"column:file_length"`
	ReleaseType      string          `json:"release_type" gorm:"column:release_type"` // Release type is release, beta, or alpha
	DownloadURL      string          `json:"download_url" gorm:"column:download_url"`
	GameVersions     string          `json:"game_versions"` // JSON array stored as string
	ModLoader        string          `json:"mod_loader" gorm:"column:mod_loader"`
	ServerPackFileID *string         `json:"server_pack_file_id" gorm:"column:server_pack_file_id"`
	Modpack          *IndexedModpack `json:"-" gorm:"foreignKey:ModpackID;constraint:OnDelete:CASCADE"`
}

type ModpackFavorite struct {
	ID        string          `json:"id" gorm:"primaryKey"`
	ModpackID string          `json:"modpack_id" gorm:"index;column:modpack_id"`
	CreatedAt time.Time       `json:"created_at" gorm:"autoCreateTime"`
	Modpack   *IndexedModpack `json:"modpack,omitempty" gorm:"foreignKey:ModpackID;constraint:OnDelete:CASCADE"`
}

// Stores the global proxy configuration
type ProxyConfig struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Enabled   bool      `json:"enabled" gorm:"not null;default:false"`
	BaseURL   string    `json:"base_url" gorm:"column:base_url"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// Represents one proxy listening port configuration
type ProxyListener struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	Port        int       `json:"port" gorm:"not null;uniqueIndex"`
	Name        string    `json:"name"` // Example names like Primary or Secondary
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled" gorm:"not null;default:true"`
	IsDefault   bool      `json:"is_default" gorm:"not null;default:false"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// Represents a shareable invite link for registration
type RegistrationInvite struct {
	ID          string     `json:"id" gorm:"primaryKey"`
	Code        string     `json:"code" gorm:"not null;uniqueIndex"`
	Description string     `json:"description"`
	Roles       []string   `json:"roles" gorm:"column:roles;serializer:json"`
	PinHash     string     `json:"-" gorm:"column:pin_hash"`
	MaxUses     int        `json:"max_uses" gorm:"default:0;column:max_uses"`
	UseCount    int        `json:"use_count" gorm:"default:0;column:use_count"`
	ExpiresAt   *time.Time `json:"expires_at" gorm:"column:expires_at"`
	CreatedBy   string     `json:"created_by" gorm:"not null;column:created_by"`
	CreatedAt   time.Time  `json:"created_at" gorm:"autoCreateTime"`
}

// Represents a user account
type User struct {
	ID           string     `json:"id" gorm:"primaryKey"`
	Username     string     `json:"username" gorm:"not null;uniqueIndex:idx_user_provider"`
	Email        *string    `json:"email" gorm:"index"`
	PasswordHash string     `json:"-" gorm:"column:password_hash"`
	AuthProvider string     `json:"auth_provider" gorm:"not null;default:'local';uniqueIndex:idx_user_provider"`
	OIDCSubject  string     `json:"oidc_subject" gorm:"column:oidc_subject;uniqueIndex:idx_oidc_identity,where:oidc_subject != ''"`
	OIDCIssuer   string     `json:"oidc_issuer" gorm:"column:oidc_issuer;uniqueIndex:idx_oidc_identity,where:oidc_subject != ''"`
	IsActive     bool       `json:"is_active" gorm:"not null;default:true"`
	LastLogin    *time.Time `json:"last_login" gorm:"column:last_login"`
	CreatedAt    time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

// Represents a role in the RBAC system
type Role struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"not null;uniqueIndex"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system" gorm:"not null;default:false"`
	IsDefault   bool      `json:"is_default" gorm:"not null;default:false"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// Links users to roles
type UserRole struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	UserID    string    `json:"user_id" gorm:"not null;index;column:user_id"`
	RoleName  string    `json:"role_name" gorm:"not null;index;column:role_name"`
	Source    string    `json:"source" gorm:"not null;default:'local'"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
}

// Stores key-value pairs for internal system configuration
type SystemSetting struct {
	Key   string `gorm:"primaryKey"`
	Value string `gorm:"not null"`
}

// One automation or panel action taken on a server
type ServerAction struct {
	ID        uint              `json:"id" gorm:"primaryKey;autoIncrement"`
	ServerID  string            `json:"server_id" gorm:"column:server_id;not null;index"`
	Timestamp time.Time         `json:"timestamp" gorm:"column:timestamp;not null"`
	Source    string            `json:"source" gorm:"column:source;not null;default:''"` // Username or subsystem
	Name      string            `json:"name" gorm:"column:name"`                         // Dotted action key
	Message   string            `json:"message" gorm:"column:message;not null"`
	Attrs     map[string]string `json:"attrs" gorm:"column:attrs;serializer:json"`
	TraceID   string            `json:"trace_id" gorm:"column:trace_id"` // Shared by one operation's events
}

// Hides one health finding until its content changes
type FindingDismissal struct {
	ServerID    string    `json:"server_id" gorm:"column:server_id;primaryKey"`
	FindingID   string    `json:"finding_id" gorm:"column:finding_id;primaryKey"`
	ContentHash string    `json:"content_hash" gorm:"column:content_hash;not null"`
	DismissedAt time.Time `json:"dismissed_at" gorm:"column:dismissed_at;not null"`
}

// Represents a long-lived API token for programmatic access
type APIToken struct {
	ID            string     `json:"id" gorm:"primaryKey"`
	UserID        string     `json:"user_id" gorm:"not null;index;column:user_id"`
	Name          string     `json:"name" gorm:"not null"`
	TokenHash     string     `json:"-" gorm:"not null;uniqueIndex;column:token_hash"`
	ExpiresAt     *time.Time `json:"expires_at" gorm:"column:expires_at"`
	LastUsedAt    *time.Time `json:"last_used_at" gorm:"column:last_used_at"`
	IsModuleToken bool       `json:"is_module_token" gorm:"default:false;column:is_module_token"`
	CreatedAt     time.Time  `json:"created_at" gorm:"autoCreateTime"`
	User          *User      `json:"-" gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
}

// Represents an active user session
type Session struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	UserID    string    `json:"user_id" gorm:"not null;index;column:user_id"`
	Token     string    `json:"-" gorm:"not null;uniqueIndex"`
	ExpiresAt time.Time `json:"expires_at" gorm:"not null;index"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	User      *User     `json:"user,omitempty" gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE"`
}

// Defines the type of scheduled task
type TaskType string

const (
	TaskTypeCommand TaskType = "command" // Execute an RCON command
	TaskTypeBackup  TaskType = "backup"  // Create a backup
	TaskTypeRestart TaskType = "restart" // Restart the server
	TaskTypeStart   TaskType = "start"   // Start the server
	TaskTypeStop    TaskType = "stop"    // Stop the server
	TaskTypeScript  TaskType = "script"  // Run a custom script
	TaskTypeWebhook TaskType = "webhook" // Send an HTTP webhook
)

// Defines the status of a scheduled task
type TaskStatus string

const (
	TaskStatusEnabled  TaskStatus = "enabled"  // Task is active and will run
	TaskStatusDisabled TaskStatus = "disabled" // Task exists but won't run
	TaskStatusPaused   TaskStatus = "paused"   // Temporarily paused
)

// Defines how the task is scheduled
type ScheduleType string

const (
	ScheduleTypeCron     ScheduleType = "cron"     // Cron expression (e.g., "0 * * * *")
	ScheduleTypeInterval ScheduleType = "interval" // Fixed interval in seconds
	ScheduleTypeOnce     ScheduleType = "once"     // Run once at specific time
	ScheduleTypeEvent    ScheduleType = "event"    // Triggered by a server event
)

// Represents a scheduled task for a server
type ScheduledTask struct {
	ID          string       `json:"id" gorm:"primaryKey"`
	ServerID    string       `json:"server_id" gorm:"not null;index;column:server_id"`
	Name        string       `json:"name" gorm:"not null"`
	Description string       `json:"description"`
	TaskType    TaskType     `json:"task_type" gorm:"not null;column:task_type"`
	Status      TaskStatus   `json:"status" gorm:"not null;default:enabled"`
	Schedule    ScheduleType `json:"schedule" gorm:"not null"`

	// Schedule configuration
	CronExpr      string                  `json:"cron_expr" gorm:"column:cron_expr"`                           // For cron schedule type
	IntervalSecs  int                     `json:"interval_secs" gorm:"column:interval_secs"`                   // For interval schedule type
	RunAt         *time.Time              `json:"run_at" gorm:"column:run_at"`                                 // For once schedule type
	EventTriggers []v1.TriggeredEventType `json:"event_triggers" gorm:"column:event_triggers;serializer:json"` // For event schedule type
	NextRun       *time.Time              `json:"next_run" gorm:"index;column:next_run"`                       // Computed next run time
	LastRun       *time.Time              `json:"last_run" gorm:"column:last_run"`                             // Last execution time
	Timezone      string                  `json:"timezone" gorm:"default:UTC"`                                 // Timezone for schedule

	// Task-specific configuration (JSON)
	Config string `json:"config" gorm:"type:text"` // JSON config based on task type

	// Execution settings
	Timeout       int  `json:"timeout" gorm:"default:300"`          // Timeout in seconds (default 5 min)
	RetryCount    int  `json:"retry_count" gorm:"default:0"`        // Number of retries on failure
	RetryDelay    int  `json:"retry_delay" gorm:"default:60"`       // Delay between retries in seconds
	RequireOnline bool `json:"require_online" gorm:"default:true"`  // Only run if server is online
	FailureNotify bool `json:"failure_notify" gorm:"default:false"` // Notify on failure (future feature)

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`

	Server *Server `json:"-" gorm:"foreignKey:ServerID;constraint:OnDelete:CASCADE"`
}

// Defines the status of a task execution
type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"   // Queued for execution
	ExecutionStatusRunning   ExecutionStatus = "running"   // Currently executing
	ExecutionStatusCompleted ExecutionStatus = "completed" // Finished successfully
	ExecutionStatusFailed    ExecutionStatus = "failed"    // Failed with error
	ExecutionStatusSkipped   ExecutionStatus = "skipped"   // Skipped (e.g., server offline)
	ExecutionStatusCancelled ExecutionStatus = "cancelled" // Cancelled by user
	ExecutionStatusTimeout   ExecutionStatus = "timeout"   // Timed out
)

// Represents a single execution of a scheduled task
type TaskExecution struct {
	ID        string          `json:"id" gorm:"primaryKey"`
	TaskID    string          `json:"task_id" gorm:"not null;index;column:task_id"`
	ServerID  string          `json:"server_id" gorm:"not null;index;column:server_id"`
	Status    ExecutionStatus `json:"status" gorm:"not null"`
	StartedAt time.Time       `json:"started_at" gorm:"not null;column:started_at"`
	EndedAt   *time.Time      `json:"ended_at" gorm:"column:ended_at"`
	Duration  int64           `json:"duration" gorm:"default:0"`                   // Duration in milliseconds
	Output    string          `json:"output" gorm:"type:text"`                     // Output or result
	Error     string          `json:"error" gorm:"type:text"`                      // Error message if failed
	RetryNum  int             `json:"retry_num" gorm:"default:0;column:retry_num"` // Which retry attempt (0 = first try)
	Trigger   string          `json:"trigger" gorm:"default:scheduled"`            // Trigger source is scheduled, manual, or startup

	Task   *ScheduledTask `json:"-" gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE"`
	Server *Server        `json:"-" gorm:"foreignKey:ServerID;constraint:OnDelete:CASCADE"`
}

// Defines whether a module template is built-in or custom
type ModuleTemplateType string

const (
	ModuleTemplateTypeBuiltin ModuleTemplateType = "builtin"
	ModuleTemplateTypeCustom  ModuleTemplateType = "custom"
)

// Defines the runtime state of a module
type ModuleStatus string

const (
	ModuleStatusStopped  ModuleStatus = "stopped"
	ModuleStatusStarting ModuleStatus = "starting"
	ModuleStatusRunning  ModuleStatus = "running"
	ModuleStatusStopping ModuleStatus = "stopping"
	ModuleStatusError    ModuleStatus = "error"
	ModuleStatusCreating ModuleStatus = "creating"
)

// Represents a blueprint for creating modules
type ModuleTemplate struct {
	ID              string             `json:"id" gorm:"primaryKey"`
	Name            string             `json:"name" gorm:"not null;uniqueIndex"`
	Description     string             `json:"description"`
	Type            ModuleTemplateType `json:"type" gorm:"not null;default:custom"`
	DockerImage     string             `json:"docker_image" gorm:"not null;column:docker_image"`
	DefaultEnv      string             `json:"default_env" gorm:"type:text;column:default_env"` // JSON map of default env vars
	DefaultVolumes  string             `json:"default_volumes" gorm:"type:text;column:default_volumes"`
	HealthCheckPath string             `json:"health_check_path" gorm:"column:health_check_path"`
	HealthCheckPort int                `json:"health_check_port" gorm:"column:health_check_port"`
	RequiresServer  bool               `json:"requires_server" gorm:"default:true;column:requires_server"`
	SupportsProxy   bool               `json:"supports_proxy" gorm:"default:true;column:supports_proxy"`
	Icon            string             `json:"icon"`
	Category        string             `json:"category"`
	Documentation   string             `json:"documentation"`
	CreatedAt       time.Time          `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time          `json:"updated_at" gorm:"autoUpdateTime"`

	// Host and container ports, protocol, and proxy flag
	Ports []*v1.ModulePort `json:"ports" gorm:"column:ports;serializer:json"`

	// Suggested module dependencies
	SuggestedDependencies []string `json:"suggested_dependencies" gorm:"column:suggested_dependencies;serializer:json"`

	// Default event hooks for server lifecycle integration
	DefaultHooks []*v1.ModuleEventHook `json:"default_hooks" gorm:"column:default_hooks;serializer:json"`

	// Display metadata as key value pairs for notes and links
	Metadata map[string]string `json:"metadata" gorm:"column:metadata;serializer:json"`

	// Overrides image CMD but not ENTRYPOINT
	DefaultCmd string `json:"default_cmd" gorm:"column:default_cmd"`

	// Default access URL templates
	DefaultAccessUrls []string `json:"default_access_urls" gorm:"column:default_access_urls;serializer:json"`

	// Default resource limits
	DefaultMemory int `json:"default_memory" gorm:"column:default_memory;default:512"` // Default memory in MB

	// Default UID/GID for container user
	DefaultUID string `json:"default_uid" gorm:"column:default_uid;default:''"`
	DefaultGID string `json:"default_gid" gorm:"column:default_gid;default:''"`

	// Init command to exec inside the container after start
	DefaultInitCommand      string `json:"default_init_command" gorm:"column:default_init_command;default:''"`
	DefaultInitCommandDelay int    `json:"default_init_command_delay" gorm:"column:default_init_command_delay;default:0"`
	DefaultRestartAfterInit bool   `json:"default_restart_after_init" gorm:"column:default_restart_after_init;default:false"`
}

// Represents a running module instance attached to a server
type Module struct {
	ID          string       `json:"id" gorm:"primaryKey"`
	Name        string       `json:"name" gorm:"not null"`
	ServerID    string       `json:"server_id" gorm:"not null;index;column:server_id"`
	TemplateID  string       `json:"template_id" gorm:"not null;index;column:template_id"`
	ContainerID string       `json:"container_id" gorm:"column:container_id"`
	Status      ModuleStatus `json:"status" gorm:"not null;default:stopped"`

	// Instance configuration (JSON - merged with template defaults)
	Config          string `json:"config" gorm:"type:text"`
	EnvOverrides    string `json:"env_overrides" gorm:"type:text;column:env_overrides"`
	VolumeOverrides string `json:"volume_overrides" gorm:"type:text;column:volume_overrides"`

	// Resource limits
	Memory   int     `json:"memory" gorm:"default:512"`
	CPULimit float64 `json:"cpu_limit" gorm:"column:cpu_limit"`

	// Container user (supports alias substitution, e.g. "{{host.uid}}")
	UID string `json:"uid" gorm:"column:uid;default:''"`
	GID string `json:"gid" gorm:"column:gid;default:''"`

	// Lifecycle
	AutoStart             bool   `json:"auto_start" gorm:"default:false;column:auto_start"`
	FollowServerLifecycle bool   `json:"follow_server_lifecycle" gorm:"default:true;column:follow_server_lifecycle"`
	Detached              bool   `json:"detached" gorm:"default:false"`
	DataPath              string `json:"data_path" gorm:"column:data_path"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
	LastStarted *time.Time `json:"last_started" gorm:"column:last_started"`

	// Host and container ports, protocol, and proxy flag
	Ports []*v1.ModulePort `json:"ports" gorm:"column:ports;serializer:json"`

	// Module dependencies (started before this module)
	Dependencies []*v1.ModuleDependency `json:"dependencies" gorm:"column:dependencies;serializer:json"`

	// Health check configuration for dependency waiting
	HealthCheckInterval int `json:"health_check_interval" gorm:"column:health_check_interval;default:30"`
	HealthCheckTimeout  int `json:"health_check_timeout" gorm:"column:health_check_timeout;default:5"`
	HealthCheckRetries  int `json:"health_check_retries" gorm:"column:health_check_retries;default:3"`

	// Event hooks for server lifecycle integration
	EventHooks []*v1.ModuleEventHook `json:"event_hooks" gorm:"column:event_hooks;serializer:json"`

	// Instance metadata (merged with/overrides template metadata)
	Metadata map[string]string `json:"metadata" gorm:"column:metadata;serializer:json"`

	// Optional command override (overrides template's default_cmd)
	CmdOverride string `json:"cmd_override" gorm:"column:cmd_override"`

	// Init command to exec inside the container after start
	InitCommand      string `json:"init_command" gorm:"column:init_command;default:''"`
	InitCommandDelay int    `json:"init_command_delay" gorm:"column:init_command_delay;default:0"`
	RestartAfterInit bool   `json:"restart_after_init" gorm:"column:restart_after_init;default:false"`

	// Access URL templates
	AccessUrls []string `json:"access_urls" gorm:"column:access_urls;serializer:json"`

	// Creator tracking and module API token
	CreatedBy      string `json:"created_by" gorm:"column:created_by"`
	TokenID        string `json:"token_id" gorm:"column:token_id"`
	TokenPlaintext string `json:"-" gorm:"-"`

	// Relationships
	Server   *Server         `json:"-" gorm:"foreignKey:ServerID;constraint:OnDelete:CASCADE"`
	Template *ModuleTemplate `json:"-" gorm:"foreignKey:TemplateID;constraint:OnDelete:RESTRICT"`

	// Runtime stats (not persisted)
	MemoryUsage float64 `json:"memory_usage" gorm:"-"`
	CPUPercent  float64 `json:"cpu_percent" gorm:"-"`
}
