package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	models "github.com/nickheyer/discopanel/internal/db"
	"gorm.io/gorm"
)

// ConfigProperty represents a single configuration property with metadata
type ConfigProperty struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Value        any      `json:"value"`
	DefaultValue any      `json:"default"`
	Type         string   `json:"type"` // text, number, checkbox, select, password
	Description  string   `json:"description"`
	Required     bool     `json:"required"`
	System       bool     `json:"system"`    // If true, field is auto-populated and read-only
	Ephemeral    bool     `json:"ephemeral"` // If true, field is cleared after server start
	EnvVar       string   `json:"env_var"`
	Options      []string `json:"options,omitempty"` // For select type
}

// ConfigCategory represents a group of related configuration properties
type ConfigCategory struct {
	Name       string           `json:"name"`
	Properties []ConfigProperty `json:"properties"`
}

func (s *Server) handleGetServerConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Get server to ensure it exists and sync config
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Ensure config is synced with server
	if err := s.store.SyncServerConfigWithServer(ctx, server); err != nil {
		s.log.Error("Failed to sync server config: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to sync server configuration")
		return
	}

	// Get the synced config
	config, err := s.store.GetServerConfig(ctx, serverID)
	if err != nil {
		s.log.Error("Failed to get server config: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to get server configuration")
		return
	}

	// Convert to categorized format
	categories := buildConfigCategories(config)
	s.respondJSON(w, http.StatusOK, categories)
}

func (s *Server) handleUpdateServerConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Get server info
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Get existing config
	config, err := s.store.GetServerConfig(ctx, serverID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			config = s.store.CreateDefaultServerConfig(serverID)
		} else {
			s.log.Error("Failed to get server config: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to get server configuration")
			return
		}
	}

	// Decode updates
	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update config fields using reflection
	configValue := reflect.ValueOf(config).Elem()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		if value, exists := updates[jsonTag]; exists {
			fieldValue := configValue.Field(i)
			if fieldValue.CanSet() {
				// Handle nil values
				if value == nil {
					fieldValue.Set(reflect.Zero(fieldValue.Type()))
					continue
				}

				// Handle pointer types
				if fieldValue.Kind() == reflect.Ptr {
					// Get the element type
					elemType := fieldValue.Type().Elem()

					// Create a new pointer to hold the value
					newValue := reflect.New(elemType)
					elem := newValue.Elem()

					switch elemType.Kind() {
					case reflect.String:
						if str, ok := value.(string); ok {
							elem.SetString(str)
							fieldValue.Set(newValue)
						}
					case reflect.Int, reflect.Int32, reflect.Int64:
						if num, ok := value.(float64); ok {
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
					// Non-pointer fields (ID, ServerID, UpdatedAt)
					switch fieldValue.Kind() {
					case reflect.String:
						if str, ok := value.(string); ok {
							fieldValue.SetString(str)
						}
					}
				}
			}
		}
	}

	// Save updated config
	if err := s.store.SaveServerConfig(ctx, config); err != nil {
		s.log.Error("Failed to save server config: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to save server configuration")
		return
	}

	// If server has a container, we need to recreate it with the new config
	// Docker containers have immutable environment variables
	if server.ContainerID != "" {
		oldContainerID := server.ContainerID

		// Check if server is running
		wasRunning := false
		if server.Status == models.StatusRunning {
			wasRunning = true
			// Stop the container first
			if err := s.docker.StopContainer(ctx, oldContainerID); err != nil {
				s.log.Error("Failed to stop container for config update: %v", err)
				s.respondError(w, http.StatusInternalServerError, "Failed to stop server for configuration update")
				return
			}

			// Wait for clean shutdown
			time.Sleep(2 * time.Second)
		}

		// Remove old container
		if err := s.docker.RemoveContainer(ctx, oldContainerID); err != nil {
			s.log.Error("Failed to remove old container: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to remove old container")
			return
		}

		// Create new container with updated config
		newContainerID, err := s.docker.CreateContainer(ctx, server, config)
		if err != nil {
			s.log.Error("Failed to create new container with updated config: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to create new container with updated configuration")
			return
		}

		// Update server with new container ID
		server.ContainerID = newContainerID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server with new container ID: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to update server")
			return
		}

		// Restart if it was running
		if wasRunning {
			if err := s.docker.StartContainer(ctx, newContainerID); err != nil {
				s.log.Error("Failed to restart container after config update: %v", err)
				// Don't fail the whole operation, config is already saved
			} else {
				server.Status = models.StatusStarting
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
	categories := buildConfigCategories(config)
	s.respondJSON(w, http.StatusOK, categories)
}

// buildConfigCategories converts ServerConfig struct to categorized format for UI
func buildConfigCategories(config any) []ConfigCategory {
	categories := []ConfigCategory{
		{Name: "JVM Configuration", Properties: []ConfigProperty{}},
		{Name: "Server Settings", Properties: []ConfigProperty{}},
		{Name: "Game Settings", Properties: []ConfigProperty{}},
		{Name: "World Generation", Properties: []ConfigProperty{}},
		{Name: "RCON", Properties: []ConfigProperty{}},
		{Name: "Resource Pack", Properties: []ConfigProperty{}},
		{Name: "Whitelist", Properties: []ConfigProperty{}},
		{Name: "Auto-Pause", Properties: []ConfigProperty{}},
		{Name: "Auto-Stop", Properties: []ConfigProperty{}},
		{Name: "CurseForge", Properties: []ConfigProperty{}},
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
		if fieldValue.Kind() == reflect.Ptr {
			if fieldValue.IsNil() {
				value = nil
			} else {
				value = fieldValue.Elem().Interface()
			}
		}

		// Parse default value based on field type (checking the underlying type for pointers)
		var defaultValue any
		fieldType := field.Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		switch fieldType.Kind() {
		case reflect.Bool:
			defaultValue = defaultTag == "true"
		case reflect.Int, reflect.Int32, reflect.Int64:
			// Try to parse as int
			if defaultTag != "" {
				if intVal, err := strconv.ParseInt(defaultTag, 10, 64); err == nil {
					defaultValue = intVal
				} else {
					// If parsing fails, default to 0
					defaultValue = 0
				}
			} else {
				defaultValue = 0
			}
		default:
			defaultValue = defaultTag
		}

		// Use label if provided, otherwise use the json tag
		label := labelTag
		if label == "" {
			label = jsonTag
		}

		prop := ConfigProperty{
			Key:          jsonTag,
			Label:        label,
			Value:        value,
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
			switch jsonTag {
			case "difficulty":
				prop.Options = []string{"peaceful", "easy", "normal", "hard"}
			case "mode":
				prop.Options = []string{"creative", "survival", "adventure", "spectator"}
			case "cfSetLevelFrom":
				prop.Options = []string{"", "WORLD_FILE", "OVERRIDES"}
			}
		}

		// Categorize the property
		categoryIndex := getCategoryIndex(jsonTag)
		if categoryIndex >= 0 && categoryIndex < len(categories) {
			categories[categoryIndex].Properties = append(categories[categoryIndex].Properties, prop)
		}
	}

	// Remove empty categories
	var nonEmptyCategories []ConfigCategory
	for _, cat := range categories {
		if len(cat.Properties) > 0 {
			nonEmptyCategories = append(nonEmptyCategories, cat)
		}
	}

	return nonEmptyCategories
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
	case "type", "eula", "version", "motd", "icon", "overrideIcon", "serverName",
		"serverPort", "console", "gui", "stopDuration", "setupOnly", "execDirectly",
		"stopServerAnnounceDelay", "proxy", "useFlareFlags", "useSimdFlags":
		return 1

	// Game Settings (2)
	case "difficulty", "maxPlayers", "allowNether", "announcePlayerAchievements",
		"enableCommandBlock", "forceGamemode", "hardcore", "snooperEnabled", "maxBuildHeight",
		"spawnAnimals", "spawnMonsters", "spawnNpcs", "spawnProtection", "viewDistance",
		"mode", "pvp", "onlineMode", "allowFlight", "playerIdleTimeout", "syncChunkWrites",
		"enableStatus", "entityBroadcastRangePercentage", "functionPermissionLevel",
		"networkCompressionThreshold", "opPermissionLevel", "preventProxyConnections",
		"useNativeTransport", "simulationDistance":
		return 2

	// World Generation (3)
	case "generateStructures", "maxWorldSize", "seed", "levelType", "generatorSettings", "level":
		return 3

	// RCON (4)
	case "enableRcon", "rconPassword", "rconPort", "broadcastRconToOps", "rconCmdsStartup",
		"rconCmdsOnConnect", "rconCmdsFirstConnect", "rconCmdsOnDisconnect", "rconCmdsLastDisconnect":
		return 4

	// Resource Pack (5)
	case "resourcePack", "resourcePackSha1", "resourcePackEnforce":
		return 5

	// Whitelist (6)
	case "enableWhitelist", "whitelist", "whitelistFile", "overrideWhitelist":
		return 6

	// Auto-Pause (7)
	case "enableAutopause", "autopauseTimeoutEst", "autopauseTimeoutInit", "autopauseTimeoutKn",
		"autopausePeriod", "autopauseKnockInterface", "debugAutopause":
		return 7

	// Auto-Stop (8)
	case "enableAutostop", "autostopTimeoutEst", "autostopTimeoutInit", "autostopPeriod", "debugAutostop":
		return 8

	// CurseForge (9)
	case "cfApiKey", "cfApiKeyFile", "cfPageUrl", "cfSlug", "cfFileId", "cfFilenameMatcher",
		"cfExcludeIncludeFile", "cfExcludeMods", "cfForceIncludeMods", "cfForceSynchronize",
		"cfSetLevelFrom", "cfParallelDownloads", "cfOverridesSkipExisting", "cfForceReinstallModloader":
		return 9

	default:
		return -1 // Unknown category
	}
}

func (s *Server) handleGetGlobalSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	config, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to get global settings")
		return
	}

	// Convert to categorized format
	categories := buildConfigCategories(config)
	s.respondJSON(w, http.StatusOK, categories)
}

func (s *Server) handleUpdateGlobalSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get existing config
	config, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to get global settings")
		return
	}

	// Decode updates
	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update config fields using reflection (same logic as server config update)
	configValue := reflect.ValueOf(config).Elem()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		if value, exists := updates[jsonTag]; exists {
			fieldValue := configValue.Field(i)
			if fieldValue.CanSet() {
				// Handle nil values
				if value == nil {
					fieldValue.Set(reflect.Zero(fieldValue.Type()))
					continue
				}

				// Handle pointer types
				if fieldValue.Kind() == reflect.Ptr {
					// Get the element type
					elemType := fieldValue.Type().Elem()

					// Create a new pointer to hold the value
					newValue := reflect.New(elemType)
					elem := newValue.Elem()

					switch elemType.Kind() {
					case reflect.String:
						if str, ok := value.(string); ok {
							elem.SetString(str)
							fieldValue.Set(newValue)
						}
					case reflect.Int, reflect.Int32, reflect.Int64:
						if num, ok := value.(float64); ok {
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
					// Non-pointer fields (ID, ServerID, UpdatedAt)
					switch fieldValue.Kind() {
					case reflect.String:
						if str, ok := value.(string); ok {
							fieldValue.SetString(str)
						}
					}
				}
			}
		}
	}

	// Save updated config
	if err := s.store.UpdateGlobalSettings(ctx, config); err != nil {
		s.log.Error("Failed to save global settings: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to save global settings")
		return
	}

	// Return updated config
	categories := buildConfigCategories(config)
	s.respondJSON(w, http.StatusOK, categories)
}
