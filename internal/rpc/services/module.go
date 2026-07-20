package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/alias"
	"github.com/nickheyer/discopanel/internal/auth"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/metrics"
	"github.com/nickheyer/discopanel/internal/module"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/config"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
)

// Compile-time check that ModuleService implements the interface
var _ discopanelv1connect.ModuleServiceHandler = (*ModuleService)(nil)

// ModuleService implements the Module service
type ModuleService struct {
	store         *storage.Store
	docker        *docker.Client
	moduleManager *module.Manager
	proxyManager  *proxy.Manager
	authManager   *auth.Manager
	config        *config.Config
	rec           *metrics.Recorder
	log           *logger.Logger
	logStreamer   *logger.LogStreamer
}

func NewModuleService(
	store *storage.Store,
	docker *docker.Client,
	moduleManager *module.Manager,
	proxyManager *proxy.Manager,
	authManager *auth.Manager,
	cfg *config.Config,
	logStreamer *logger.LogStreamer,
	rec *metrics.Recorder,
	log *logger.Logger,
) *ModuleService {
	return &ModuleService{
		store:         store,
		docker:        docker,
		moduleManager: moduleManager,
		proxyManager:  proxyManager,
		authManager:   authManager,
		config:        cfg,
		logStreamer:   logStreamer,
		rec:           rec,
		log:           log,
	}
}

func (s *ModuleService) applyModuleStats(ctx context.Context, m *v1.Module) {
	if m.ContainerId == "" || m.Status != v1.ModuleStatus_MODULE_STATUS_RUNNING {
		return
	}
	stats, err := s.docker.GetContainerStats(ctx, m.ContainerId)
	if err != nil {
		return
	}
	m.CpuPercent = stats.CpuPercent
	m.MemoryUsage = stats.MemoryUsage
}

// Looks up the username behind a module's creator id
func (s *ModuleService) resolveCreatedByUsername(ctx context.Context, userID string) string {
	if userID == "" {
		return ""
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return ""
	}
	return user.Username
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
			if t.Type != *msg.Type {
				continue
			}
		}
		// Filter by category if specified
		if msg.Category != nil && *msg.Category != "" && t.Category != *msg.Category {
			continue
		}
		protoTemplates = append(protoTemplates, t)
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
		Template: template,
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

	template := &v1.ModuleTemplate{
		Id:                      uuid.New().String(),
		Name:                    msg.Name,
		Description:             msg.Description,
		Type:                    v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_CUSTOM, // User-created templates are always custom
		DockerImage:             msg.DockerImage,
		DefaultEnv:              msg.DefaultEnv,
		DefaultVolumes:          msg.DefaultVolumes,
		HealthCheckPath:         msg.HealthCheckPath,
		HealthCheckPort:         msg.HealthCheckPort,
		RequiresServer:          msg.RequiresServer,
		SupportsProxy:           msg.SupportsProxy,
		Icon:                    msg.Icon,
		Category:                msg.Category,
		Documentation:           msg.Documentation,
		Ports:                   msg.Ports,
		SuggestedDependencies:   msg.SuggestedDependencies,
		DefaultHooks:            msg.DefaultHooks,
		Metadata:                msg.Metadata,
		DefaultCmd:              msg.DefaultCmd,
		DefaultAccessUrls:       msg.DefaultAccessUrls,
		DefaultUid:              msg.DefaultUid,
		DefaultGid:              msg.DefaultGid,
		DefaultInitCommand:      msg.DefaultInitCommand,
		DefaultInitCommandDelay: msg.DefaultInitCommandDelay,
		DefaultRestartAfterInit: msg.DefaultRestartAfterInit,
	}

	if err := s.store.CreateModuleTemplate(ctx, template); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create template: %w", err))
	}

	return connect.NewResponse(&v1.CreateModuleTemplateResponse{
		Template: template,
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
	if template.Type == v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_BUILTIN {
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
		template.HealthCheckPort = *msg.HealthCheckPort
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
	if msg.DefaultUid != nil {
		template.DefaultUid = *msg.DefaultUid
	}
	if msg.DefaultGid != nil {
		template.DefaultGid = *msg.DefaultGid
	}
	if msg.DefaultInitCommand != nil {
		template.DefaultInitCommand = *msg.DefaultInitCommand
	}
	if msg.DefaultInitCommandDelay != nil {
		template.DefaultInitCommandDelay = *msg.DefaultInitCommandDelay
	}
	if msg.DefaultRestartAfterInit != nil {
		template.DefaultRestartAfterInit = *msg.DefaultRestartAfterInit
	}

	if err := s.store.UpdateModuleTemplate(ctx, template); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update template: %w", err))
	}

	return connect.NewResponse(&v1.UpdateModuleTemplateResponse{
		Template: template,
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
	if template.Type == v1.ModuleTemplateType_MODULE_TEMPLATE_TYPE_BUILTIN {
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

	var modules []*v1.Module
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

	servers, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list servers: %w", err))
	}
	serversByID := make(map[string]*v1.Server, len(servers))
	for _, srv := range servers {
		serversByID[srv.Id] = srv
	}
	templates, err := s.store.ListModuleTemplates(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list templates: %w", err))
	}
	templateNames := make(map[string]string, len(templates))
	for _, t := range templates {
		templateNames[t.Id] = t.Name
	}
	usernames := map[string]string{}

	fullStats := msg.FullStats != nil && *msg.FullStats
	var protoModules []*v1.Module
	for _, m := range modules {
		// Live docker state serves the response, never the row
		if fullStats && m.ContainerId != "" {
			if actualStatus, err := s.moduleManager.GetModuleStatus(ctx, m.Id); err == nil {
				m.Status = actualStatus
			}
		}
		if m.ContainerId == "" && m.Status == v1.ModuleStatus_MODULE_STATUS_CREATING && time.Since(m.UpdatedAt.AsTime()) > 30*time.Second {
			m.Status = v1.ModuleStatus_MODULE_STATUS_ERROR
		}
		if fullStats {
			s.applyModuleStats(ctx, m)
		}

		serverName := ""
		serverProxyHostname := ""
		if srv := serversByID[m.ServerId]; srv != nil {
			serverName = srv.Name
			serverProxyHostname = srv.ProxyHostname
		}
		if _, ok := usernames[m.CreatedByUserId]; !ok {
			usernames[m.CreatedByUserId] = s.resolveCreatedByUsername(ctx, m.CreatedByUserId)
		}
		m.Server = nil
		m.Template = nil
		m.ServerName = serverName
		m.TemplateName = templateNames[m.TemplateId]
		m.ServerProxyHostname = serverProxyHostname
		m.CreatedByUsername = usernames[m.CreatedByUserId]
		protoModules = append(protoModules, m.Redact())
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
	if module.ContainerId != "" {
		actualStatus, err := s.moduleManager.GetModuleStatus(ctx, msg.Id)
		if err == nil && actualStatus != module.Status {
			module.Status = actualStatus
			s.store.UpdateModule(ctx, module)
		}
	}

	s.applyModuleStats(ctx, module)

	module.Server = nil
	module.Template = nil
	if server, err := s.store.GetServer(ctx, module.ServerId); err == nil {
		module.ServerName = server.Name
		module.ServerProxyHostname = server.ProxyHostname
	}
	if template, err := s.store.GetModuleTemplate(ctx, module.TemplateId); err == nil {
		module.TemplateName = template.Name
	}
	module.CreatedByUsername = s.resolveCreatedByUsername(ctx, module.CreatedByUserId)
	return connect.NewResponse(&v1.GetModuleResponse{
		Module: module.Redact(),
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

	moduleID := uuid.New().String()

	module := &v1.Module{
		Id:                    moduleID,
		Name:                  msg.Name,
		ServerId:              msg.ServerId,
		TemplateId:            msg.TemplateId,
		Status:                v1.ModuleStatus_MODULE_STATUS_STOPPED,
		Config:                msg.Config,
		EnvOverrides:          msg.EnvOverrides,
		VolumeOverrides:       msg.VolumeOverrides,
		Memory:                msg.Memory,
		CpuLimit:              msg.CpuLimit,
		AutoStart:             msg.AutoStart,
		FollowServerLifecycle: msg.FollowServerLifecycle,
		Detached:              msg.Detached,
		Ports:                 ports,
		Dependencies:          msg.Dependencies,
		HealthCheckInterval:   msg.HealthCheckInterval,
		HealthCheckTimeout:    msg.HealthCheckTimeout,
		HealthCheckRetries:    msg.HealthCheckRetries,
		EventHooks:            msg.EventHooks,
		Metadata:              msg.Metadata,
		CmdOverride:           msg.CmdOverride,
		AccessUrls:            msg.AccessUrls,
		Uid:                   msg.Uid,
		Gid:                   msg.Gid,
		InitCommand:           msg.InitCommand,
		InitCommandDelay:      msg.InitCommandDelay,
		RestartAfterInit:      msg.RestartAfterInit,
	}

	// Manager mints a scoped token at container create
	if user := auth.GetUserFromContext(ctx); user != nil {
		module.CreatedByUserId = user.Id
	}

	// Use template defaults for access URLs if not provided
	if len(module.AccessUrls) == 0 {
		module.AccessUrls = template.DefaultAccessUrls
	}

	if module.Memory == 0 {
		if template.DefaultMemory > 0 {
			module.Memory = template.DefaultMemory
		} else {
			module.Memory = 512 // Default 512MB
		}
	}

	if module.Uid == "" && template.DefaultUid != "" {
		module.Uid = template.DefaultUid
	}
	if module.Gid == "" && template.DefaultGid != "" {
		module.Gid = template.DefaultGid
	}
	if module.InitCommand == "" && template.DefaultInitCommand != "" {
		module.InitCommand = template.DefaultInitCommand
	}
	if module.InitCommandDelay == 0 && template.DefaultInitCommandDelay > 0 {
		module.InitCommandDelay = template.DefaultInitCommandDelay
	}
	if !module.RestartAfterInit && template.DefaultRestartAfterInit {
		module.RestartAfterInit = template.DefaultRestartAfterInit
	}

	if err := s.store.CreateModule(ctx, module); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create module: %w", err))
	}
	s.rec.Record(ctx, module.ServerId, "module.create", metrics.Attrs{"module": module.Name, "template": template.Name}, "created module %s", module.Name)

	// Create container in background
	bgCtx := detach(ctx)
	go func() {
		if err := s.moduleManager.CreateAndStartModule(bgCtx, module.Id, msg.StartImmediately); err != nil {
			s.log.Error("Failed to create module container: %v", err)
		}
	}()

	module.Server = nil
	module.Template = nil
	module.ServerName = server.Name
	module.TemplateName = template.Name
	module.ServerProxyHostname = server.ProxyHostname
	module.CreatedByUsername = s.resolveCreatedByUsername(ctx, module.CreatedByUserId)
	return connect.NewResponse(&v1.CreateModuleResponse{
		Module: module.Redact(),
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

	// Update fields if provided
	if msg.Name != nil {
		module.Name = *msg.Name
	}
	if msg.Config != nil {
		module.Config = *msg.Config
	}
	if msg.EnvOverrides != nil {
		module.EnvOverrides = *msg.EnvOverrides
	}
	if msg.VolumeOverrides != nil {
		module.VolumeOverrides = *msg.VolumeOverrides
	}
	if msg.Memory != nil {
		module.Memory = *msg.Memory
	}
	if msg.CpuLimit != nil {
		module.CpuLimit = *msg.CpuLimit
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
		server, err := s.store.GetServer(ctx, module.ServerId)
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
			conflict, err := s.store.CheckPortAvailability(ctx, int(port.HostPort), protocol, port.ProxyEnabled, server.ProxyHostname, module.Id)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check port availability: %w", err))
			}
			if conflict != nil {
				return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("port %d/%s: %s (used by %s)", port.HostPort, protocol, conflict.Reason, conflict.Module.Name))
			}
		}

		module.Ports = msg.Ports
	}
	if len(msg.Dependencies) > 0 {
		module.Dependencies = msg.Dependencies
	}
	if msg.HealthCheckInterval != nil {
		module.HealthCheckInterval = *msg.HealthCheckInterval
	}
	if msg.HealthCheckTimeout != nil {
		module.HealthCheckTimeout = *msg.HealthCheckTimeout
	}
	if msg.HealthCheckRetries != nil {
		module.HealthCheckRetries = *msg.HealthCheckRetries
	}
	if len(msg.EventHooks) > 0 {
		module.EventHooks = msg.EventHooks
	}
	if len(msg.Metadata) > 0 {
		module.Metadata = msg.Metadata
	}
	if msg.CmdOverride != nil {
		module.CmdOverride = *msg.CmdOverride
	}
	if len(msg.AccessUrls) > 0 {
		module.AccessUrls = msg.AccessUrls
	}
	if msg.Uid != nil {
		module.Uid = *msg.Uid
	}
	if msg.Gid != nil {
		module.Gid = *msg.Gid
	}
	if msg.InitCommand != nil {
		module.InitCommand = *msg.InitCommand
	}
	if msg.InitCommandDelay != nil {
		module.InitCommandDelay = *msg.InitCommandDelay
	}
	if msg.RestartAfterInit != nil {
		module.RestartAfterInit = *msg.RestartAfterInit
	}

	if err := s.store.UpdateModule(ctx, module); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update module: %w", err))
	}

	// Config hash decides whether the container must rebuild
	if needsRecreate, err := s.moduleManager.NeedsRecreate(ctx, module.Id); err == nil && needsRecreate {
		go func() {
			bgCtx := context.Background()
			if err := s.moduleManager.RecreateModule(bgCtx, module.Id); err != nil {
				s.log.Error("Failed to recreate module container: %v", err)
			}
		}()
	}

	module.Server = nil
	module.Template = nil
	if server, err := s.store.GetServer(ctx, module.ServerId); err == nil {
		module.ServerName = server.Name
		module.ServerProxyHostname = server.ProxyHostname
	}
	if template, err := s.store.GetModuleTemplate(ctx, module.TemplateId); err == nil {
		module.TemplateName = template.Name
	}
	module.CreatedByUsername = s.resolveCreatedByUsername(ctx, module.CreatedByUserId)
	return connect.NewResponse(&v1.UpdateModuleResponse{
		Module: module.Redact(),
	}), nil
}

func (s *ModuleService) DeleteModule(ctx context.Context, req *connect.Request[v1.DeleteModuleRequest]) (*connect.Response[v1.DeleteModuleResponse], error) {
	msg := req.Msg
	if msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("module ID is required"))
	}
	module, _ := s.store.GetModule(ctx, msg.Id)

	if err := s.moduleManager.DeleteModule(ctx, msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete module: %w", err))
	}
	if module != nil {
		s.rec.Record(ctx, module.ServerId, "module.delete", metrics.Attrs{"module": module.Name}, "deleted module %s", module.Name)
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
	if module.ContainerId == "" {
		if err := s.moduleManager.CreateAndStartModule(ctx, msg.Id, true); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create and start module: %w", err))
		}
	} else {
		if err := s.moduleManager.StartModule(ctx, msg.Id); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start module: %w", err))
		}
	}

	s.rec.Record(ctx, module.ServerId, "module.start", metrics.Attrs{"module": module.Name}, "started module %s", module.Name)

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
	if module, err := s.store.GetModule(ctx, msg.Id); err == nil {
		s.rec.Record(ctx, module.ServerId, "module.stop", metrics.Attrs{"module": module.Name}, "stopped module %s", module.Name)
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
	if module, err := s.store.GetModule(ctx, msg.Id); err == nil {
		s.rec.Record(ctx, module.ServerId, "module.restart", metrics.Attrs{"module": module.Name}, "restarted module %s", module.Name)
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

	tail := msg.Tail
	if tail == 0 {
		tail = 100
	}

	// Get structured log entries from the log streamer if available
	var protoLogs []*v1.LogEntry
	if s.logStreamer != nil {
		if module.ContainerId != "" {
			if err := s.logStreamer.StartStreaming(module.Id, module.ContainerId); err != nil {
				s.log.Warn("Failed to start log streaming for module %s: %v", module.Id, err)
			}
		}
		protoLogs = s.logStreamer.GetLogs(module.Id, int(tail))
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
			Port: int32(p),
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
			if serverConfig, err := s.store.GetServerProperties(ctx, *msg.ServerId); err == nil {
				aliasCtx.ServerProperties = serverConfig
			}
		}
	}

	// Get module context if provided
	if msg.ModuleId != nil && *msg.ModuleId != "" {
		if mod, err := s.store.GetModule(ctx, *msg.ModuleId); err == nil {
			aliasCtx.Module = mod
		}
	}

	return connect.NewResponse(&v1.GetAvailableAliasesResponse{
		Aliases: alias.GetAvailableAliases(aliasCtx),
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
			if serverConfig, err := s.store.GetServerProperties(ctx, *msg.ServerId); err == nil {
				aliasCtx.ServerProperties = serverConfig
			}
		}
	}

	if msg.ModuleId != nil && *msg.ModuleId != "" {
		if mod, err := s.store.GetModule(ctx, *msg.ModuleId); err == nil {
			aliasCtx.Module = mod
			if siblings, err := s.store.ListServerModules(ctx, mod.ServerId); err == nil {
				aliasCtx.Modules = make(map[string]*v1.Module)
				for _, sib := range siblings {
					aliasCtx.Modules[sib.Name] = sib
				}
			}
		}
	}

	resolved := alias.GetResolvedAliases(aliasCtx)
	return connect.NewResponse(&v1.GetResolvedAliasesResponse{Aliases: resolved}), nil
}
