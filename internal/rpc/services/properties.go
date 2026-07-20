package services

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/lifecycle"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/pkg/config"
	"github.com/nickheyer/discopanel/pkg/logger"
	"github.com/nickheyer/discopanel/pkg/minecraft"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"gorm.io/gorm"
)

var _ discopanelv1connect.PropertiesServiceHandler = (*PropertiesService)(nil)

type PropertiesService struct {
	store     *storage.Store
	config    *config.Config
	docker    *docker.Client
	lifecycle *lifecycle.Manager
	rec       *metrics.Recorder
	log       *logger.Logger
}

// Creates new config service
func NewPropertiesService(store *storage.Store, cfg *config.Config, docker *docker.Client, lifecycleManager *lifecycle.Manager, rec *metrics.Recorder, log *logger.Logger) *PropertiesService {
	return &PropertiesService{
		store:     store,
		config:    cfg,
		docker:    docker,
		lifecycle: lifecycleManager,
		rec:       rec,
		log:       log,
	}
}

// Gets server config
func (s *PropertiesService) GetServerProperties(ctx context.Context, req *connect.Request[v1.GetServerPropertiesRequest]) (*connect.Response[v1.GetServerPropertiesResponse], error) {
	msg := req.Msg

	// Get server to ensure it exists
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Ensure config is synced with server
	if err := s.store.SyncServerPropertiesWithServer(ctx, server); err != nil {
		s.log.Error("Failed to sync server config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to sync server properties"))
	}

	// Get the synced config
	config, err := s.store.GetServerProperties(ctx, msg.ServerId)
	if err != nil {
		s.log.Error("Failed to get server config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get server properties"))
	}

	// Convert to categorized format
	categories, err := buildPropertyCategories(config)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to format properties"))
	}

	return connect.NewResponse(&v1.GetServerPropertiesResponse{
		Categories: categories,
	}), nil
}

// Updates server config
func (s *PropertiesService) UpdateServerProperties(ctx context.Context, req *connect.Request[v1.UpdateServerPropertiesRequest]) (*connect.Response[v1.UpdateServerPropertiesResponse], error) {
	msg := req.Msg

	// Get server info
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Get existing config
	config, err := s.store.GetServerProperties(ctx, msg.ServerId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			config = s.store.CreateDefaultServerProperties(msg.ServerId)
		} else {
			s.log.Error("Failed to get server config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get server properties"))
		}
	}

	// Apply updates w/ reflection
	if err := applyPropertyUpdates(config, msg.Updates); err != nil {
		s.log.Error("Failed to apply config updates: %v", err)
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("failed to apply property updates"))
	}

	// Save updated config
	if err := s.store.UpdateServerProperties(ctx, config); err != nil {
		s.log.Error("Failed to save server config: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to save server properties"))
	}
	s.rec.Record(ctx, server.Id, "properties.update", metrics.Attrs{"changed": strconv.Itoa(len(msg.Updates))}, "updated server properties (%d changed)", len(msg.Updates))

	// Restarts running servers so new config applies
	if server.ContainerId != "" && s.lifecycle != nil {
		s.applyPropertiesToRunningServer(ctx, server)
	}

	// Reconciles proxy route right away without server start
	if s.lifecycle != nil {
		s.lifecycle.SyncProxyRoute(ctx, msg.ServerId)
	}

	// Return updated config
	categories, err := buildPropertyCategories(config)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to format properties"))
	}

	return connect.NewResponse(&v1.UpdateServerPropertiesResponse{
		Categories: categories,
	}), nil
}

// Gets global settings
func (s *PropertiesService) GetGlobalSettings(ctx context.Context, req *connect.Request[v1.GetGlobalSettingsRequest]) (*connect.Response[v1.GetGlobalSettingsResponse], error) {
	config, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get global settings"))
	}

	categories, err := buildPropertyCategories(config)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to format properties"))
	}

	return connect.NewResponse(&v1.GetGlobalSettingsResponse{
		Categories: categories,
	}), nil
}

// Updates global settings
func (s *PropertiesService) UpdateGlobalSettings(ctx context.Context, req *connect.Request[v1.UpdateGlobalSettingsRequest]) (*connect.Response[v1.UpdateGlobalSettingsResponse], error) {
	msg := req.Msg
	config, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get global settings"))
	}

	if err := applyPropertyUpdates(config, msg.Updates); err != nil {
		s.log.Error("Failed to apply config updates: %v", err)
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("failed to apply property updates"))
	}

	if err := s.store.UpdateGlobalSettings(ctx, config); err != nil {
		s.log.Error("Failed to save global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to save global settings"))
	}

	categories, err := buildPropertyCategories(config)
	if err != nil {
		s.log.Error("Failed to build config categories: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to format properties"))
	}

	return connect.NewResponse(&v1.UpdateGlobalSettingsResponse{
		Categories: categories,
	}), nil
}

// Restarts running server so saved properties take effect
func (s *PropertiesService) applyPropertiesToRunningServer(reqCtx context.Context, server *v1.Server) {
	switch server.Status {
	case v1.ServerStatus_SERVER_STATUS_RUNNING, v1.ServerStatus_SERVER_STATUS_STARTING, v1.ServerStatus_SERVER_STATUS_UNHEALTHY, v1.ServerStatus_SERVER_STATUS_PAUSED:
		go func() {
			ctx, cancel := context.WithTimeout(detach(reqCtx), 30*time.Minute)
			defer cancel()
			if err := s.lifecycle.Restart(ctx, server.Id); err != nil {
				s.log.Error("Failed to restart server %s after config update: %v", server.Name, err)
			}
		}()
	}
}

// Maps updates w/ reflection
func applyPropertyUpdates(config any, updates map[string]string) error {
	configValue := reflect.ValueOf(config).Elem()
	configType := configValue.Type()

	for key, strValue := range updates {
		// Find the field by json tag
		fieldIndex := -1
		for i := 0; i < configType.NumField(); i++ {
			field := configType.Field(i)
			jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]
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

		// Unset
		if strValue == "" {
			if fieldValue.Kind() == reflect.Pointer {
				fieldValue.Set(reflect.Zero(fieldValue.Type()))
				continue
			}
			fieldValue.Set(reflect.Zero(fieldValue.Type()))
			continue
		}

		targetType := fieldValue.Type()
		isPtr := targetType.Kind() == reflect.Pointer
		if isPtr {
			targetType = targetType.Elem()
		}

		var val reflect.Value

		switch targetType.Kind() {
		case reflect.String:
			val = reflect.ValueOf(strValue)
		case reflect.Bool:
			b, err := strconv.ParseBool(strValue)
			if err != nil {
				return fmt.Errorf("invalid boolean for key %s: %v", key, err)
			}
			val = reflect.ValueOf(b)
		case reflect.Int, reflect.Int32, reflect.Int64:
			i, err := strconv.ParseInt(strValue, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid integer for key %s: %v", key, err)
			}
			// Convert to specific int type
			if targetType.Kind() == reflect.Int {
				val = reflect.ValueOf(int(i))
			} else if targetType.Kind() == reflect.Int32 {
				val = reflect.ValueOf(int32(i))
			} else {
				val = reflect.ValueOf(i)
			}
		case reflect.Float32, reflect.Float64:
			f, err := strconv.ParseFloat(strValue, 64)
			if err != nil {
				return fmt.Errorf("invalid float for key %s: %v", key, err)
			}
			if targetType.Kind() == reflect.Float32 {
				val = reflect.ValueOf(float32(f))
			} else {
				val = reflect.ValueOf(f)
			}
		default:
			// Skip complex types we don't support updating this way
			continue
		}

		if isPtr {
			// New pointer and set value
			ptr := reflect.New(targetType)
			ptr.Elem().Set(val)
			fieldValue.Set(ptr)
		} else {
			fieldValue.Set(val)
		}
	}

	return nil
}

// Category slugs on struct tags mapped to display order
var propertyCategorySlugs = []struct {
	Slug string
	Name string
}{
	{"jvm", "JVM"},
	{"server", "Server Settings"},
	{"game", "Game Settings"},
	{"world", "World Generation"},
	{"rcon", "RCON"},
	{"resourcepack", "Resource Pack"},
	{"management", "Management Server"},
	{"ops", "Ops/Admins"},
	{"whitelist", "Whitelist"},
	{"autopause", "Auto-Pause"},
	{"autostop", "Auto-Stop"},
	{"curseforge", "CurseForge"},
	{"modrinth", "Modrinth"},
	{"proxy", "Proxy"},
}

func propertyCategoryIndex(slug string) int {
	for i, c := range propertyCategorySlugs {
		if c.Slug == slug {
			return i
		}
	}
	return -1
}

// Reads one settings field by its property key
func propertyValueByKey(config any, key string) string {
	configValue := reflect.ValueOf(config).Elem()
	configType := configValue.Type()
	for i := 0; i < configType.NumField(); i++ {
		if strings.Split(configType.Field(i).Tag.Get("json"), ",")[0] != key {
			continue
		}
		fieldValue := configValue.Field(i)
		if fieldValue.Kind() == reflect.Pointer {
			if fieldValue.IsNil() {
				return ""
			}
			fieldValue = fieldValue.Elem()
		}
		return fmt.Sprintf("%v", fieldValue.Interface())
	}
	return ""
}

func buildPropertyCategories(config any) ([]*v1.PropertyCategory, error) {
	categories := make([]*v1.PropertyCategory, 0, len(propertyCategorySlugs))
	for _, c := range propertyCategorySlugs {
		categories = append(categories, &v1.PropertyCategory{Name: c.Name, Properties: []*v1.ServerProperty{}})
	}

	configValue := reflect.ValueOf(config).Elem()
	configType := configValue.Type()

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonTag == "" || jsonTag == "-" || jsonTag == "id" || jsonTag == "server_id" || jsonTag == "updated_at" {
			continue
		}

		// Metadata tags
		envTag := field.Tag.Get("env")
		if envTag == "" {
			// Falls back to prop tag for display
			envTag = field.Tag.Get("prop")
		}
		defaultTag := field.Tag.Get("default")
		descTag := field.Tag.Get("desc")
		inputTag := field.Tag.Get("input")
		requiredTag := field.Tag.Get("required")
		labelTag := field.Tag.Get("label")
		systemTag := field.Tag.Get("system")
		ephemeralTag := field.Tag.Get("ephemeral")

		fieldValue := configValue.Field(i)
		var strValue string
		if fieldValue.Kind() == reflect.Pointer {
			if fieldValue.IsNil() {
				// Explicitly nil/unset
				strValue = ""
			} else {
				// Dereference and stringify
				strValue = fmt.Sprintf("%v", fieldValue.Elem().Interface())
			}
		} else {
			// Stringify direct value
			strValue = fmt.Sprintf("%v", fieldValue.Interface())
		}

		label := labelTag
		if label == "" {
			label = jsonTag
		}

		prop := &v1.ServerProperty{
			Key:         jsonTag,
			Label:       label,
			Value:       strValue,
			Type:        inputTag,
			Description: descTag,
			Required:    requiredTag == "true",
			System:      systemTag == "true",
			Ephemeral:   ephemeralTag == "true",
			EnvVar:      envTag,
		}

		// Only set default when tag specifies one
		if defaultTag != "" {
			prop.DefaultValue = &defaultTag
		}

		if inputTag == "select" {
			prop.Options = getSelectOptions(jsonTag)
		}

		categoryIndex := propertyCategoryIndex(field.Tag.Get("category"))
		if categoryIndex >= 0 {
			categories[categoryIndex].Properties = append(categories[categoryIndex].Properties, prop)
		}
	}

	// Filter empty
	var nonEmptyCategories []*v1.PropertyCategory
	for _, cat := range categories {
		if len(cat.Properties) > 0 {
			nonEmptyCategories = append(nonEmptyCategories, cat)
		}
	}

	return nonEmptyCategories, nil
}

// Returns options for select fields
func getSelectOptions(key string) []string {
	switch key {
	case "difficulty":
		return []string{"peaceful", "easy", "normal", "hard"}
	case "mode":
		return []string{"creative", "survival", "adventure", "spectator"}
	case "modrinthDownloadDependencies":
		return []string{"none", "required", "optional"}
	case "modrinthProjectsDefaultVersionType":
		return []string{"release", "beta", "alpha"}
	case "modrinthModpackVersionType":
		return []string{"release", "beta", "alpha"}
	case "modrinthLoader":
		return minecraft.PackLoaderNames()
	default:
		return []string{}
	}
}
