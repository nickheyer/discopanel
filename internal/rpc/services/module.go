package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/alias"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/module"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that ModuleService implements the interface
var _ discopanelv1connect.ModuleServiceHandler = (*ModuleService)(nil)

// ModuleService implements the Module service
type ModuleService struct {
	store         *storage.Store
	docker        *docker.Client
	moduleManager *module.Manager
	proxyManager  *proxy.Manager
	config        *config.Config
	log           *logger.Logger
	logStreamer   *logger.LogStreamer
}

// NewModuleService creates a new module service
func NewModuleService(
	store *storage.Store,
	docker *docker.Client,
	moduleManager *module.Manager,
	proxyManager *proxy.Manager,
	cfg *config.Config,
	logStreamer *logger.LogStreamer,
	log *logger.Logger,
) *ModuleService {
	return &ModuleService{
		store:         store,
		docker:        docker,
		moduleManager: moduleManager,
		proxyManager:  proxyManager,
		config:        cfg,
		logStreamer:   logStreamer,
		log:           log,
	}
}

// Conversion functions

func dbModuleTemplateTypeToProto(t storage.ModuleTemplateType) v1.ModuleTemplateType {
	switch t {
	case storage.ModuleTemplateTypeBuiltin:
		return v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_BUILTIN
	case storage.ModuleTemplateTypeCustom:
		return v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_CUSTOM
	default:
		return v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_UNSPECIFIED
	}
}

// func protoModuleTemplateTypeToDB(t v1.ModuleTemplateType) storage.ModuleTemplateType {
// 	switch t {
// 	case v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_BUILTIN:
// 		return storage.ModuleTemplateTypeBuiltin
// 	case v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_CUSTOM:
// 		return storage.ModuleTemplateTypeCustom
// 	default:
// 		return storage.ModuleTemplateTypeCustom
// 	}
// }

// func dbModuleProtocolToProto(p storage.ModuleProtocol) v1.ModuleProtocol {
// 	switch p {
// 	case storage.ModuleProtocolTCP:
// 		return v1.ModuleProtocol_MODULE_PROTOCOL_TCP
// 	case storage.ModuleProtocolUDP:
// 		return v1.ModuleProtocol_MODULE_PROTOCOL_UDP
// 	default:
// 		return v1.ModuleProtocol_MODULE_PROTOCOL_UNSPECIFIED
// 	}
// }

// func protoModuleProtocolToDB(p v1.ModuleProtocol) storage.ModuleProtocol {
// 	switch p {
// 	case v1.ModuleProtocol_MODULE_PROTOCOL_TCP:
// 		return storage.ModuleProtocolTCP
// 	case v1.ModuleProtocol_MODULE_PROTOCOL_UDP:
// 		return storage.ModuleProtocolUDP
// 	default:
// 		return storage.ModuleProtocolTCP
// 	}
// }

func dbModuleStatusToProto(s storage.ModuleStatus) v1.ModuleStatus {
	switch s {
	case storage.ModuleStatusStopped:
		return v1.ModuleStatus_MODULE_STATUS_STOPPED
	case storage.ModuleStatusStarting:
		return v1.ModuleStatus_MODULE_STATUS_STARTING
	case storage.ModuleStatusRunning:
		return v1.ModuleStatus_MODULE_STATUS_RUNNING
	case storage.ModuleStatusStopping:
		return v1.ModuleStatus_MODULE_STATUS_STOPPING
	case storage.ModuleStatusError:
		return v1.ModuleStatus_MODULE_STATUS_ERROR
	case storage.ModuleStatusCreating:
		return v1.ModuleStatus_MODULE_STATUS_CREATING
	default:
		return v1.ModuleStatus_MODULE_STATUS_UNSPECIFIED
	}
}

func dbModuleTemplateToProto(t *storage.ModuleTemplate) *v1.ModuleTemplate {
	if t == nil {
		return nil
	}
	return &v1.ModuleTemplate{
		Id:                    t.ID,
		Name:                  t.Name,
		Description:           t.Description,
		Type:                  dbModuleTemplateTypeToProto(t.Type),
		DockerImage:           t.DockerImage,
		ConfigSchema:          t.ConfigSchema,
		DefaultEnv:            t.DefaultEnv,
		DefaultVolumes:        t.DefaultVolumes,
		HealthCheckPath:       t.HealthCheckPath,
		HealthCheckPort:       int32(t.HealthCheckPort),
		RequiresServer:        t.RequiresServer,
		SupportsProxy:         t.SupportsProxy,
		Icon:                  t.Icon,
		Category:              t.Category,
		Documentation:         t.Documentation,
		CreatedAt:             timestamppb.New(t.CreatedAt),
		UpdatedAt:             timestamppb.New(t.UpdatedAt),
		Ports:                 t.Ports,
		SuggestedDependencies: t.SuggestedDependencies,
		DefaultHooks:          t.DefaultHooks,
		Metadata:              t.Metadata,
		DefaultCmd:            t.DefaultCmd,
		DefaultAccessUrls:     t.DefaultAccessUrls,
	}
}

func dbModuleToProto(m *storage.Module, serverName, templateName, serverProxyHostname string) *v1.Module {
	if m == nil {
		return nil
	}

	protoModule := &v1.Module{
		Id:                    m.ID,
		Name:                  m.Name,
		ServerId:              m.ServerID,
		TemplateId:            m.TemplateID,
		ContainerId:           m.ContainerID,
		Status:                dbModuleStatusToProto(m.Status),
		Config:                m.Config,
		EnvOverrides:          m.EnvOverrides,
		VolumeOverrides:       m.VolumeOverrides,
		Memory:                int32(m.Memory),
		CpuLimit:              m.CPULimit,
		AutoStart:             m.AutoStart,
		FollowServerLifecycle: m.FollowServerLifecycle,
		Detached:              m.Detached,
		DataPath:              m.DataPath,
		CreatedAt:             timestamppb.New(m.CreatedAt),
		UpdatedAt:             timestamppb.New(m.UpdatedAt),
		MemoryUsage:           m.MemoryUsage,
		CpuPercent:            m.CPUPercent,
		ServerName:            serverName,
		TemplateName:          templateName,
		ServerProxyHostname:   serverProxyHostname,
		Ports:                 m.Ports,
		Dependencies:          m.Dependencies,
		HealthCheckInterval:   int32(m.HealthCheckInterval),
		HealthCheckTimeout:    int32(m.HealthCheckTimeout),
		HealthCheckRetries:    int32(m.HealthCheckRetries),
		EventHooks:            m.EventHooks,
		Metadata:              m.Metadata,
		CmdOverride:           m.CmdOverride,
		AccessUrls:            m.AccessUrls,
	}

	if m.LastStarted != nil {
		protoModule.LastStarted = timestamppb.New(*m.LastStarted)
	}

	return protoModule
}

// Template operations

func (s *ModuleService) ListModuleTemplates(ctx context.Context, req *connect.Request[v1.ListModuleTemplatesRequest]) (*connect.Response[v1.ListModuleTemplatesResponse], error) {
	templates, err := s.store.ListModuleTemplates(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list templates: %w", err))
	}

	msg := req.Msg
	var protoTemplates []*v1.ModuleTemplate
	for _, t := range templates {
		// Filter by type if specified
		if msg.Type != nil && *msg.Type != v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_UNSPECIFIED {
			if dbModuleTemplateTypeToProto(t.Type) != *msg.Type {
				continue
			}
		}
		// Filter by category if specified
		if msg.Category != nil && *msg.Category != "" && t.Category != *msg.Category {
			continue
		}
		protoTemplates = append(protoTemplates, dbModuleTemplateToProto(t))
	}

	return connect.NewResponse(&v1.ListModuleTemplatesResponse{
		Templates: protoTemplates,
	}), nil
}

func (s *ModuleService) GetModuleTemplate(ctx context.Context, req *connect.Request[v1.GetModuleTemplateRequest]) (*connect.Response[v1.GetModuleTemplateResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("template ID is required"))
	}

	template, err := s.store.GetModuleTemplate(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("template not found"))
	}

	return connect.NewResponse(&v1.GetModuleTemplateResponse{
		Template: dbModuleTemplateToProto(template),
	}), nil
}

func (s *ModuleService) CreateModuleTemplate(ctx context.Context, req *connect.Request[v1.CreateModuleTemplateRequest]) (*connect.Response[v1.CreateModuleTemplateResponse], error) {
	msg := req.Msg
	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}
	if msg.DockerImage == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("docker_image is required"))
	}

	// Check for duplicate name
	if _, err := s.store.GetModuleTemplateByName(ctx, msg.Name); err == nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("template with this name already exists"))
	}

	template := &storage.ModuleTemplate{
		ID:                    uuid.New().String(),
		Name:                  msg.Name,
		Description:           msg.Description,
		Type:                  storage.ModuleTemplateTypeCustom, // User-created templates are always custom
		DockerImage:           msg.DockerImage,
		ConfigSchema:          msg.ConfigSchema,
		DefaultEnv:            msg.DefaultEnv,
		DefaultVolumes:        msg.DefaultVolumes,
		HealthCheckPath:       msg.HealthCheckPath,
		HealthCheckPort:       int(msg.HealthCheckPort),
		RequiresServer:        msg.RequiresServer,
		SupportsProxy:         msg.SupportsProxy,
		Icon:                  msg.Icon,
		Category:              msg.Category,
		Documentation:         msg.Documentation,
		Ports:                 msg.Ports,
		SuggestedDependencies: msg.SuggestedDependencies,
		DefaultHooks:          msg.DefaultHooks,
		Metadata:              msg.Metadata,
		DefaultCmd:            msg.DefaultCmd,
		DefaultAccessUrls:     msg.DefaultAccessUrls,
	}

	if err := s.store.CreateModuleTemplate(ctx, template); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create template: %w", err))
	}

	return connect.NewResponse(&v1.CreateModuleTemplateResponse{
		Template: dbModuleTemplateToProto(template),
	}), nil
}

func (s *ModuleService) UpdateModuleTemplate(ctx context.Context, req *connect.Request[v1.UpdateModuleTemplateRequest]) (*connect.Response[v1.UpdateModuleTemplateResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("template ID is required"))
	}

	template, err := s.store.GetModuleTemplate(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("template not found"))
	}

	// Don't allow modifying built-in templates' core fields
	if template.Type == storage.ModuleTemplateTypeBuiltin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot modify built-in template"))
	}

	// Update fields if provided
	if msg.Name != nil {
		template.Name = *msg.Name
	}
	if msg.Description != nil {
		template.Description = *msg.Description
	}
	if msg.DockerImage != nil {
		template.DockerImage = *msg.DockerImage
	}
	if msg.ConfigSchema != nil {
		template.ConfigSchema = *msg.ConfigSchema
	}
	if msg.DefaultEnv != nil {
		template.DefaultEnv = *msg.DefaultEnv
	}
	if msg.DefaultVolumes != nil {
		template.DefaultVolumes = *msg.DefaultVolumes
	}
	if msg.HealthCheckPath != nil {
		template.HealthCheckPath = *msg.HealthCheckPath
	}
	if msg.HealthCheckPort != nil {
		template.HealthCheckPort = int(*msg.HealthCheckPort)
	}
	if msg.RequiresServer != nil {
		template.RequiresServer = *msg.RequiresServer
	}
	if msg.SupportsProxy != nil {
		template.SupportsProxy = *msg.SupportsProxy
	}
	if msg.Icon != nil {
		template.Icon = *msg.Icon
	}
	if msg.Category != nil {
		template.Category = *msg.Category
	}
	if msg.Documentation != nil {
		template.Documentation = *msg.Documentation
	}
	if len(msg.Ports) > 0 {
		template.Ports = msg.Ports
	}
	if len(msg.SuggestedDependencies) > 0 {
		template.SuggestedDependencies = msg.SuggestedDependencies
	}
	if len(msg.DefaultHooks) > 0 {
		template.DefaultHooks = msg.DefaultHooks
	}
	if len(msg.Metadata) > 0 {
		template.Metadata = msg.Metadata
	}
	if msg.DefaultCmd != nil {
		template.DefaultCmd = *msg.DefaultCmd
	}
	if len(msg.DefaultAccessUrls) > 0 {
		template.DefaultAccessUrls = msg.DefaultAccessUrls
	}

	if err := s.store.UpdateModuleTemplate(ctx, template); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update template: %w", err))
	}

	return connect.NewResponse(&v1.UpdateModuleTemplateResponse{
		Template: dbModuleTemplateToProto(template),
	}), nil
}

func (s *ModuleService) DeleteModuleTemplate(ctx context.Context, req *connect.Request[v1.DeleteModuleTemplateRequest]) (*connect.Response[v1.DeleteModuleTemplateResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("template ID is required"))
	}

	template, err := s.store.GetModuleTemplate(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("template not found"))
	}

	// Don't allow deleting built-in templates
	if template.Type == storage.ModuleTemplateTypeBuiltin {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cannot delete built-in template"))
	}

	if err := s.store.DeleteModuleTemplate(ctx, msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete template: %w", err))
	}

	return connect.NewResponse(&v1.DeleteModuleTemplateResponse{}), nil
}

// Module operations

func (s *ModuleService) ListModules(ctx context.Context, req *connect.Request[v1.ListModulesRequest]) (*connect.Response[v1.ListModulesResponse], error) {
	msg := req.Msg

	var modules []*storage.Module
	var err error

	if msg.ServerId != nil && *msg.ServerId != "" {
		modules, err = s.store.ListServerModules(ctx, *msg.ServerId)
	} else if msg.TemplateId != nil && *msg.TemplateId != "" {
		modules, err = s.store.ListModulesByTemplate(ctx, *msg.TemplateId)
	} else {
		modules, err = s.store.ListModules(ctx)
	}

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list modules: %w", err))
	}

	// Enrich with server and template names, and update status from Docker
	var protoModules []*v1.Module
	for _, m := range modules {
		// Get actual status from Docker and update if different
		if m.ContainerID != "" {
			actualStatus, err := s.moduleManager.GetModuleStatus(ctx, m.ID)
			if err == nil && actualStatus != m.Status {
				m.Status = actualStatus
				s.store.UpdateModule(ctx, m)
			}
		} else if m.Status == storage.ModuleStatusCreating {
			// If no container but status is Creating, check if creation stalled
			// This can happen if container creation failed silently
			// Give it some grace period (30 seconds) before marking as error
			if time.Since(m.UpdatedAt) > 30*time.Second {
				m.Status = storage.ModuleStatusError
				s.store.UpdateModule(ctx, m)
			}
		}

		serverName := ""
		serverProxyHostname := ""
		if server, err := s.store.GetServer(ctx, m.ServerID); err == nil {
			serverName = server.Name
			serverProxyHostname = server.ProxyHostname
		}

		templateName := ""
		if template, err := s.store.GetModuleTemplate(ctx, m.TemplateID); err == nil {
			templateName = template.Name
		}

		protoModules = append(protoModules, dbModuleToProto(m, serverName, templateName, serverProxyHostname))
	}

	return connect.NewResponse(&v1.ListModulesResponse{
		Modules: protoModules,
	}), nil
}

func (s *ModuleService) GetModule(ctx context.Context, req *connect.Request[v1.GetModuleRequest]) (*connect.Response[v1.GetModuleResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module ID is required"))
	}

	module, err := s.store.GetModule(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("module not found"))
	}

	// Get actual status from Docker and update if different
	if module.ContainerID != "" {
		actualStatus, err := s.moduleManager.GetModuleStatus(ctx, msg.Id)
		if err == nil && actualStatus != module.Status {
			module.Status = actualStatus
			s.store.UpdateModule(ctx, module)
		}
	}

	// Enrich with server and template names
	serverName := ""
	serverProxyHostname := ""
	if server, err := s.store.GetServer(ctx, module.ServerID); err == nil {
		serverName = server.Name
		serverProxyHostname = server.ProxyHostname
	}

	templateName := ""
	if template, err := s.store.GetModuleTemplate(ctx, module.TemplateID); err == nil {
		templateName = template.Name
	}

	return connect.NewResponse(&v1.GetModuleResponse{
		Module: dbModuleToProto(module, serverName, templateName, serverProxyHostname),
	}), nil
}

func (s *ModuleService) CreateModule(ctx context.Context, req *connect.Request[v1.CreateModuleRequest]) (*connect.Response[v1.CreateModuleResponse], error) {
	msg := req.Msg
	if msg.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}
	if msg.ServerId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("server_id is required"))
	}
	if msg.TemplateId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("template_id is required"))
	}

	// Verify server exists
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Verify template exists
	template, err := s.store.GetModuleTemplate(ctx, msg.TemplateId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("template not found"))
	}

	// Use ports from request, or fall back to template defaults
	ports := msg.Ports
	if len(ports) == 0 {
		ports = template.Ports
	}

	// Allocate host ports for any port entries that need it
	// Track ports allocated in this request to avoid duplicates
	allocatedInRequest := make(map[int]bool)
	for _, port := range ports {
		if port == nil || port.ContainerPort == 0 {
			continue
		}
		if port.HostPort == 0 {
			allocatedPort, err := s.moduleManager.AllocateModulePortExcluding(ctx, allocatedInRequest)
			if err != nil {
				return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("failed to allocate port: %w", err))
			}
			port.HostPort = int32(allocatedPort)
			allocatedInRequest[allocatedPort] = true
		}
	}

	// Verify all ports are available (considering proxy/protocol rules)
	for _, port := range ports {
		if port == nil || port.HostPort == 0 {
			continue
		}
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		conflict, err := s.store.CheckPortAvailability(ctx, int(port.HostPort), protocol, port.ProxyEnabled, server.ProxyHostname, "")
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check port availability: %w", err))
		}
		if conflict != nil {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("port %d/%s: %s (used by %s)", port.HostPort, protocol, conflict.Reason, conflict.Module.Name))
		}
	}

	module := &storage.Module{
		ID:                    uuid.New().String(),
		Name:                  msg.Name,
		ServerID:              msg.ServerId,
		TemplateID:            msg.TemplateId,
		Status:                storage.ModuleStatusStopped,
		Config:                msg.Config,
		EnvOverrides:          msg.EnvOverrides,
		VolumeOverrides:       msg.VolumeOverrides,
		Memory:                int(msg.Memory),
		CPULimit:              msg.CpuLimit,
		AutoStart:             msg.AutoStart,
		FollowServerLifecycle: msg.FollowServerLifecycle,
		Detached:              msg.Detached,
		Ports:                 ports,
		Dependencies:          msg.Dependencies,
		HealthCheckInterval:   int(msg.HealthCheckInterval),
		HealthCheckTimeout:    int(msg.HealthCheckTimeout),
		HealthCheckRetries:    int(msg.HealthCheckRetries),
		EventHooks:            msg.EventHooks,
		Metadata:              msg.Metadata,
		CmdOverride:           msg.CmdOverride,
		AccessUrls:            msg.AccessUrls,
	}

	// Use template defaults for access URLs if not provided
	if len(module.AccessUrls) == 0 {
		module.AccessUrls = template.DefaultAccessUrls
	}

	if module.Memory == 0 {
		module.Memory = 512 // Default 512MB
	}

	if err := s.store.CreateModule(ctx, module); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create module: %w", err))
	}

	// Create container in background
	go func() {
		bgCtx := context.Background()
		if err := s.moduleManager.CreateAndStartModule(bgCtx, module.ID, msg.StartImmediately); err != nil {
			s.log.Error("Failed to create module container: %v", err)
		}
	}()

	return connect.NewResponse(&v1.CreateModuleResponse{
		Module: dbModuleToProto(module, server.Name, template.Name, server.ProxyHostname),
	}), nil
}

func (s *ModuleService) UpdateModule(ctx context.Context, req *connect.Request[v1.UpdateModuleRequest]) (*connect.Response[v1.UpdateModuleResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module ID is required"))
	}

	module, err := s.store.GetModule(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("module not found"))
	}

	needsRecreate := false

	// Update fields if provided
	if msg.Name != nil {
		module.Name = *msg.Name
	}
	if msg.Config != nil {
		module.Config = *msg.Config
	}
	if msg.EnvOverrides != nil {
		if *msg.EnvOverrides != module.EnvOverrides {
			module.EnvOverrides = *msg.EnvOverrides
			needsRecreate = true
		}
	}
	if msg.VolumeOverrides != nil {
		if *msg.VolumeOverrides != module.VolumeOverrides {
			module.VolumeOverrides = *msg.VolumeOverrides
			needsRecreate = true
		}
	}
	if msg.Memory != nil {
		if int(*msg.Memory) != module.Memory {
			module.Memory = int(*msg.Memory)
			needsRecreate = true
		}
	}
	if msg.CpuLimit != nil {
		module.CPULimit = *msg.CpuLimit
	}
	if msg.AutoStart != nil {
		module.AutoStart = *msg.AutoStart
	}
	if msg.FollowServerLifecycle != nil {
		module.FollowServerLifecycle = *msg.FollowServerLifecycle
	}
	if msg.Detached != nil {
		module.Detached = *msg.Detached
	}
	if len(msg.Ports) > 0 {
		// Get server for hostname context
		server, err := s.store.GetServer(ctx, module.ServerID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server: %w", err))
		}

		// Validate new ports are available (excluding current module)
		for _, port := range msg.Ports {
			if port == nil || port.HostPort == 0 {
				continue
			}
			protocol := port.Protocol
			if protocol == "" {
				protocol = "tcp"
			}
			conflict, err := s.store.CheckPortAvailability(ctx, int(port.HostPort), protocol, port.ProxyEnabled, server.ProxyHostname, module.ID)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check port availability: %w", err))
			}
			if conflict != nil {
				return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("port %d/%s: %s (used by %s)", port.HostPort, protocol, conflict.Reason, conflict.Module.Name))
			}
		}

		module.Ports = msg.Ports
		needsRecreate = true
	}
	if len(msg.Dependencies) > 0 {
		module.Dependencies = msg.Dependencies
	}
	if msg.HealthCheckInterval != nil {
		module.HealthCheckInterval = int(*msg.HealthCheckInterval)
	}
	if msg.HealthCheckTimeout != nil {
		module.HealthCheckTimeout = int(*msg.HealthCheckTimeout)
	}
	if msg.HealthCheckRetries != nil {
		module.HealthCheckRetries = int(*msg.HealthCheckRetries)
	}
	if len(msg.EventHooks) > 0 {
		module.EventHooks = msg.EventHooks
	}
	if len(msg.Metadata) > 0 {
		module.Metadata = msg.Metadata
	}
	if msg.CmdOverride != nil {
		if *msg.CmdOverride != module.CmdOverride {
			module.CmdOverride = *msg.CmdOverride
			needsRecreate = true
		}
	}
	if len(msg.AccessUrls) > 0 {
		module.AccessUrls = msg.AccessUrls
	}

	if err := s.store.UpdateModule(ctx, module); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update module: %w", err))
	}

	// Recreate container if needed
	if needsRecreate && module.ContainerID != "" {
		go func() {
			bgCtx := context.Background()
			if err := s.moduleManager.RecreateModule(bgCtx, module.ID); err != nil {
				s.log.Error("Failed to recreate module container: %v", err)
			}
		}()
	}

	// Get enrichment data
	serverName := ""
	serverProxyHostname := ""
	if server, err := s.store.GetServer(ctx, module.ServerID); err == nil {
		serverName = server.Name
		serverProxyHostname = server.ProxyHostname
	}
	templateName := ""
	if template, err := s.store.GetModuleTemplate(ctx, module.TemplateID); err == nil {
		templateName = template.Name
	}

	return connect.NewResponse(&v1.UpdateModuleResponse{
		Module: dbModuleToProto(module, serverName, templateName, serverProxyHostname),
	}), nil
}

func (s *ModuleService) DeleteModule(ctx context.Context, req *connect.Request[v1.DeleteModuleRequest]) (*connect.Response[v1.DeleteModuleResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module ID is required"))
	}

	if err := s.moduleManager.DeleteModule(ctx, msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete module: %w", err))
	}

	return connect.NewResponse(&v1.DeleteModuleResponse{}), nil
}

// Lifecycle operations

func (s *ModuleService) StartModule(ctx context.Context, req *connect.Request[v1.StartModuleRequest]) (*connect.Response[v1.StartModuleResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module ID is required"))
	}

	module, err := s.store.GetModule(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("module not found"))
	}

	// If no container exists, create one first
	if module.ContainerID == "" {
		if err := s.moduleManager.CreateAndStartModule(ctx, msg.Id, true); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create and start module: %w", err))
		}
	} else {
		if err := s.moduleManager.StartModule(ctx, msg.Id); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start module: %w", err))
		}
	}

	return connect.NewResponse(&v1.StartModuleResponse{
		Status: "started",
	}), nil
}

func (s *ModuleService) StopModule(ctx context.Context, req *connect.Request[v1.StopModuleRequest]) (*connect.Response[v1.StopModuleResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module ID is required"))
	}

	if err := s.moduleManager.StopModule(ctx, msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop module: %w", err))
	}

	return connect.NewResponse(&v1.StopModuleResponse{
		Status: "stopped",
	}), nil
}

func (s *ModuleService) RestartModule(ctx context.Context, req *connect.Request[v1.RestartModuleRequest]) (*connect.Response[v1.RestartModuleResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module ID is required"))
	}

	if err := s.moduleManager.RestartModule(ctx, msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restart module: %w", err))
	}

	return connect.NewResponse(&v1.RestartModuleResponse{
		Status: "restarted",
	}), nil
}

func (s *ModuleService) RecreateModule(ctx context.Context, req *connect.Request[v1.RecreateModuleRequest]) (*connect.Response[v1.RecreateModuleResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module ID is required"))
	}

	if err := s.moduleManager.RecreateModule(ctx, msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to recreate module: %w", err))
	}

	return connect.NewResponse(&v1.RecreateModuleResponse{
		Status: "recreated",
	}), nil
}

// Logs and status

func (s *ModuleService) GetModuleLogs(ctx context.Context, req *connect.Request[v1.GetModuleLogsRequest]) (*connect.Response[v1.GetModuleLogsResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module ID is required"))
	}

	module, err := s.store.GetModule(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("module not found"))
	}

	if module.ContainerID == "" {
		return connect.NewResponse(&v1.GetModuleLogsResponse{
			Logs:  []*v1.LogEntry{},
			Total: 0,
		}), nil
	}

	tail := int(msg.Tail)
	if tail == 0 {
		tail = 100
	}

	// Get structured log entries from the log streamer if available
	var protoLogs []*v1.LogEntry
	if s.logStreamer != nil {
		protoLogs = s.logStreamer.GetLogs(module.ContainerID, tail)
	}

	return connect.NewResponse(&v1.GetModuleLogsResponse{
		Logs:  protoLogs,
		Total: int32(len(protoLogs)),
	}), nil
}

func (s *ModuleService) GetNextAvailableModulePort(ctx context.Context, req *connect.Request[v1.GetNextAvailableModulePortRequest]) (*connect.Response[v1.GetNextAvailableModulePortResponse], error) {
	port, err := s.moduleManager.AllocateModulePort(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeResourceExhausted, err)
	}

	usedPorts, err := s.moduleManager.GetUsedModulePorts(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var protoUsedPorts []*v1.UsedPort
	for _, p := range usedPorts {
		protoUsedPorts = append(protoUsedPorts, &v1.UsedPort{
			Port:  int32(p),
			InUse: true,
		})
	}

	return connect.NewResponse(&v1.GetNextAvailableModulePortResponse{
		Port:      int32(port),
		UsedPorts: protoUsedPorts,
	}), nil
}

// GetAvailableAliases returns all available aliases for module/template configuration
func (s *ModuleService) GetAvailableAliases(ctx context.Context, req *connect.Request[v1.GetAvailableAliasesRequest]) (*connect.Response[v1.GetAvailableAliasesResponse], error) {
	msg := req.Msg

	// Build alias context from request
	aliasCtx := alias.NewContext()
	aliasCtx.Config = s.config

	// Get server context if provided
	if msg.ServerId != nil && *msg.ServerId != "" {
		if server, err := s.store.GetServer(ctx, *msg.ServerId); err == nil {
			aliasCtx.Server = server
			// Also get server config for server.config.* aliases
			if serverConfig, err := s.store.GetServerConfig(ctx, *msg.ServerId); err == nil {
				aliasCtx.ServerConfig = serverConfig
			}
		}
	}

	// Get module context if provided
	if msg.ModuleId != nil && *msg.ModuleId != "" {
		if mod, err := s.store.GetModule(ctx, *msg.ModuleId); err == nil {
			aliasCtx.Module = mod
		}
	}

	// Get all available aliases dynamically using reflection
	availableAliases := alias.GetAvailableAliases(aliasCtx)

	// Convert to proto messages
	var protoAliases []*v1.AliasInfo
	for _, a := range availableAliases {
		protoAliases = append(protoAliases, &v1.AliasInfo{
			Alias:        a.Alias,
			Description:  a.Description,
			Category:     aliasCategoryToProto(a.Category),
			ExampleValue: a.ExampleValue,
		})
	}

	return connect.NewResponse(&v1.GetAvailableAliasesResponse{
		Aliases: protoAliases,
	}), nil
}

// Get all aliases with resolved values for ctx
func (s *ModuleService) GetResolvedAliases(ctx context.Context, req *connect.Request[v1.GetResolvedAliasesRequest]) (*connect.Response[v1.GetResolvedAliasesResponse], error) {
	msg := req.Msg
	aliasCtx := alias.NewContext()
	aliasCtx.Config = s.config

	if msg.ServerId != nil && *msg.ServerId != "" {
		if server, err := s.store.GetServer(ctx, *msg.ServerId); err == nil {
			aliasCtx.Server = server
			if serverConfig, err := s.store.GetServerConfig(ctx, *msg.ServerId); err == nil {
				aliasCtx.ServerConfig = serverConfig
			}
		}
	}

	if msg.ModuleId != nil && *msg.ModuleId != "" {
		if mod, err := s.store.GetModule(ctx, *msg.ModuleId); err == nil {
			aliasCtx.Module = mod
			if siblings, err := s.store.ListServerModules(ctx, mod.ServerID); err == nil {
				aliasCtx.Modules = make(map[string]*storage.Module)
				for _, sib := range siblings {
					aliasCtx.Modules[sib.Name] = sib
				}
			}
		}
	}

	resolved := alias.GetResolvedAliases(aliasCtx)
	return connect.NewResponse(&v1.GetResolvedAliasesResponse{Aliases: resolved}), nil
}

// aliasCategoryToProto converts internal alias category to proto enum
func aliasCategoryToProto(c alias.Category) v1.AliasCategory {
	switch c {
	case alias.CategoryServer:
		return v1.AliasCategory_ALIAS_CATEGORY_SERVER
	case alias.CategoryModule:
		return v1.AliasCategory_ALIAS_CATEGORY_MODULE
	case alias.CategorySpecial:
		return v1.AliasCategory_ALIAS_CATEGORY_SPECIAL
	default:
		return v1.AliasCategory_ALIAS_CATEGORY_UNSPECIFIED
	}
}
