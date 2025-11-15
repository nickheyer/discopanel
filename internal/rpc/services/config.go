package services

import (
	"context"
	"errors"
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
	config *config.Config
	docker *docker.Client
	log    *logger.Logger
}

// NewConfigService creates a new config service
func NewConfigService(store *storage.Store, cfg *config.Config, docker *docker.Client, log *logger.Logger) *ConfigService {
	return &ConfigService{
		store:  store,
		config: cfg,
		docker: docker,
		log:    log,
	}
}

// GetServerConfig gets server configuration
func (s *ConfigService) GetServerConfig(ctx context.Context, req *connect.Request[v1.GetServerConfigRequest]) (*connect.Response[v1.GetServerConfigResponse], error) {
	msg := req.Msg

	// Get server to ensure it exists and sync config
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Ensure config is synced with server
	if err := s.store.SyncServerConfigWithServer(ctx, server); err != nil {
		s.log.Error("Failed to sync server config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to sync server configuration"))
	}

	// Get the synced config
	config, err := s.store.GetServerConfig(ctx, msg.ServerId)
	if err != nil {
		s.log.Error("Failed to get server config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get server configuration"))
	}

	// Convert to categorized format
	categories, err := buildConfigCategories(config)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to format configuration"))
	}

	return connect.NewResponse(&v1.GetServerConfigResponse{
		Categories: categories,
	}), nil
}

// UpdateServerConfig updates server configuration
func (s *ConfigService) UpdateServerConfig(ctx context.Context, req *connect.Request[v1.UpdateServerConfigRequest]) (*connect.Response[v1.UpdateServerConfigResponse], error) {
	msg := req.Msg

	// Get server info
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Get existing config
	config, err := s.store.GetServerConfig(ctx, msg.ServerId)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			config = s.store.CreateDefaultServerConfig(msg.ServerId)
		} else {
			s.log.Error("Failed to get server config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get server configuration"))
		}
	}

	// Apply updates using reflection
	if err := applyConfigUpdates(config, msg.Updates); err != nil {
		s.log.Error("Failed to apply config updates: %v", err)
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("failed to apply configuration updates"))
	}

	// Save updated config
	if err := s.store.SaveServerConfig(ctx, config); err != nil {
		s.log.Error("Failed to save server config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to save server configuration"))
	}

	// If server has a container, we need to recreate it with the new config
	// Docker containers have immutable environment variables
	if server.ContainerID != "" && s.docker != nil {
		oldContainerID := server.ContainerID

		// Check if server is running
		wasRunning := false
		if server.Status == storage.StatusRunning {
			wasRunning = true
			// Stop the container first
			if err := s.docker.StopContainer(ctx, oldContainerID); err != nil {
				s.log.Error("Failed to stop container for config update: %v", err)
				return nil, connect.NewError(connect.CodeInternal, errors.New("failed to stop server for configuration update"))
			}

			// Wait for clean shutdown
			time.Sleep(2 * time.Second)
		}

		// Remove old container
		if err := s.docker.RemoveContainer(ctx, oldContainerID); err != nil {
			s.log.Error("Failed to remove old container: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to remove old container"))
		}

		// Create new container with updated config
		newContainerID, err := s.docker.CreateContainer(ctx, server, config)
		if err != nil {
			s.log.Error("Failed to create new container with updated config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create new container with updated configuration"))
		}

		// Update server with new container ID
		server.ContainerID = newContainerID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server with new container ID: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update server"))
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
	categories, err := buildConfigCategories(config)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to format configuration"))
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
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get auth configuration"))
	}

	if authConfig.Enabled {
		user := auth.GetUserFromContext(ctx)
		if user == nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
		}
		if !auth.CheckPermission(user, storage.RoleAdmin) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin access required"))
		}
	}

	config, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get global settings"))
	}

	// Convert to categorized format
	categories, err := buildConfigCategories(config)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to format configuration"))
	}

	return connect.NewResponse(&v1.GetGlobalSettingsResponse{
		Categories: categories,
	}), nil
}

// UpdateGlobalSettings updates the global settings
func (s *ConfigService) UpdateGlobalSettings(ctx context.Context, req *connect.Request[v1.UpdateGlobalSettingsRequest]) (*connect.Response[v1.UpdateGlobalSettingsResponse], error) {
	msg := req.Msg

	// Check if auth is enabled and enforce admin role
	authConfig, _, err := s.store.GetAuthConfig(ctx)
	if err != nil {
		s.log.Error("Failed to get auth config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get auth configuration"))
	}

	if authConfig.Enabled {
		user := auth.GetUserFromContext(ctx)
		if user == nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
		}
		if !auth.CheckPermission(user, storage.RoleAdmin) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin access required"))
		}
	}

	// Get existing config
	config, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get global settings"))
	}

	// Apply updates using reflection
	if err := applyConfigUpdates(config, msg.Updates); err != nil {
		s.log.Error("Failed to apply config updates: %v", err)
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("failed to apply configuration updates"))
	}

	// Save updated config
	if err := s.store.UpdateGlobalSettings(ctx, config); err != nil {
		s.log.Error("Failed to save global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to save global settings"))
	}

	// Return updated config
	categories, err := buildConfigCategories(config)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to format configuration"))
	}

	return connect.NewResponse(&v1.UpdateGlobalSettingsResponse{
		Categories: categories,
	}), nil
}

// applyConfigUpdates applies updates to a config struct using reflection
func applyConfigUpdates(config any, updates map[string]*anypb.Any) error {
	configValue := reflect.ValueOf(config).Elem()
	configType := configValue.Type()

	for key, anyValue := range updates {
		// Find the field by json tag
		fieldIndex := -1
		for i := 0; i < configType.NumField(); i++ {
			field := configType.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag == key {
				fieldIndex = i
				break
			}
		}

		if fieldIndex == -1 {
			continue // Skip unknown fields
		}

		fieldValue := configValue.Field(fieldIndex)
		if !fieldValue.CanSet() {
			continue
		}

		// Unwrap the Any value
		if anyValue == nil {
			fieldValue.Set(reflect.Zero(fieldValue.Type()))
			continue
		}

		// Try to unmarshal as different wrapper types
		var value any

		// Try string wrapper
		strVal := &wrapperspb.StringValue{}
		if anyValue.MessageIs(strVal) {
			if err := anyValue.UnmarshalTo(strVal); err == nil {
				value = strVal.Value
			}
		}
		// Try int64 wrapper
		int64Val := &wrapperspb.Int64Value{}
		if anyValue.MessageIs(int64Val) {
			if err := anyValue.UnmarshalTo(int64Val); err == nil {
				value = int64Val.Value
			}
		}
		// Try int32 wrapper
		int32Val := &wrapperspb.Int32Value{}
		if anyValue.MessageIs(int32Val) {
			if err := anyValue.UnmarshalTo(int32Val); err == nil {
				value = int64(int32Val.Value)
			}
		}
		// Try bool wrapper
		boolVal := &wrapperspb.BoolValue{}
		if anyValue.MessageIs(boolVal) {
			if err := anyValue.UnmarshalTo(boolVal); err == nil {
				value = boolVal.Value
			}
		}
		// Try double wrapper
		doubleVal := &wrapperspb.DoubleValue{}
		if anyValue.MessageIs(doubleVal) {
			if err := anyValue.UnmarshalTo(doubleVal); err == nil {
				value = doubleVal.Value
			}
		}
		// Try float wrapper
		floatVal := &wrapperspb.FloatValue{}
		if anyValue.MessageIs(floatVal) {
			if err := anyValue.UnmarshalTo(floatVal); err == nil {
				value = float64(floatVal.Value)
			}
		}

		if value == nil {
			continue
		}

		// Handle pointer types
		if fieldValue.Kind() == reflect.Pointer {
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
				switch v := value.(type) {
				case int64:
					elem.SetInt(v)
					fieldValue.Set(newValue)
				case float64:
					elem.SetInt(int64(v))
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
				switch v := value.(type) {
				case int64:
					fieldValue.SetInt(v)
				case float64:
					fieldValue.SetInt(int64(v))
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

// buildConfigCategories converts ServerConfig struct to categorized format for UI
func buildConfigCategories(config any) ([]*v1.ConfigCategory, error) {
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

	configValue := reflect.ValueOf(config).Elem()
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
		if fieldValue.Kind() == reflect.Pointer {
			if fieldValue.IsNil() {
				value = nil
			} else {
				value = fieldValue.Elem().Interface()
			}
		}

		// Convert value to Any proto
		var anyValue *anypb.Any
		var err error
		if value != nil {
			switch v := value.(type) {
			case string:
				anyValue, err = anypb.New(wrapperspb.String(v))
			case int, int32, int64:
				anyValue, err = anypb.New(wrapperspb.Int64(reflect.ValueOf(v).Int()))
			case bool:
				anyValue, err = anypb.New(wrapperspb.Bool(v))
			case float32, float64:
				anyValue, err = anypb.New(wrapperspb.Double(reflect.ValueOf(v).Float()))
			}
			if err != nil {
				return nil, err
			}
		}

		// Parse default value based on field type
		var defaultValue *anypb.Any
		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		switch fieldType.Kind() {
		case reflect.Bool:
			defaultValue, _ = anypb.New(wrapperspb.Bool(defaultTag == "true"))
		case reflect.Int, reflect.Int32, reflect.Int64:
			if defaultTag != "" {
				if intVal, err := strconv.ParseInt(defaultTag, 10, 64); err == nil {
					defaultValue, _ = anypb.New(wrapperspb.Int64(intVal))
				} else {
					defaultValue, _ = anypb.New(wrapperspb.Int64(0))
				}
			} else {
				defaultValue, _ = anypb.New(wrapperspb.Int64(0))
			}
		default:
			defaultValue, _ = anypb.New(wrapperspb.String(defaultTag))
		}

		// Use label if provided, otherwise use the json tag
		label := labelTag
		if label == "" {
			label = jsonTag
		}

		prop := &v1.ConfigProperty{
			Key:          jsonTag,
			Label:        label,
			Value:        anyValue,
			DefaultValue: defaultValue,
			Type:         inputTag,
			Description:  descTag,
			Required:     requiredTag == "true",
			System:       systemTag == "true",
			Ephemeral:    ephemeralTag == "true",
			EnvVar:       envTag,
		}

		// Add options for select fields
		if inputTag == "select" {
			prop.Options = getSelectOptions(jsonTag)
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

// getSelectOptions returns options for select fields
func getSelectOptions(key string) []string {
	switch key {
	case "difficulty":
		return []string{"peaceful", "easy", "normal", "hard"}
	case "mode":
		return []string{"creative", "survival", "adventure", "spectator"}
	case "cfSetLevelFrom":
		return []string{"", "WORLD_FILE", "OVERRIDES"}
	case "userApiProvider":
		return []string{"playerdb", "mojang"}
	case "existingOpsFile":
		return []string{"SKIP", "SYNCHRONIZE", "MERGE", "SYNC_FILE_MERGE_LIST"}
	case "existingWhitelistFile":
		return []string{"SKIP", "SYNCHRONIZE", "MERGE", "SYNC_FILE_MERGE_LIST"}
	case "modrinthDownloadDependencies":
		return []string{"none", "required", "optional"}
	case "modrinthProjectsDefaultVersionType":
		return []string{"release", "beta", "alpha"}
	case "modrinthModpackVersionType":
		return []string{"release", "beta", "alpha"}
	case "modrinthLoader":
		return []string{"forge", "fabric", "quilt"}
	default:
		return []string{}
	}
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
