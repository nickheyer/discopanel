package services

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/auth"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"gorm.io/gorm"
)

// Compile-time check that ConfigService implements the interface
var _ discopanelv1connect.ConfigServiceHandler = (*ConfigService)(nil)

// ConfigService implements the Config service
type ConfigService struct {
	store  *storage.Store
	docker *docker.Client
	config *config.Config
	log    *logger.Logger
}

// NewConfigService creates a new config service
func NewConfigService(store *storage.Store, docker *docker.Client, cfg *config.Config, log *logger.Logger) *ConfigService {
	return &ConfigService{
		store:  store,
		docker: docker,
		config: cfg,
		log:    log,
	}
}

// GetServerConfig gets server configuration
func (s *ConfigService) GetServerConfig(ctx context.Context, req *connect.Request[v1.GetServerConfigRequest]) (*connect.Response[v1.GetServerConfigResponse], error) {
	serverID := req.Msg.ServerId
	if serverID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("server_id is required"))
	}

	// Get server to ensure it exists and sync config
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Ensure config is synced with server
	if err := s.store.SyncServerConfigWithServer(ctx, server); err != nil {
		s.log.Error("Failed to sync server config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to sync server configuration"))
	}

	// Get the synced config
	serverConfig, err := s.store.GetServerConfig(ctx, serverID)
	if err != nil {
		s.log.Error("Failed to get server config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration"))
	}

	// Convert to categorized format
	categories, err := buildConfigCategories(serverConfig)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build configuration"))
	}

	return connect.NewResponse(&v1.GetServerConfigResponse{
		Categories: categories,
	}), nil
}

// UpdateServerConfig updates server configuration
func (s *ConfigService) UpdateServerConfig(ctx context.Context, req *connect.Request[v1.UpdateServerConfigRequest]) (*connect.Response[v1.UpdateServerConfigResponse], error) {
	serverID := req.Msg.ServerId
	if serverID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("server_id is required"))
	}

	// Get server info
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Get existing config
	serverConfig, err := s.store.GetServerConfig(ctx, serverID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			serverConfig = s.store.CreateDefaultServerConfig(serverID)
		} else {
			s.log.Error("Failed to get server config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration"))
		}
	}

	// Apply updates from protobuf Any map
	if err := applyConfigUpdates(serverConfig, req.Msg.Updates); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("failed to apply updates: %w", err))
	}

	// Save updated config
	if err := s.store.SaveServerConfig(ctx, serverConfig); err != nil {
		s.log.Error("Failed to save server config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save server configuration"))
	}

	// If server has a container, we need to recreate it with the new config
	if server.ContainerID != "" {
		oldContainerID := server.ContainerID

		// Check if server is running
		wasRunning := false
		if server.Status == storage.StatusRunning {
			wasRunning = true
			// Stop the container first
			if err := s.docker.StopContainer(ctx, oldContainerID); err != nil {
				s.log.Error("Failed to stop container for config update: %v", err)
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop server for configuration update"))
			}

			// Wait for clean shutdown
			time.Sleep(2 * time.Second)
		}

		// Remove old container
		if err := s.docker.RemoveContainer(ctx, oldContainerID); err != nil {
			s.log.Error("Failed to remove old container: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to remove old container"))
		}

		// Create new container with updated config
		newContainerID, err := s.docker.CreateContainer(ctx, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to create new container with updated config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create new container with updated configuration"))
		}

		// Update server with new container ID
		server.ContainerID = newContainerID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server with new container ID: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
		}

		// Restart if it was running
		if wasRunning {
			if err := s.docker.StartContainer(ctx, newContainerID); err != nil {
				s.log.Error("Failed to restart container after config update: %v", err)
				// Don't fail the whole operation, config is already saved
			} else {
				server.Status = storage.StatusStarting
				now := time.Now()
				server.LastStarted = &now
				if err := s.store.UpdateServer(ctx, server); err != nil {
					s.log.Error("Failed to update server status: %v", err)
				}
			}
		}

		s.log.Info("Container recreated with updated configuration")
	}

	// Return updated config
	categories, err := buildConfigCategories(serverConfig)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build configuration"))
	}

	return connect.NewResponse(&v1.UpdateServerConfigResponse{
		Categories: categories,
	}), nil
}

// GetGlobalSettings gets the global settings
func (s *ConfigService) GetGlobalSettings(ctx context.Context, req *connect.Request[v1.GetGlobalSettingsRequest]) (*connect.Response[v1.GetGlobalSettingsResponse], error) {
	// Check if auth is enabled and enforce admin role
	authConfig, _, err := s.store.GetAuthConfig(ctx)
	if err != nil {
		s.log.Error("Failed to get auth config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get auth configuration"))
	}

	if authConfig.Enabled {
		// Get user from context (set by auth middleware if token present)
		user := auth.GetUserFromContext(ctx)
		if user == nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
		}
		if !auth.CheckPermission(user, storage.RoleAdmin) {
			return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("admin access required"))
		}
	}

	globalConfig, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get global settings"))
	}

	// Convert to categorized format
	categories, err := buildConfigCategories(globalConfig)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build configuration"))
	}

	return connect.NewResponse(&v1.GetGlobalSettingsResponse{
		Categories: categories,
	}), nil
}

// UpdateGlobalSettings updates the global settings
func (s *ConfigService) UpdateGlobalSettings(ctx context.Context, req *connect.Request[v1.UpdateGlobalSettingsRequest]) (*connect.Response[v1.UpdateGlobalSettingsResponse], error) {
	// Check if auth is enabled and enforce admin role
	authConfig, _, err := s.store.GetAuthConfig(ctx)
	if err != nil {
		s.log.Error("Failed to get auth config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get auth configuration"))
	}

	if authConfig.Enabled {
		// Get user from context (set by auth middleware if token present)
		user := auth.GetUserFromContext(ctx)
		if user == nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
		}
		if !auth.CheckPermission(user, storage.RoleAdmin) {
			return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("admin access required"))
		}
	}

	// Get existing config
	globalConfig, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get global settings"))
	}

	// Apply updates from protobuf Any map
	if err := applyConfigUpdates(globalConfig, req.Msg.Updates); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("failed to apply updates: %w", err))
	}

	// Save updated config
	if err := s.store.UpdateGlobalSettings(ctx, globalConfig); err != nil {
		s.log.Error("Failed to save global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save global settings"))
	}

	// Return updated config
	categories, err := buildConfigCategories(globalConfig)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build configuration"))
	}

	return connect.NewResponse(&v1.UpdateGlobalSettingsResponse{
		Categories: categories,
	}), nil
}

// buildConfigCategories converts ServerConfig struct to categorized format for UI
func buildConfigCategories(configStruct any) ([]*v1.ConfigCategory, error) {
	categories := []*v1.ConfigCategory{
		{Name: "JVM Configuration", Properties: []*v1.ConfigProperty{}},
		{Name: "Server Settings", Properties: []*v1.ConfigProperty{}},
		{Name: "Game Settings", Properties: []*v1.ConfigProperty{}},
		{Name: "World Generation", Properties: []*v1.ConfigProperty{}},
		{Name: "RCON", Properties: []*v1.ConfigProperty{}},
		{Name: "Resource Pack", Properties: []*v1.ConfigProperty{}},
		{Name: "Management Server", Properties: []*v1.ConfigProperty{}},
		{Name: "Ops/Admins", Properties: []*v1.ConfigProperty{}},
		{Name: "Whitelist", Properties: []*v1.ConfigProperty{}},
		{Name: "Auto-Pause", Properties: []*v1.ConfigProperty{}},
		{Name: "Auto-Stop", Properties: []*v1.ConfigProperty{}},
		{Name: "CurseForge", Properties: []*v1.ConfigProperty{}},
		{Name: "Modrinth", Properties: []*v1.ConfigProperty{}},
	}

	configValue := reflect.ValueOf(configStruct).Elem()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" || jsonTag == "id" || jsonTag == "serverId" || jsonTag == "updatedAt" {
			continue
		}

		envTag := field.Tag.Get("env")
		defaultTag := field.Tag.Get("default")
		descTag := field.Tag.Get("desc")
		inputTag := field.Tag.Get("input")
		requiredTag := field.Tag.Get("required")
		labelTag := field.Tag.Get("label")
		systemTag := field.Tag.Get("system")
		ephemeralTag := field.Tag.Get("ephemeral")

		fieldValue := configValue.Field(i)
		value := fieldValue.Interface()

		// Handle pointer types - if nil, leave as nil, otherwise dereference
		var valueAny *anypb.Any
		if fieldValue.Kind() == reflect.Pointer {
			if fieldValue.IsNil() {
				valueAny = nil
			} else {
				derefValue := fieldValue.Elem().Interface()
				var err error
				valueAny, err = toAnyPb(derefValue)
				if err != nil {
					return nil, fmt.Errorf("failed to convert value for field %s: %w", jsonTag, err)
				}
			}
		} else {
			var err error
			valueAny, err = toAnyPb(value)
			if err != nil {
				return nil, fmt.Errorf("failed to convert value for field %s: %w", jsonTag, err)
			}
		}

		// Parse default value based on field type
		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		var defaultValueAny *anypb.Any
		switch fieldType.Kind() {
		case reflect.Bool:
			defaultVal := defaultTag == "true"
			defaultValueAny, _ = toAnyPb(defaultVal)
		case reflect.Int, reflect.Int32, reflect.Int64:
			if defaultTag != "" {
				if intVal, err := strconv.ParseInt(defaultTag, 10, 64); err == nil {
					defaultValueAny, _ = toAnyPb(intVal)
				} else {
					defaultValueAny, _ = toAnyPb(int64(0))
				}
			} else {
				defaultValueAny, _ = toAnyPb(int64(0))
			}
		default:
			if defaultTag != "" {
				defaultValueAny, _ = toAnyPb(defaultTag)
			}
		}

		// Use label if provided, otherwise use the json tag
		label := labelTag
		if label == "" {
			label = jsonTag
		}

		prop := &v1.ConfigProperty{
			Key:          jsonTag,
			Label:        label,
			Value:        valueAny,
			DefaultValue: defaultValueAny,
			Type:         inputTag,
			Description:  descTag,
			Required:     requiredTag == "true",
			System:       systemTag == "true",
			Ephemeral:    ephemeralTag == "true",
			EnvVar:       envTag,
		}

		// Add options for select fields
		if inputTag == "select" {
			switch jsonTag {
			case "difficulty":
				prop.Options = []string{"peaceful", "easy", "normal", "hard"}
			case "mode":
				prop.Options = []string{"creative", "survival", "adventure", "spectator"}
			case "cfSetLevelFrom":
				prop.Options = []string{"", "WORLD_FILE", "OVERRIDES"}
			case "userApiProvider":
				prop.Options = []string{"playerdb", "mojang"}
			case "existingOpsFile":
				prop.Options = []string{"SKIP", "SYNCHRONIZE", "MERGE", "SYNC_FILE_MERGE_LIST"}
			case "existingWhitelistFile":
				prop.Options = []string{"SKIP", "SYNCHRONIZE", "MERGE", "SYNC_FILE_MERGE_LIST"}
			case "modrinthDownloadDependencies":
				prop.Options = []string{"none", "required", "optional"}
			case "modrinthProjectsDefaultVersionType":
				prop.Options = []string{"release", "beta", "alpha"}
			case "modrinthModpackVersionType":
				prop.Options = []string{"release", "beta", "alpha"}
			case "modrinthLoader":
				prop.Options = []string{"forge", "fabric", "quilt"}
			}
		}

		// Categorize the property
		categoryIndex := getCategoryIndex(jsonTag)
		if categoryIndex >= 0 && categoryIndex < len(categories) {
			categories[categoryIndex].Properties = append(categories[categoryIndex].Properties, prop)
		}
	}

	// Remove empty categories
	var nonEmptyCategories []*v1.ConfigCategory
	for _, cat := range categories {
		if len(cat.Properties) > 0 {
			nonEmptyCategories = append(nonEmptyCategories, cat)
		}
	}

	return nonEmptyCategories, nil
}

// getCategoryIndex determines which category a property belongs to
func getCategoryIndex(key string) int {
	switch key {
	// JVM Configuration (0)
	case "uid", "gid", "memory", "initMemory", "maxMemory", "tz", "enableRollingLogs",
		"enableJmx", "jmxHost", "useAikarFlags", "useMeowiceFlags", "useMeowiceGraalvmFlags",
		"jvmOpts", "jvmXxOpts", "jvmDdOpts", "extraArgs", "logTimestamp":
		return 0

	// Server Settings (1)
	case "type", "customServer", "customJarExec", "eula", "version", "motd", "icon", "overrideIcon", "serverName",
		"serverPort", "console", "gui", "stopDuration", "setupOnly", "execDirectly",
		"stopServerAnnounceDelay", "proxy", "useFlareFlags", "useSimdFlags",
		"serverPropertiesEscapeUnicode", "bugReportLink", "customServerProperties":
		return 1

	// Game Settings (2)
	case "difficulty", "maxPlayers", "allowNether", "announcePlayerAchievements",
		"enableCommandBlock", "forceGamemode", "hardcore", "snooperEnabled", "maxBuildHeight",
		"spawnAnimals", "spawnMonsters", "spawnNpcs", "spawnProtection", "viewDistance",
		"mode", "pvp", "onlineMode", "allowFlight", "playerIdleTimeout", "syncChunkWrites",
		"enableStatus", "entityBroadcastRangePercentage", "functionPermissionLevel",
		"networkCompressionThreshold", "opPermissionLevel", "preventProxyConnections",
		"useNativeTransport", "simulationDistance", "enableQuery", "queryPort",
		"acceptsTransfers", "broadcastConsoleToOps", "enforceSecureProfile",
		"hideOnlinePlayers", "logIps", "maxChainedNeighborUpdates", "pauseWhenEmptySeconds",
		"rateLimit", "statusHeartbeatInterval":
		return 2

	// World Generation (3)
	case "generateStructures", "maxWorldSize", "seed", "levelType", "generatorSettings", "level",
		"regionFileCompression":
		return 3

	// RCON (4)
	case "enableRcon", "rconPassword", "rconPort", "rconCmdsStartup",
		"rconCmdsOnConnect", "rconCmdsFirstConnect", "rconCmdsOnDisconnect", "rconCmdsLastDisconnect":
		return 4

	// Resource Pack (5)
	case "resourcePack", "resourcePackSha1", "resourcePackEnforce", "resourcePackId", "resourcePackPrompt":
		return 5

	// Management Server (6)
	case "managementServerAllowedOrigins", "managementServerEnabled", "managementServerHost",
		"managementServerPort", "managementServerSecret", "managementServerTlsEnabled",
		"managementServerTlsKeystore", "managementServerTlsKeystorePassword":
		return 6

	// Ops/Admins (7)
	case "userApiProvider", "ops", "opsFile", "existingOpsFile":
		return 7

	// Whitelist (8)
	case "enableWhitelist", "whitelist", "whitelistFile", "overrideWhitelist",
		"existingWhitelistFile", "enforceWhitelist":
		return 8

	// Auto-Pause (9)
	case "enableAutopause", "autopauseTimeoutEst", "autopauseTimeoutInit", "autopauseTimeoutKn",
		"autopausePeriod", "autopauseKnockInterface", "debugAutopause":
		return 9

	// Auto-Stop (10)
	case "enableAutostop", "autostopTimeoutEst", "autostopTimeoutInit", "autostopPeriod", "debugAutostop":
		return 10

	// CurseForge (11)
	case "cfApiKey", "cfApiKeyFile", "cfPageUrl", "cfSlug", "cfFileId", "cfFilenameMatcher",
		"cfExcludeIncludeFile", "cfExcludeMods", "cfForceIncludeMods", "cfForceSynchronize",
		"cfSetLevelFrom", "cfParallelDownloads", "cfOverridesSkipExisting", "cfForceReinstallModloader":
		return 11

	// Modrinth (12)
	case "modrinthModpack", "modrinthModpackVersionType", "modrinthVersion", "modrinthLoader",
		"modrinthIgnoreMissingFiles", "modrinthExcludeFiles", "modrinthForceIncludeFiles",
		"modrinthForceSynchronize", "modrinthDefaultExcludeIncludes", "modrinthOverridesExclusions",
		"modrinthProjects", "modrinthDownloadDependencies", "modrinthProjectsDefaultVersionType",
		"versionFromModrinthProjects":
		return 12

	default:
		return -1 // Unknown category
	}
}

// toAnyPb converts a Go value to protobuf Any
func toAnyPb(value any) (*anypb.Any, error) {
	switch v := value.(type) {
	case string:
		return anypb.New(wrapperspb.String(v))
	case int:
		return anypb.New(wrapperspb.Int64(int64(v)))
	case int32:
		return anypb.New(wrapperspb.Int32(v))
	case int64:
		return anypb.New(wrapperspb.Int64(v))
	case bool:
		return anypb.New(wrapperspb.Bool(v))
	case float32:
		return anypb.New(wrapperspb.Float(v))
	case float64:
		return anypb.New(wrapperspb.Double(v))
	default:
		return nil, fmt.Errorf("unsupported type: %T", value)
	}
}

// fromAnyPb extracts a Go value from protobuf Any
func fromAnyPb(anyVal *anypb.Any) (any, error) {
	if anyVal == nil {
		return nil, nil
	}

	// Try to unmarshal as different wrapper types
	var strVal wrapperspb.StringValue
	if err := anyVal.UnmarshalTo(&strVal); err == nil {
		return strVal.Value, nil
	}

	var int64Val wrapperspb.Int64Value
	if err := anyVal.UnmarshalTo(&int64Val); err == nil {
		return int64Val.Value, nil
	}

	var int32Val wrapperspb.Int32Value
	if err := anyVal.UnmarshalTo(&int32Val); err == nil {
		return int32Val.Value, nil
	}

	var boolVal wrapperspb.BoolValue
	if err := anyVal.UnmarshalTo(&boolVal); err == nil {
		return boolVal.Value, nil
	}

	var floatVal wrapperspb.FloatValue
	if err := anyVal.UnmarshalTo(&floatVal); err == nil {
		return floatVal.Value, nil
	}

	var doubleVal wrapperspb.DoubleValue
	if err := anyVal.UnmarshalTo(&doubleVal); err == nil {
		return doubleVal.Value, nil
	}

	return nil, fmt.Errorf("unsupported Any type: %s", anyVal.TypeUrl)
}

// applyConfigUpdates applies updates from a protobuf Any map to a config struct
func applyConfigUpdates(config any, updates map[string]*anypb.Any) error {
	configValue := reflect.ValueOf(config).Elem()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		anyVal, exists := updates[jsonTag]
		if !exists {
			continue
		}

		// Extract value from Any
		value, err := fromAnyPb(anyVal)
		if err != nil {
			return fmt.Errorf("failed to extract value for field %s: %w", jsonTag, err)
		}

		fieldValue := configValue.Field(i)
		if !fieldValue.CanSet() {
			continue
		}

		// Handle nil values
		if value == nil {
			fieldValue.Set(reflect.Zero(fieldValue.Type()))
			continue
		}

		// Handle pointer types
		if fieldValue.Kind() == reflect.Ptr {
			elemType := fieldValue.Type().Elem()
			newValue := reflect.New(elemType)
			elem := newValue.Elem()

			switch elemType.Kind() {
			case reflect.String:
				if str, ok := value.(string); ok {
					elem.SetString(str)
					fieldValue.Set(newValue)
				}
			case reflect.Int, reflect.Int32, reflect.Int64:
				if num, ok := value.(int64); ok {
					elem.SetInt(num)
					fieldValue.Set(newValue)
				} else if num, ok := value.(int32); ok {
					elem.SetInt(int64(num))
					fieldValue.Set(newValue)
				} else if num, ok := value.(float64); ok {
					elem.SetInt(int64(num))
					fieldValue.Set(newValue)
				}
			case reflect.Bool:
				if b, ok := value.(bool); ok {
					elem.SetBool(b)
					fieldValue.Set(newValue)
				}
			}
		} else {
			// Non-pointer fields
			switch fieldValue.Kind() {
			case reflect.String:
				if str, ok := value.(string); ok {
					fieldValue.SetString(str)
				}
			case reflect.Int, reflect.Int32, reflect.Int64:
				if num, ok := value.(int64); ok {
					fieldValue.SetInt(num)
				} else if num, ok := value.(int32); ok {
					fieldValue.SetInt(int64(num))
				} else if num, ok := value.(float64); ok {
					fieldValue.SetInt(int64(num))
				}
			case reflect.Bool:
				if b, ok := value.(bool); ok {
					fieldValue.SetBool(b)
				}
			}
		}
	}

	return nil
}
