package db

import (
	"time"
)

type ServerStatus string

const (
	StatusStopped  ServerStatus = "stopped"
	StatusStarting ServerStatus = "starting"
	StatusRunning  ServerStatus = "running"
	StatusStopping ServerStatus = "stopping"
	StatusError    ServerStatus = "error"
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
	ModLoaderBukkit     ModLoader = "bukkit"
	ModLoaderSpigot     ModLoader = "spigot"
	ModLoaderPaper      ModLoader = "paper"
	ModLoaderPufferfish ModLoader = "pufferfish"
	
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
	ID          string       `json:"id" gorm:"primaryKey"`
	Name        string       `json:"name" gorm:"not null"`
	Description string       `json:"description"`
	ModLoader   ModLoader    `json:"mod_loader" gorm:"not null"`
	MCVersion   string       `json:"mc_version" gorm:"not null;column:mc_version"`
	ContainerID string       `json:"container_id" gorm:"column:container_id"`
	Status      ServerStatus `json:"status" gorm:"not null"`
	Port        int          `json:"port"`
	ProxyPort   int          `json:"proxy_port" gorm:"column:proxy_port"`
	ProxyHostname string     `json:"proxy_hostname" gorm:"column:proxy_hostname;uniqueIndex"`
	MaxPlayers  int          `json:"max_players" gorm:"default:20;column:max_players"`
	Memory      int          `json:"memory" gorm:"default:2048"` // in MB
	CreatedAt   time.Time    `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time    `json:"updated_at" gorm:"autoUpdateTime"`
	LastStarted *time.Time   `json:"last_started" gorm:"column:last_started"`
	JavaVersion string       `json:"java_version" gorm:"column:java_version"`
	DockerImage string       `json:"docker_image" gorm:"column:docker_image"`
	DataPath    string       `json:"data_path" gorm:"not null;column:data_path"`
}

type ServerConfig struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	ServerID  string    `json:"server_id" gorm:"not null;index;column:server_id"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
	Server    *Server   `json:"-" gorm:"foreignKey:ServerID;constraint:OnDelete:CASCADE"`

	// JVM Configuration
	UID                    *int    `json:"uid" env:"UID" default:"1000" desc:"The linux user id to run as" input:"number" label:"User ID"`
	GID                    *int    `json:"gid" env:"GID" default:"1000" desc:"The linux group id to run as" input:"number" label:"Group ID"`
	Memory                 *string `json:"memory" env:"MEMORY" default:"1G" desc:"Initial and maximum Java memory-heap limit" input:"text" label:"Memory" system:"true"`
	InitMemory             *string `json:"initMemory" env:"INIT_MEMORY" default:"1G" desc:"Independently sets the initial heap size" input:"text" label:"Initial Memory" system:"true"`
	MaxMemory              *string `json:"maxMemory" env:"MAX_MEMORY" default:"1G" desc:"Independently sets the max heap size" input:"text" label:"Maximum Memory" system:"true"`
	TZ                     *string `json:"tz" env:"TZ" default:"UTC" desc:"Timezone configuration" input:"text" label:"Timezone"`
	EnableRollingLogs      *bool   `json:"enableRollingLogs" env:"ENABLE_ROLLING_LOGS" default:"false" desc:"Enable rolling log files strategy" input:"checkbox" label:"Enable Rolling Logs"`
	EnableJMX              *bool   `json:"enableJmx" env:"ENABLE_JMX" default:"false" desc:"Enable remote JMX for profiling" input:"checkbox" label:"Enable JMX"`
	JMXHost                *string `json:"jmxHost" env:"JMX_HOST" default:"" desc:"IP/host running the Docker container for JMX" input:"text" label:"JMX Host"`
	UseAikarFlags          *bool   `json:"useAikarFlags" env:"USE_AIKAR_FLAGS" default:"false" desc:"Use Aikar's optimized JVM flags for GC tuning" input:"checkbox" label:"Use Aikar Flags"`
	UseMeowiceFlags        *bool   `json:"useMeowiceFlags" env:"USE_MEOWICE_FLAGS" default:"false" desc:"Use MeowIce's JVM flags optimized for Java 17+" input:"checkbox" label:"Use MeowIce Flags"`
	UseMeowiceGraalVMFlags *bool   `json:"useMeowiceGraalvmFlags" env:"USE_MEOWICE_GRAALVM_FLAGS" default:"true" desc:"Enable MeowIce's flags for GraalVM" input:"checkbox" label:"Use MeowIce GraalVM Flags"`
	JVMOpts                *string `json:"jvmOpts" env:"JVM_OPTS" default:"" desc:"General JVM options" input:"text" label:"JVM Options"`
	JVMXXOpts              *string `json:"jvmXxOpts" env:"JVM_XX_OPTS" default:"" desc:"JVM -XX options" input:"text" label:"JVM XX Options"`
	JVMDDOpts              *string `json:"jvmDdOpts" env:"JVM_DD_OPTS" default:"" desc:"Comma separated list of system properties as name=value pairs" input:"text" label:"JVM DD Options"`
	ExtraArgs              *string `json:"extraArgs" env:"EXTRA_ARGS" default:"" desc:"Arguments passed to the jar file" input:"text" label:"Extra Arguments"`
	LogTimestamp           *bool   `json:"logTimestamp" env:"LOG_TIMESTAMP" default:"false" desc:"Include timestamp with each log" input:"checkbox" label:"Log Timestamp"`

	// Server Configuration
	Type                           *string `json:"type" env:"TYPE" default:"VANILLA" desc:"The server type" input:"text" label:"Server Type" system:"true"`
	EULA                           *string `json:"eula" env:"EULA" default:"TRUE" desc:"This MUST be set to TRUE" input:"checkbox" required:"true" label:"Accept EULA" system:"true"`
	Version                        *string `json:"version" env:"VERSION" default:"LATEST" desc:"The minecraft version" input:"text" label:"Minecraft Version" system:"true"`
	MOTD                           *string `json:"motd" env:"MOTD" default:"" desc:"Server log in message" input:"text" label:"Message of the Day"`
	Difficulty                     *string `json:"difficulty" env:"DIFFICULTY" default:"easy" desc:"Difficulty level (peaceful,easy,normal,hard)" input:"select" label:"Difficulty"`
	Icon                           *string `json:"icon" env:"ICON" default:"" desc:"URL or file path for server icon" input:"text" label:"Server Icon"`
	OverrideIcon                   *bool   `json:"overrideIcon" env:"OVERRIDE_ICON" default:"false" desc:"Override existing server icon" input:"checkbox" label:"Override Icon"`
	MaxPlayers                     *int    `json:"maxPlayers" env:"MAX_PLAYERS" default:"20" desc:"Maximum number of players" input:"number" label:"Max Players"`
	MaxWorldSize                   *int    `json:"maxWorldSize" env:"MAX_WORLD_SIZE" default:"0" desc:"Maximum world size in blocks (radius)" input:"number" label:"Max World Size"`
	AllowNether                    *bool   `json:"allowNether" env:"ALLOW_NETHER" default:"true" desc:"Allow players to travel to the Nether" input:"checkbox" label:"Allow Nether"`
	AnnouncePlayerAchievements     *bool   `json:"announcePlayerAchievements" env:"ANNOUNCE_PLAYER_ACHIEVEMENTS" default:"true" desc:"Announce player achievements" input:"checkbox" label:"Announce Player Achievements"`
	EnableCommandBlock             *bool   `json:"enableCommandBlock" env:"ENABLE_COMMAND_BLOCK" default:"false" desc:"Enable command blocks" input:"checkbox" label:"Enable Command Blocks"`
	ForceGamemode                  *bool   `json:"forceGamemode" env:"FORCE_GAMEMODE" default:"false" desc:"Force players to join in default game mode" input:"checkbox" label:"Force Gamemode"`
	GenerateStructures             *bool   `json:"generateStructures" env:"GENERATE_STRUCTURES" default:"true" desc:"Generate structures like villages" input:"checkbox" label:"Generate Structures"`
	Hardcore                       *bool   `json:"hardcore" env:"HARDCORE" default:"false" desc:"Players set to spectator mode on death" input:"checkbox" label:"Hardcore Mode"`
	SnooperEnabled                 *bool   `json:"snooperEnabled" env:"SNOOPER_ENABLED" default:"true" desc:"Send data to snoop.minecraft.net" input:"checkbox" label:"Enable Snooper"`
	MaxBuildHeight                 *int    `json:"maxBuildHeight" env:"MAX_BUILD_HEIGHT" default:"256" desc:"Maximum building height" input:"number" label:"Max Build Height"`
	SpawnAnimals                   *bool   `json:"spawnAnimals" env:"SPAWN_ANIMALS" default:"true" desc:"Allow animals to spawn" input:"checkbox" label:"Spawn Animals"`
	SpawnMonsters                  *bool   `json:"spawnMonsters" env:"SPAWN_MONSTERS" default:"true" desc:"Allow monsters to spawn" input:"checkbox" label:"Spawn Monsters"`
	SpawnNPCs                      *bool   `json:"spawnNpcs" env:"SPAWN_NPCS" default:"true" desc:"Allow villagers to spawn" input:"checkbox" label:"Spawn NPCs"`
	SpawnProtection                *int    `json:"spawnProtection" env:"SPAWN_PROTECTION" default:"0" desc:"Area that non-ops cannot edit (0 to disable)" input:"number" label:"Spawn Protection"`
	ViewDistance                   *int    `json:"viewDistance" env:"VIEW_DISTANCE" default:"0" desc:"Server-side viewing distance in chunks" input:"number" label:"View Distance"`
	Seed                           *string `json:"seed" env:"SEED" default:"" desc:"World generation seed" input:"text" label:"World Seed"`
	Mode                           *string `json:"mode" env:"MODE" default:"survival" desc:"Game mode (creative,survival,adventure,spectator)" input:"select" label:"Game Mode"`
	PVP                            *bool   `json:"pvp" env:"PVP" default:"true" desc:"Enable player-vs-player mode" input:"checkbox" label:"Enable PVP"`
	LevelType                      *string `json:"levelType" env:"LEVEL_TYPE" default:"minecraft:default" desc:"World generation type" input:"text" label:"Level Type"`
	GeneratorSettings              *string `json:"generatorSettings" env:"GENERATOR_SETTINGS" default:"" desc:"Additional world generation settings" input:"text" label:"Generator Settings"`
	Level                          *string `json:"level" env:"LEVEL" default:"world" desc:"World save name" input:"text" label:"World Name"`
	OnlineMode                     *bool   `json:"onlineMode" env:"ONLINE_MODE" default:"true" desc:"Authenticate players against Minecraft account database" input:"checkbox" label:"Online Mode"`
	AllowFlight                    *bool   `json:"allowFlight" env:"ALLOW_FLIGHT" default:"false" desc:"Allow flight in survival mode with mods" input:"checkbox" label:"Allow Flight"`
	ServerName                     *string `json:"serverName" env:"SERVER_NAME" default:"" desc:"The server name" input:"text" label:"Server Name"`
	ServerPort                     *int    `json:"serverPort" env:"SERVER_PORT" default:"0" desc:"Server port (only change if using host networking)" input:"number" label:"Server Port" system:"true"`
	PlayerIdleTimeout              *int    `json:"playerIdleTimeout" env:"PLAYER_IDLE_TIMEOUT" default:"0" desc:"Player idle timeout" input:"number" label:"Player Idle Timeout"`
	SyncChunkWrites                *bool   `json:"syncChunkWrites" env:"SYNC_CHUNK_WRITES" default:"false" desc:"Sync chunk writes" input:"checkbox" label:"Sync Chunk Writes"`
	EnableStatus                   *bool   `json:"enableStatus" env:"ENABLE_STATUS" default:"false" desc:"Enable server status" input:"checkbox" label:"Enable Status"`
	EntityBroadcastRangePercentage *int    `json:"entityBroadcastRangePercentage" env:"ENTITY_BROADCAST_RANGE_PERCENTAGE" default:"0" desc:"Entity broadcast range percentage" input:"number" label:"Entity Broadcast Range Percentage"`
	FunctionPermissionLevel        *int    `json:"functionPermissionLevel" env:"FUNCTION_PERMISSION_LEVEL" default:"0" desc:"Function permission level" input:"number" label:"Function Permission Level"`
	NetworkCompressionThreshold    *int    `json:"networkCompressionThreshold" env:"NETWORK_COMPRESSION_THRESHOLD" default:"0" desc:"Network compression threshold" input:"number" label:"Network Compression Threshold"`
	OpPermissionLevel              *int    `json:"opPermissionLevel" env:"OP_PERMISSION_LEVEL" default:"0" desc:"OP permission level" input:"number" label:"OP Permission Level"`
	PreventProxyConnections        *bool   `json:"preventProxyConnections" env:"PREVENT_PROXY_CONNECTIONS" default:"false" desc:"Prevent proxy connections" input:"checkbox" label:"Prevent Proxy Connections"`
	UseNativeTransport             *bool   `json:"useNativeTransport" env:"USE_NATIVE_TRANSPORT" default:"false" desc:"Use native transport" input:"checkbox" label:"Use Native Transport"`
	SimulationDistance             *int    `json:"simulationDistance" env:"SIMULATION_DISTANCE" default:"0" desc:"Simulation distance" input:"number" label:"Simulation Distance"`
	ExecDirectly                   *bool   `json:"execDirectly" env:"EXEC_DIRECTLY" default:"false" desc:"Enable docker attach with color and interactive capabilities" input:"checkbox" label:"Execute Directly"`
	StopServerAnnounceDelay        *int    `json:"stopServerAnnounceDelay" env:"STOP_SERVER_ANNOUNCE_DELAY" default:"0" desc:"Delay in seconds after shutdown announcement" input:"number" label:"Stop Server Announce Delay"`
	Proxy                          *string `json:"proxy" env:"PROXY" default:"" desc:"HTTP/HTTPS proxy URL" input:"text" label:"Proxy URL"`
	Console                        *bool   `json:"console" env:"CONSOLE" default:"true" desc:"Console setting for older Spigot versions" input:"checkbox" label:"Enable Console"`
	GUI                            *bool   `json:"gui" env:"GUI" default:"true" desc:"GUI interface setting for older servers" input:"checkbox" label:"Enable GUI"`
	StopDuration                   *int    `json:"stopDuration" env:"STOP_DURATION" default:"60" desc:"Seconds to wait for graceful shutdown" input:"number" label:"Stop Duration"`
	SetupOnly                      *bool   `json:"setupOnly" env:"SETUP_ONLY" default:"false" desc:"Setup server files without launching" input:"checkbox" label:"Setup Only"`
	UseFlareFlags                  *bool   `json:"useFlareFlags" env:"USE_FLARE_FLAGS" default:"false" desc:"Enable JVM flags for Flare profiling suite" input:"checkbox" label:"Use Flare Flags"`
	UseSimdFlags                   *bool   `json:"useSimdFlags" env:"USE_SIMD_FLAGS" default:"false" desc:"Support for optimized SIMD operations" input:"checkbox" label:"Use SIMD Flags"`

	// Custom Resource Pack
	ResourcePack        *string `json:"resourcePack" env:"RESOURCE_PACK" default:"" desc:"Link to custom resource pack" input:"text" label:"Resource Pack URL"`
	ResourcePackSHA1    *string `json:"resourcePackSha1" env:"RESOURCE_PACK_SHA1" default:"" desc:"Checksum for custom resource pack" input:"text" label:"Resource Pack SHA1"`
	ResourcePackEnforce *bool   `json:"resourcePackEnforce" env:"RESOURCE_PACK_ENFORCE" default:"false" desc:"Enforce resource pack on clients" input:"checkbox" label:"Enforce Resource Pack"`

	// Whitelist
	EnableWhitelist   *bool   `json:"enableWhitelist" env:"ENABLE_WHITELIST" default:"false" desc:"Enable server whitelist" input:"checkbox" label:"Enable Whitelist"`
	Whitelist         *string `json:"whitelist" env:"WHITELIST" default:"" desc:"Comma-separated list of usernames/UUIDs" input:"text" label:"Whitelist Players"`
	WhitelistFile     *string `json:"whitelistFile" env:"WHITELIST_FILE" default:"" desc:"URL or path to whitelist JSON file" input:"text" label:"Whitelist File"`
	OverrideWhitelist *bool   `json:"overrideWhitelist" env:"OVERRIDE_WHITELIST" default:"false" desc:"Regenerate whitelist on each startup" input:"checkbox" label:"Override Whitelist"`

	// RCON
	EnableRCON             *bool   `json:"enableRcon" env:"ENABLE_RCON" default:"true" desc:"Enable RCON support" input:"checkbox" label:"Enable RCON"`
	RCONPassword           *string `json:"rconPassword" env:"RCON_PASSWORD" default:"" desc:"RCON password (MUST be changed)" input:"password" required:"true" label:"RCON Password"`
	RCONPort               *int    `json:"rconPort" env:"RCON_PORT" default:"25575" desc:"RCON port" input:"number" label:"RCON Port"`
	BroadcastRCONToOps     *bool   `json:"broadcastRconToOps" env:"BROADCAST_RCON_TO_OPS" default:"false" desc:"Broadcast RCON to ops" input:"checkbox" label:"Broadcast RCON to OPs"`
	RCONCmdsStartup        *string `json:"rconCmdsStartup" env:"RCON_CMDS_STARTUP" default:"" desc:"RCON commands to execute on server start" input:"text" label:"RCON Commands on Startup"`
	RCONCmdsOnConnect      *string `json:"rconCmdsOnConnect" env:"RCON_CMDS_ON_CONNECT" default:"" desc:"RCON commands to execute on client connect" input:"text" label:"RCON Commands on Connect"`
	RCONCmdsFirstConnect   *string `json:"rconCmdsFirstConnect" env:"RCON_CMDS_FIRST_CONNECT" default:"" desc:"RCON commands to execute on first client connect" input:"text" label:"RCON Commands on First Connect"`
	RCONCmdsOnDisconnect   *string `json:"rconCmdsOnDisconnect" env:"RCON_CMDS_ON_DISCONNECT" default:"" desc:"RCON commands to execute on client disconnect" input:"text" label:"RCON Commands on Disconnect"`
	RCONCmdsLastDisconnect *string `json:"rconCmdsLastDisconnect" env:"RCON_CMDS_LAST_DISCONNECT" default:"" desc:"RCON commands to execute on last client disconnect" input:"text" label:"RCON Commands on Last Disconnect"`

	// Auto-Pause
	EnableAutopause         *bool   `json:"enableAutopause" env:"ENABLE_AUTOPAUSE" default:"false" desc:"Enable autopause functionality" input:"checkbox" label:"Enable Auto-Pause"`
	AutopauseTimeoutEst     *int    `json:"autopauseTimeoutEst" env:"AUTOPAUSE_TIMEOUT_EST" default:"3600" desc:"Time between last disconnect and pausing (seconds)" input:"number" label:"Auto-Pause Timeout (Established)"`
	AutopauseTimeoutInit    *int    `json:"autopauseTimeoutInit" env:"AUTOPAUSE_TIMEOUT_INIT" default:"600" desc:"Time between server start and pausing if no client connects (seconds)" input:"number" label:"Auto-Pause Timeout (Initial)"`
	AutopauseTimeoutKn      *int    `json:"autopauseTimeoutKn" env:"AUTOPAUSE_TIMEOUT_KN" default:"120" desc:"Time between port knock and pausing if no client connects (seconds)" input:"number" label:"Auto-Pause Timeout (Knock)"`
	AutopausePeriod         *int    `json:"autopausePeriod" env:"AUTOPAUSE_PERIOD" default:"10" desc:"Period of the autopause state machine (seconds)" input:"number" label:"Auto-Pause Period"`
	AutopauseKnockInterface *string `json:"autopauseKnockInterface" env:"AUTOPAUSE_KNOCK_INTERFACE" default:"eth0" desc:"Network interface for knockd daemon" input:"text" label:"Auto-Pause Knock Interface"`
	DebugAutopause          *bool   `json:"debugAutopause" env:"DEBUG_AUTOPAUSE" default:"false" desc:"Enable autopause debugging output" input:"checkbox" label:"Debug Auto-Pause"`

	// Auto-Stop
	EnableAutostop      *bool `json:"enableAutostop" env:"ENABLE_AUTOSTOP" default:"false" desc:"Enable autostop functionality" input:"checkbox" label:"Enable Auto-Stop"`
	AutostopTimeoutEst  *int  `json:"autostopTimeoutEst" env:"AUTOSTOP_TIMEOUT_EST" default:"3600" desc:"Time between last disconnect and stopping (seconds)" input:"number" label:"Auto-Stop Timeout (Established)"`
	AutostopTimeoutInit *int  `json:"autostopTimeoutInit" env:"AUTOSTOP_TIMEOUT_INIT" default:"1800" desc:"Time between server start and stopping if no client connects (seconds)" input:"number" label:"Auto-Stop Timeout (Initial)"`
	AutostopPeriod      *int  `json:"autostopPeriod" env:"AUTOSTOP_PERIOD" default:"10" desc:"Period of the autostop state machine (seconds)" input:"number" label:"Auto-Stop Period"`
	DebugAutostop       *bool `json:"debugAutostop" env:"DEBUG_AUTOSTOP" default:"false" desc:"Enable autostop debugging output" input:"checkbox" label:"Debug Auto-Stop"`

	// Forge Configuration
	ForgeVersion            *string `json:"forgeVersion" env:"FORGE_VERSION" default:"" desc:"Specific Forge version to install" input:"text" label:"Forge Version"`
	ForgeInstaller          *string `json:"forgeInstaller" env:"FORGE_INSTALLER" default:"" desc:"Path to pre-downloaded Forge installer" input:"text" label:"Forge Installer"`
	ForgeInstallerURL       *string `json:"forgeInstallerUrl" env:"FORGE_INSTALLER_URL" default:"" desc:"URL to download Forge installer" input:"text" label:"Forge Installer URL"`
	
	// CurseForge
	CFAPIKey                *string `json:"cfApiKey" env:"CF_API_KEY" default:"" desc:"CurseForge (Eternal) API Key" input:"password" label:"CurseForge API Key"`
	CFAPIKeyFile            *string `json:"cfApiKeyFile" env:"CF_API_KEY_FILE" default:"" desc:"Path to file containing CurseForge API Key" input:"text" label:"CurseForge API Key File"`
	CFPageURL               *string `json:"cfPageUrl" env:"CF_PAGE_URL" default:"" desc:"URL to modpack or specific file" input:"text" label:"CurseForge Page URL"`
	CFSlug                  *string `json:"cfSlug" env:"CF_SLUG" default:"" desc:"Modpack slug identifier" input:"text" label:"CurseForge Slug"`
	CFFileID                *string `json:"cfFileId" env:"CF_FILE_ID" default:"" desc:"Mod CurseForge numerical ID" input:"text" label:"CurseForge File ID"`
	CFModpackZip            *string `json:"cfModpackZip" env:"CF_MODPACK_ZIP" default:"" desc:"Container path to unpublished modpack zip" input:"text" label:"CurseForge Modpack Zip"`
	CFFilenameMatcher       *string `json:"cfFilenameMatcher" env:"CF_FILENAME_MATCHER" default:"" desc:"Substring to match desired filename" input:"text" label:"CurseForge Filename Matcher"`
	CFExcludeIncludeFile    *string `json:"cfExcludeIncludeFile" env:"CF_EXCLUDE_INCLUDE_FILE" default:"" desc:"JSON file for global/modpack exclusions" input:"text" label:"CurseForge Exclude/Include File"`
	CFExcludeMods           *string `json:"cfExcludeMods" env:"CF_EXCLUDE_MODS" default:"" desc:"Comma/space delimited list of mod slugs/IDs to exclude" input:"text" label:"CurseForge Exclude Mods"`
	CFForceIncludeMods      *string `json:"cfForceIncludeMods" env:"CF_FORCE_INCLUDE_MODS" default:"" desc:"Comma/space delimited list of mod slugs/IDs to include" input:"text" label:"CurseForge Force Include Mods"`
	CFForceSynchronize      *bool   `json:"cfForceSynchronize" env:"CF_FORCE_SYNCHRONIZE" default:"false" desc:"Force re-evaluation of excludes/includes" input:"checkbox" label:"CurseForge Force Synchronize"`
	CFSetLevelFrom          *string `json:"cfSetLevelFrom" env:"CF_SET_LEVEL_FROM" default:"" desc:"Set LEVEL from WORLD_FILE or OVERRIDES" input:"select" label:"CurseForge Set Level From"`
	CFParallelDownloads     *int    `json:"cfParallelDownloads" env:"CF_PARALLEL_DOWNLOADS" default:"4" desc:"Number of parallel mod downloads" input:"number" label:"CurseForge Parallel Downloads"`
	CFOverridesSkipExisting *bool   `json:"cfOverridesSkipExisting" env:"CF_OVERRIDES_SKIP_EXISTING" default:"false" desc:"Skip existing files in overrides" input:"checkbox" label:"CurseForge Skip Existing Overrides"`
	CFForceReinstallModloader *bool   `json:"cfForceReinstallModloader" env:"CF_FORCE_REINSTALL_MODLOADER" default:"false" desc:"Force reinstall modloader (cleared after start)" input:"checkbox" label:"Force Reinstall Modloader" ephemeral:"true"`
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
	ID              string    `json:"id" gorm:"primaryKey"` // Format: "indexer-originalId"
	IndexerID       string    `json:"indexer_id" gorm:"index;column:indexer_id"` // Original ID from indexer
	Indexer         string    `json:"indexer" gorm:"index"` // "fuego", "modrinth", etc.
	Name            string    `json:"name" gorm:"not null;index"`
	Slug            string    `json:"slug" gorm:"index"`
	Summary         string    `json:"summary"`
	Description     string    `json:"description" gorm:"type:text"`
	LogoURL         string    `json:"logo_url" gorm:"column:logo_url"`
	WebsiteURL      string    `json:"website_url" gorm:"column:website_url"`
	DownloadCount   int64     `json:"download_count" gorm:"column:download_count"`
	Categories      string    `json:"categories"` // JSON array stored as string
	GameVersions    string    `json:"game_versions"` // JSON array stored as string
	ModLoaders      string    `json:"mod_loaders"` // JSON array stored as string
	LatestFileID    string    `json:"latest_file_id" gorm:"column:latest_file_id"`
	DateCreated     time.Time `json:"date_created" gorm:"column:date_created"`
	DateModified    time.Time `json:"date_modified" gorm:"column:date_modified"`
	DateReleased    time.Time `json:"date_released" gorm:"column:date_released"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"autoUpdateTime"`
	IndexedAt       time.Time `json:"indexed_at" gorm:"autoCreateTime"`
	// Computed fields for server creation
	MCVersion       string    `json:"mc_version" gorm:"column:mc_version"` // Primary MC version
	JavaVersion     string    `json:"java_version" gorm:"column:java_version"` // Required Java version
	DockerImage     string    `json:"docker_image" gorm:"column:docker_image"` // Recommended Docker image
	RecommendedRAM  int       `json:"recommended_ram" gorm:"column:recommended_ram"` // Recommended RAM in MB
}

type IndexedModpackFile struct {
	ID              string    `json:"id" gorm:"primaryKey"`
	ModpackID       string    `json:"modpack_id" gorm:"index;column:modpack_id"`
	DisplayName     string    `json:"display_name" gorm:"column:display_name"`
	FileName        string    `json:"file_name" gorm:"column:file_name"`
	FileDate        time.Time `json:"file_date" gorm:"column:file_date"`
	FileLength      int64     `json:"file_length" gorm:"column:file_length"`
	ReleaseType     string    `json:"release_type" gorm:"column:release_type"` // "release", "beta", "alpha"
	DownloadURL     string    `json:"download_url" gorm:"column:download_url"`
	GameVersions    string    `json:"game_versions"` // JSON array stored as string
	ModLoader       string    `json:"mod_loader" gorm:"column:mod_loader"`
	ServerPackFileID *string  `json:"server_pack_file_id" gorm:"column:server_pack_file_id"`
	Modpack         *IndexedModpack `json:"-" gorm:"foreignKey:ModpackID;constraint:OnDelete:CASCADE"`
}

type ModpackFavorite struct {
	ID        string          `json:"id" gorm:"primaryKey"`
	ModpackID string          `json:"modpack_id" gorm:"index;column:modpack_id"`
	CreatedAt time.Time       `json:"created_at" gorm:"autoCreateTime"`
	Modpack   *IndexedModpack `json:"modpack,omitempty" gorm:"foreignKey:ModpackID;constraint:OnDelete:CASCADE"`
}
