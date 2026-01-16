package services

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
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
	store       *storage.Store
	docker      *docker.Client
	config      *config.Config
	proxy       *proxy.Manager
	log         *logger.Logger
	logStreamer *logger.LogStreamer
}

// NewModuleService creates a new module service
func NewModuleService(store *storage.Store, docker *docker.Client, config *config.Config, proxy *proxy.Manager, logStreamer *logger.LogStreamer, log *logger.Logger) *ModuleService {
	return &ModuleService{
		store:       store,
		docker:      docker,
		config:      config,
		proxy:       proxy,
		log:         log,
		logStreamer: logStreamer,
	}
}

// dbModuleToProto converts a database module model to proto module
func dbModuleToProto(module *storage.Module) *v1.Module {
	if module == nil {
		return nil
	}

	protoModule := &v1.Module{
		Id:              module.ID,
		ServerId:        module.ServerID,
		TemplateId:      module.TemplateID,
		Name:            module.Name,
		Description:     module.Description,
		DockerImage:     module.DockerImage,
		ContainerId:     module.ContainerID,
		Memory:          int32(module.Memory),
		ProxyListenerId: module.ProxyListenerID,
		ProxyPort:       int32(module.ProxyPort),
		InjectServerHost: module.InjectServerHost,
		InjectRcon:      module.InjectRCON,
		ShareServerData: module.ShareServerData,
		AutoStart:       module.AutoStart,
		AutoStop:        module.AutoStop,
		MemoryUsage:     module.MemoryUsage,
		CpuPercent:      module.CPUPercent,
		CreatedAt:       timestamppb.New(module.CreatedAt),
		UpdatedAt:       timestamppb.New(module.UpdatedAt),
	}

	// Convert category
	protoModule.Category = dbModuleCategoryToProto(module.Category)

	// Convert status
	protoModule.Status = dbModuleStatusToProto(module.Status)

	// Convert protocol
	protoModule.ProxyProtocol = dbModuleProtocolToProto(module.ProxyProtocol)

	// Convert environment
	protoModule.Environment = module.Environment

	// Convert ports
	protoModule.Ports = dbModulePortsToProto(module.Ports)

	// Convert docker overrides
	protoModule.DockerOverrides = module.DockerOverrides

	// Map optional last started
	if module.LastStarted != nil {
		protoModule.LastStarted = timestamppb.New(*module.LastStarted)
	}

	return protoModule
}

// dbModuleTemplateToProto converts a database module template to proto
func dbModuleTemplateToProto(template *storage.ModuleTemplate) *v1.ModuleTemplate {
	if template == nil {
		return nil
	}

	protoTemplate := &v1.ModuleTemplate{
		Id:               template.ID,
		Name:             template.Name,
		Description:      template.Description,
		DockerImage:      template.DockerImage,
		IconUrl:          template.IconURL,
		InjectServerHost: template.InjectServerHost,
		InjectRcon:       template.InjectRCON,
		ShareServerData:  template.ShareServerData,
		DefaultPort:      int32(template.DefaultPort),
		IsBuiltin:        template.IsBuiltin,
		Version:          template.Version,
		CreatedAt:        timestamppb.New(template.CreatedAt),
		UpdatedAt:        timestamppb.New(template.UpdatedAt),
	}

	// Convert category
	protoTemplate.Category = dbModuleCategoryToProto(template.Category)

	// Convert default protocol
	protoTemplate.DefaultProtocol = dbModuleProtocolToProto(template.DefaultProtocol)

	// Convert env var schema
	protoTemplate.EnvVarSchema = dbEnvVarSchemaToProto(template.EnvVarSchema)

	// Convert port schema
	protoTemplate.PortSchema = dbPortSchemaToProto(template.PortSchema)

	// Convert default overrides
	protoTemplate.DefaultOverrides = template.DefaultOverrides

	return protoTemplate
}

// dbModuleCategoryToProto converts database module category to proto
func dbModuleCategoryToProto(category storage.ModuleCategory) v1.ModuleCategory {
	switch category {
	case storage.ModuleCategoryWebUI:
		return v1.ModuleCategory_MODULE_CATEGORY_WEBUI
	case storage.ModuleCategoryVoice:
		return v1.ModuleCategory_MODULE_CATEGORY_VOICE
	case storage.ModuleCategoryMap:
		return v1.ModuleCategory_MODULE_CATEGORY_MAP
	case storage.ModuleCategoryUtility:
		return v1.ModuleCategory_MODULE_CATEGORY_UTILITY
	case storage.ModuleCategoryCustom:
		return v1.ModuleCategory_MODULE_CATEGORY_CUSTOM
	default:
		return v1.ModuleCategory_MODULE_CATEGORY_UNSPECIFIED
	}
}

// protoModuleCategoryToDB converts proto module category to database
func protoModuleCategoryToDB(category v1.ModuleCategory) storage.ModuleCategory {
	switch category {
	case v1.ModuleCategory_MODULE_CATEGORY_WEBUI:
		return storage.ModuleCategoryWebUI
	case v1.ModuleCategory_MODULE_CATEGORY_VOICE:
		return storage.ModuleCategoryVoice
	case v1.ModuleCategory_MODULE_CATEGORY_MAP:
		return storage.ModuleCategoryMap
	case v1.ModuleCategory_MODULE_CATEGORY_UTILITY:
		return storage.ModuleCategoryUtility
	case v1.ModuleCategory_MODULE_CATEGORY_CUSTOM:
		return storage.ModuleCategoryCustom
	default:
		return storage.ModuleCategoryCustom
	}
}

// dbModuleStatusToProto converts database module status to proto
func dbModuleStatusToProto(status storage.ModuleStatus) v1.ModuleStatus {
	switch status {
	case storage.ModuleStatusCreating:
		return v1.ModuleStatus_MODULE_STATUS_CREATING
	case storage.ModuleStatusStarting:
		return v1.ModuleStatus_MODULE_STATUS_STARTING
	case storage.ModuleStatusRunning:
		return v1.ModuleStatus_MODULE_STATUS_RUNNING
	case storage.ModuleStatusStopping:
		return v1.ModuleStatus_MODULE_STATUS_STOPPING
	case storage.ModuleStatusStopped:
		return v1.ModuleStatus_MODULE_STATUS_STOPPED
	case storage.ModuleStatusError:
		return v1.ModuleStatus_MODULE_STATUS_ERROR
	case storage.ModuleStatusUnhealthy:
		return v1.ModuleStatus_MODULE_STATUS_UNHEALTHY
	default:
		return v1.ModuleStatus_MODULE_STATUS_UNSPECIFIED
	}
}

// dbModuleProtocolToProto converts database module protocol to proto
func dbModuleProtocolToProto(protocol storage.ModuleProtocol) v1.ModuleProtocol {
	switch protocol {
	case storage.ModuleProtocolHTTP:
		return v1.ModuleProtocol_MODULE_PROTOCOL_HTTP
	case storage.ModuleProtocolTCP:
		return v1.ModuleProtocol_MODULE_PROTOCOL_TCP
	case storage.ModuleProtocolNone:
		return v1.ModuleProtocol_MODULE_PROTOCOL_NONE
	default:
		return v1.ModuleProtocol_MODULE_PROTOCOL_UNSPECIFIED
	}
}

// protoModuleProtocolToDB converts proto module protocol to database
func protoModuleProtocolToDB(protocol v1.ModuleProtocol) storage.ModuleProtocol {
	switch protocol {
	case v1.ModuleProtocol_MODULE_PROTOCOL_HTTP:
		return storage.ModuleProtocolHTTP
	case v1.ModuleProtocol_MODULE_PROTOCOL_TCP:
		return storage.ModuleProtocolTCP
	case v1.ModuleProtocol_MODULE_PROTOCOL_NONE:
		return storage.ModuleProtocolNone
	default:
		return storage.ModuleProtocolHTTP
	}
}

// dbModulePortsToProto converts database module ports to proto
func dbModulePortsToProto(ports []*storage.ModulePort) []*v1.ModulePort {
	if ports == nil {
		return nil
	}

	protoPorts := make([]*v1.ModulePort, len(ports))
	for i, port := range ports {
		protoPorts[i] = &v1.ModulePort{
			Name:          port.Name,
			ContainerPort: int32(port.ContainerPort),
			HostPort:      int32(port.HostPort),
			Protocol:      dbModuleProtocolToProto(port.Protocol),
		}
	}
	return protoPorts
}

// protoModulePortsToDB converts proto module ports to database
func protoModulePortsToDB(ports []*v1.ModulePort) []*storage.ModulePort {
	if ports == nil {
		return nil
	}

	dbPorts := make([]*storage.ModulePort, len(ports))
	for i, port := range ports {
		dbPorts[i] = &storage.ModulePort{
			Name:          port.Name,
			ContainerPort: int(port.ContainerPort),
			HostPort:      int(port.HostPort),
			Protocol:      protoModuleProtocolToDB(port.Protocol),
		}
	}
	return dbPorts
}

// dbEnvVarSchemaToProto converts database env var schema to proto
func dbEnvVarSchemaToProto(schema []*storage.ModuleEnvVarDef) []*v1.ModuleEnvVarDef {
	if schema == nil {
		return nil
	}

	protoSchema := make([]*v1.ModuleEnvVarDef, len(schema))
	for i, def := range schema {
		protoSchema[i] = &v1.ModuleEnvVarDef{
			Name:         def.Name,
			Description:  def.Description,
			DefaultValue: def.Default,
			Required:     def.Required,
			IsSecret:     def.IsSecret,
		}
	}
	return protoSchema
}

// dbPortSchemaToProto converts database port schema to proto
func dbPortSchemaToProto(schema []*storage.ModulePortDef) []*v1.ModulePortDef {
	if schema == nil {
		return nil
	}

	protoSchema := make([]*v1.ModulePortDef, len(schema))
	for i, def := range schema {
		protoSchema[i] = &v1.ModulePortDef{
			Name:          def.Name,
			ContainerPort: int32(def.ContainerPort),
			Protocol:      dbModuleProtocolToProto(def.Protocol),
			Required:      def.Required,
		}
	}
	return protoSchema
}

// ListModules lists all modules for a server
func (s *ModuleService) ListModules(ctx context.Context, req *connect.Request[v1.ListModulesRequest]) (*connect.Response[v1.ListModulesResponse], error) {
	var modules []*storage.Module
	var err error

	if req.Msg.ServerId != "" {
		modules, err = s.store.ListModulesByServer(ctx, req.Msg.ServerId)
	} else {
		modules, err = s.store.ListModules(ctx)
	}

	if err != nil {
		s.log.Error("Failed to list modules: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list modules"))
	}

	// Update status from Docker
	for _, module := range modules {
		if module.ContainerID != "" {
			status, err := s.docker.GetModuleContainerStatus(ctx, module.ContainerID)
			if err == nil {
				module.Status = status
			}
		}
	}

	// Filter by category if specified
	if req.Msg.Category != nil {
		targetCategory := protoModuleCategoryToDB(*req.Msg.Category)
		filtered := make([]*storage.Module, 0)
		for _, module := range modules {
			if module.Category == targetCategory {
				filtered = append(filtered, module)
			}
		}
		modules = filtered
	}

	// Convert to proto
	protoModules := make([]*v1.Module, len(modules))
	for i, module := range modules {
		protoModules[i] = dbModuleToProto(module)
	}

	return connect.NewResponse(&v1.ListModulesResponse{
		Modules: protoModules,
	}), nil
}

// GetModule gets a specific module
func (s *ModuleService) GetModule(ctx context.Context, req *connect.Request[v1.GetModuleRequest]) (*connect.Response[v1.GetModuleResponse], error) {
	module, err := s.store.GetModule(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found"))
	}

	// Update status from Docker
	if module.ContainerID != "" {
		status, err := s.docker.GetModuleContainerStatus(ctx, module.ContainerID)
		if err == nil {
			module.Status = status
		}
	}

	return connect.NewResponse(&v1.GetModuleResponse{
		Module: dbModuleToProto(module),
	}), nil
}

// CreateModule creates a new module
func (s *ModuleService) CreateModule(ctx context.Context, req *connect.Request[v1.CreateModuleRequest]) (*connect.Response[v1.CreateModuleResponse], error) {
	msg := req.Msg

	// Validate server exists
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Get server config
	serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server config"))
	}

	module := &storage.Module{
		ID:          uuid.New().String(),
		ServerID:    msg.ServerId,
		Name:        msg.Name,
		Description: msg.Description,
		Status:      storage.ModuleStatusCreating,
		Environment: msg.Environment,
		Ports:       protoModulePortsToDB(msg.Ports),
	}

	// Load template if specified
	var template *storage.ModuleTemplate
	if msg.TemplateId != nil && *msg.TemplateId != "" {
		template, err = s.store.GetModuleTemplate(ctx, *msg.TemplateId)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("template not found"))
		}

		// Apply template defaults
		module.TemplateID = template.ID
		module.DockerImage = template.DockerImage
		module.Category = template.Category
		module.ProxyProtocol = template.DefaultProtocol
		module.ProxyPort = template.DefaultPort
		module.InjectServerHost = template.InjectServerHost
		module.InjectRCON = template.InjectRCON
		module.ShareServerData = template.ShareServerData
		module.DockerOverrides = template.DefaultOverrides

		// Apply default ports from template schema if none provided
		if len(module.Ports) == 0 && len(template.PortSchema) > 0 {
			module.Ports = make([]*storage.ModulePort, len(template.PortSchema))
			for i, portDef := range template.PortSchema {
				module.Ports[i] = &storage.ModulePort{
					Name:          portDef.Name,
					ContainerPort: portDef.ContainerPort,
					Protocol:      portDef.Protocol,
				}
			}
		}
	}

	// Override with custom values if provided
	if msg.DockerImage != nil && *msg.DockerImage != "" {
		module.DockerImage = *msg.DockerImage
	}
	if msg.Category != nil {
		module.Category = protoModuleCategoryToDB(*msg.Category)
	}
	if msg.DockerOverrides != nil {
		module.DockerOverrides = msg.DockerOverrides
	}
	if msg.Memory != nil {
		module.Memory = int(*msg.Memory)
	} else {
		module.Memory = 256 // Default 256MB
	}
	if msg.InjectServerHost != nil {
		module.InjectServerHost = *msg.InjectServerHost
	}
	if msg.InjectRcon != nil {
		module.InjectRCON = *msg.InjectRcon
	}
	if msg.ShareServerData != nil {
		module.ShareServerData = *msg.ShareServerData
	}
	if msg.AutoStart != nil {
		module.AutoStart = *msg.AutoStart
	} else {
		module.AutoStart = true
	}
	if msg.AutoStop != nil {
		module.AutoStop = *msg.AutoStop
	} else {
		module.AutoStop = true
	}

	// Use server's proxy listener
	module.ProxyListenerID = server.ProxyListenerID

	// Validate docker image
	if module.DockerImage == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("docker image is required"))
	}

	// Create module in database
	if err := s.store.CreateModule(ctx, module); err != nil {
		s.log.Error("Failed to create module: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create module"))
	}

	// Create Docker container
	containerID, err := s.docker.CreateModuleContainer(ctx, module, server, serverConfig, template)
	if err != nil {
		s.log.Error("Failed to create module container: %v", err)
		module.Status = storage.ModuleStatusError
		s.store.UpdateModule(ctx, module)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create container: %v", err))
	}

	module.ContainerID = containerID
	module.Status = storage.ModuleStatusStopped

	if err := s.store.UpdateModule(ctx, module); err != nil {
		s.log.Error("Failed to update module with container ID: %v", err)
	}

	// Start immediately if requested
	if msg.StartImmediately {
		if err := s.startModule(ctx, module, server); err != nil {
			s.log.Error("Failed to start module: %v", err)
		}
	}

	return connect.NewResponse(&v1.CreateModuleResponse{
		Module: dbModuleToProto(module),
	}), nil
}

// UpdateModule updates a module configuration
func (s *ModuleService) UpdateModule(ctx context.Context, req *connect.Request[v1.UpdateModuleRequest]) (*connect.Response[v1.UpdateModuleResponse], error) {
	msg := req.Msg

	module, err := s.store.GetModule(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found"))
	}

	// Track if we need to recreate the container
	needsRecreate := false

	// Update fields if provided
	if msg.Name != nil && *msg.Name != "" {
		module.Name = *msg.Name
	}
	if msg.Description != nil {
		module.Description = *msg.Description
	}
	if msg.DockerImage != nil && *msg.DockerImage != "" && *msg.DockerImage != module.DockerImage {
		module.DockerImage = *msg.DockerImage
		needsRecreate = true
	}
	if len(msg.Environment) > 0 {
		module.Environment = msg.Environment
		needsRecreate = true
	}
	if len(msg.Ports) > 0 {
		module.Ports = protoModulePortsToDB(msg.Ports)
		needsRecreate = true
	}
	if msg.DockerOverrides != nil {
		module.DockerOverrides = msg.DockerOverrides
		needsRecreate = true
	}
	if msg.Memory != nil {
		module.Memory = int(*msg.Memory)
		needsRecreate = true
	}
	if msg.InjectServerHost != nil {
		module.InjectServerHost = *msg.InjectServerHost
		needsRecreate = true
	}
	if msg.InjectRcon != nil {
		module.InjectRCON = *msg.InjectRcon
		needsRecreate = true
	}
	if msg.ShareServerData != nil {
		module.ShareServerData = *msg.ShareServerData
		needsRecreate = true
	}
	if msg.AutoStart != nil {
		module.AutoStart = *msg.AutoStart
	}
	if msg.AutoStop != nil {
		module.AutoStop = *msg.AutoStop
	}

	// Recreate container if needed
	if needsRecreate && module.ContainerID != "" {
		server, err := s.store.GetServer(ctx, module.ServerID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server"))
		}

		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server config"))
		}

		var template *storage.ModuleTemplate
		if module.TemplateID != "" {
			template, _ = s.store.GetModuleTemplate(ctx, module.TemplateID)
		}

		result, err := s.docker.RecreateModuleContainer(ctx, module.ContainerID, module, server, serverConfig, template)
		if err != nil {
			s.log.Error("Failed to recreate module container: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to recreate container: %v", err))
		}

		module.ContainerID = result.NewContainerID

		// Update proxy route if container was running
		if result.WasRunning && module.ProxyProtocol == storage.ModuleProtocolHTTP {
			containerIP, err := proxy.GetContainerIP(module.ContainerID, s.config.Docker.NetworkName)
			if err == nil {
				s.proxy.UpdateModuleRoute(module, server, containerIP)
			}
		}
	}

	if err := s.store.UpdateModule(ctx, module); err != nil {
		s.log.Error("Failed to update module: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update module"))
	}

	return connect.NewResponse(&v1.UpdateModuleResponse{
		Module: dbModuleToProto(module),
	}), nil
}

// DeleteModule deletes a module
func (s *ModuleService) DeleteModule(ctx context.Context, req *connect.Request[v1.DeleteModuleRequest]) (*connect.Response[v1.DeleteModuleResponse], error) {
	module, err := s.store.GetModule(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found"))
	}

	// Remove proxy route
	if module.ProxyProtocol == storage.ModuleProtocolHTTP {
		server, _ := s.store.GetServer(ctx, module.ServerID)
		if server != nil {
			s.proxy.RemoveModuleRoute(module, server)
		}
	}

	// Stop and remove container
	if module.ContainerID != "" {
		if s.logStreamer != nil {
			s.logStreamer.StopStreaming(module.ContainerID)
		}
		s.docker.StopContainer(ctx, module.ContainerID)
		s.docker.RemoveContainer(ctx, module.ContainerID)
	}

	// Delete from database
	if err := s.store.DeleteModule(ctx, req.Msg.Id); err != nil {
		s.log.Error("Failed to delete module: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete module"))
	}

	return connect.NewResponse(&v1.DeleteModuleResponse{}), nil
}

// StartModule starts a module container
func (s *ModuleService) StartModule(ctx context.Context, req *connect.Request[v1.StartModuleRequest]) (*connect.Response[v1.StartModuleResponse], error) {
	module, err := s.store.GetModule(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found"))
	}

	server, err := s.store.GetServer(ctx, module.ServerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server"))
	}

	if err := s.startModule(ctx, module, server); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start module: %v", err))
	}

	return connect.NewResponse(&v1.StartModuleResponse{
		Status: "started",
	}), nil
}

// startModule is the internal method to start a module
func (s *ModuleService) startModule(ctx context.Context, module *storage.Module, server *storage.Server) error {
	if module.ContainerID == "" {
		return fmt.Errorf("module has no container")
	}

	// Update status
	module.Status = storage.ModuleStatusStarting
	s.store.UpdateModule(ctx, module)

	// Start container
	if err := s.docker.StartContainer(ctx, module.ContainerID); err != nil {
		module.Status = storage.ModuleStatusError
		s.store.UpdateModule(ctx, module)
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Start log streaming if configured
	if s.logStreamer != nil {
		if err := s.logStreamer.StartStreaming(module.ContainerID); err != nil {
			s.log.Warn("Failed to start log streaming for module %s: %v", module.ID, err)
		}
	}

	// Update proxy route for HTTP modules
	if module.ProxyProtocol == storage.ModuleProtocolHTTP && server.ProxyHostname != "" {
		containerIP, err := proxy.GetContainerIP(module.ContainerID, s.config.Docker.NetworkName)
		if err == nil {
			s.proxy.UpdateModuleRoute(module, server, containerIP)
		} else {
			s.log.Warn("Failed to get container IP for module %s: %v", module.ID, err)
		}
	}

	now := time.Now()
	module.Status = storage.ModuleStatusRunning
	module.LastStarted = &now
	s.store.UpdateModule(ctx, module)

	s.log.Info("Started module %s (%s)", module.Name, module.ID)
	return nil
}

// StopModule stops a module container
func (s *ModuleService) StopModule(ctx context.Context, req *connect.Request[v1.StopModuleRequest]) (*connect.Response[v1.StopModuleResponse], error) {
	module, err := s.store.GetModule(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found"))
	}

	server, err := s.store.GetServer(ctx, module.ServerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server"))
	}

	if err := s.stopModule(ctx, module, server); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop module: %v", err))
	}

	return connect.NewResponse(&v1.StopModuleResponse{
		Status: "stopped",
	}), nil
}

// stopModule is the internal method to stop a module
func (s *ModuleService) stopModule(ctx context.Context, module *storage.Module, server *storage.Server) error {
	if module.ContainerID == "" {
		return nil
	}

	// Update status
	module.Status = storage.ModuleStatusStopping
	s.store.UpdateModule(ctx, module)

	// Remove proxy route
	if module.ProxyProtocol == storage.ModuleProtocolHTTP {
		s.proxy.RemoveModuleRoute(module, server)
	}

	// Stop log streaming
	if s.logStreamer != nil {
		s.logStreamer.StopStreaming(module.ContainerID)
	}

	// Stop container
	if _, err := s.docker.StopContainer(ctx, module.ContainerID); err != nil {
		s.log.Warn("Failed to stop module container: %v", err)
	}

	module.Status = storage.ModuleStatusStopped
	s.store.UpdateModule(ctx, module)

	s.log.Info("Stopped module %s (%s)", module.Name, module.ID)
	return nil
}

// RestartModule restarts a module container
func (s *ModuleService) RestartModule(ctx context.Context, req *connect.Request[v1.RestartModuleRequest]) (*connect.Response[v1.RestartModuleResponse], error) {
	module, err := s.store.GetModule(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found"))
	}

	server, err := s.store.GetServer(ctx, module.ServerID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server"))
	}

	if err := s.stopModule(ctx, module, server); err != nil {
		s.log.Warn("Error stopping module during restart: %v", err)
	}

	if err := s.startModule(ctx, module, server); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to restart module: %v", err))
	}

	return connect.NewResponse(&v1.RestartModuleResponse{
		Status: "restarted",
	}), nil
}

// GetModuleLogs gets logs from a module container
func (s *ModuleService) GetModuleLogs(ctx context.Context, req *connect.Request[v1.GetModuleLogsRequest]) (*connect.Response[v1.GetModuleLogsResponse], error) {
	module, err := s.store.GetModule(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("module not found"))
	}

	if module.ContainerID == "" {
		return connect.NewResponse(&v1.GetModuleLogsResponse{
			Logs:  []*v1.ModuleLogEntry{},
			Total: 0,
		}), nil
	}

	// Get logs from log streamer if available
	var logs []*v1.ModuleLogEntry
	if s.logStreamer != nil {
		tail := int(req.Msg.Tail)
		if tail == 0 {
			tail = 100
		}

		entries := s.logStreamer.GetLogs(module.ContainerID, tail)
		logs = make([]*v1.ModuleLogEntry, len(entries))
		for i, entry := range entries {
			logs[i] = &v1.ModuleLogEntry{
				Timestamp: entry.Timestamp,
				Message:   entry.Message,
				Stream:    entry.Source, // Use Source field from LogEntry
			}
		}
	}

	return connect.NewResponse(&v1.GetModuleLogsResponse{
		Logs:  logs,
		Total: int32(len(logs)),
	}), nil
}

// ListModuleTemplates lists available module templates
func (s *ModuleService) ListModuleTemplates(ctx context.Context, req *connect.Request[v1.ListModuleTemplatesRequest]) (*connect.Response[v1.ListModuleTemplatesResponse], error) {
	var templates []*storage.ModuleTemplate
	var err error

	if req.Msg.Category != nil {
		category := protoModuleCategoryToDB(*req.Msg.Category)
		templates, err = s.store.ListModuleTemplatesByCategory(ctx, category)
	} else {
		templates, err = s.store.ListModuleTemplates(ctx)
	}

	if err != nil {
		s.log.Error("Failed to list module templates: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list templates"))
	}

	// Convert to proto
	protoTemplates := make([]*v1.ModuleTemplate, len(templates))
	for i, template := range templates {
		protoTemplates[i] = dbModuleTemplateToProto(template)
	}

	return connect.NewResponse(&v1.ListModuleTemplatesResponse{
		Templates: protoTemplates,
	}), nil
}

// GetModuleTemplate gets a specific module template
func (s *ModuleService) GetModuleTemplate(ctx context.Context, req *connect.Request[v1.GetModuleTemplateRequest]) (*connect.Response[v1.GetModuleTemplateResponse], error) {
	template, err := s.store.GetModuleTemplate(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("template not found"))
	}

	return connect.NewResponse(&v1.GetModuleTemplateResponse{
		Template: dbModuleTemplateToProto(template),
	}), nil
}

// CreateModuleTemplate creates a custom module template
func (s *ModuleService) CreateModuleTemplate(ctx context.Context, req *connect.Request[v1.CreateModuleTemplateRequest]) (*connect.Response[v1.CreateModuleTemplateResponse], error) {
	msg := req.Msg

	template := &storage.ModuleTemplate{
		ID:               uuid.New().String(),
		Name:             msg.Name,
		Description:      msg.Description,
		Category:         protoModuleCategoryToDB(msg.Category),
		DockerImage:      msg.DockerImage,
		IconURL:          msg.IconUrl,
		InjectServerHost: msg.InjectServerHost,
		InjectRCON:       msg.InjectRcon,
		ShareServerData:  msg.ShareServerData,
		DefaultProtocol:  protoModuleProtocolToDB(msg.DefaultProtocol),
		DefaultPort:      int(msg.DefaultPort),
		DefaultOverrides: msg.DefaultOverrides,
		IsBuiltin:        false,
	}

	// Convert env var schema
	if len(msg.EnvVarSchema) > 0 {
		template.EnvVarSchema = make([]*storage.ModuleEnvVarDef, len(msg.EnvVarSchema))
		for i, def := range msg.EnvVarSchema {
			template.EnvVarSchema[i] = &storage.ModuleEnvVarDef{
				Name:        def.Name,
				Description: def.Description,
				Default:     def.DefaultValue,
				Required:    def.Required,
				IsSecret:    def.IsSecret,
			}
		}
	}

	// Convert port schema
	if len(msg.PortSchema) > 0 {
		template.PortSchema = make([]*storage.ModulePortDef, len(msg.PortSchema))
		for i, def := range msg.PortSchema {
			template.PortSchema[i] = &storage.ModulePortDef{
				Name:          def.Name,
				ContainerPort: int(def.ContainerPort),
				Protocol:      protoModuleProtocolToDB(def.Protocol),
				Required:      def.Required,
			}
		}
	}

	if err := s.store.CreateModuleTemplate(ctx, template); err != nil {
		s.log.Error("Failed to create module template: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create template"))
	}

	return connect.NewResponse(&v1.CreateModuleTemplateResponse{
		Template: dbModuleTemplateToProto(template),
	}), nil
}

// DeleteModuleTemplate deletes a custom module template
func (s *ModuleService) DeleteModuleTemplate(ctx context.Context, req *connect.Request[v1.DeleteModuleTemplateRequest]) (*connect.Response[v1.DeleteModuleTemplateResponse], error) {
	template, err := s.store.GetModuleTemplate(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("template not found"))
	}

	// Don't allow deleting builtin templates
	if template.IsBuiltin {
		return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("cannot delete builtin template"))
	}

	if err := s.store.DeleteModuleTemplate(ctx, req.Msg.Id); err != nil {
		s.log.Error("Failed to delete module template: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete template"))
	}

	return connect.NewResponse(&v1.DeleteModuleTemplateResponse{}), nil
}

// StartModulesForServer starts all auto-start modules for a server
func (s *ModuleService) StartModulesForServer(ctx context.Context, server *storage.Server) {
	modules, err := s.store.ListModulesByServer(ctx, server.ID)
	if err != nil {
		s.log.Error("Failed to list modules for server %s: %v", server.ID, err)
		return
	}

	for _, module := range modules {
		if module.AutoStart {
			if err := s.startModule(ctx, module, server); err != nil {
				s.log.Error("Failed to auto-start module %s: %v", module.Name, err)
			}
		}
	}
}

// StopModulesForServer stops all auto-stop modules for a server
func (s *ModuleService) StopModulesForServer(ctx context.Context, server *storage.Server) {
	modules, err := s.store.ListModulesByServer(ctx, server.ID)
	if err != nil {
		s.log.Error("Failed to list modules for server %s: %v", server.ID, err)
		return
	}

	for _, module := range modules {
		if module.AutoStop {
			if err := s.stopModule(ctx, module, server); err != nil {
				s.log.Error("Failed to auto-stop module %s: %v", module.Name, err)
			}
		}
	}
}
