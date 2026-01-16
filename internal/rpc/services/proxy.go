package services

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that ProxyService implements the interface
var _ discopanelv1connect.ProxyServiceHandler = (*ProxyService)(nil)

// ProxyService implements the Proxy service
type ProxyService struct {
	store        *storage.Store
	docker       *docker.Client
	proxyManager *proxy.Manager
	config       *config.Config
	log          *logger.Logger
	logStreamer  *logger.LogStreamer
}

// NewProxyService creates a new proxy service
func NewProxyService(store *storage.Store, dockerClient *docker.Client, proxyManager *proxy.Manager, cfg *config.Config, logStreamer *logger.LogStreamer, log *logger.Logger) *ProxyService {
	return &ProxyService{
		store:        store,
		docker:       dockerClient,
		proxyManager: proxyManager,
		config:       cfg,
		log:          log,
		logStreamer:  logStreamer,
	}
}

// GetProxyRoutes gets proxy routes
func (s *ProxyService) GetProxyRoutes(ctx context.Context, req *connect.Request[v1.GetProxyRoutesRequest]) (*connect.Response[v1.GetProxyRoutesResponse], error) {
	if s.proxyManager == nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("proxy not enabled"))
	}

	routes := s.proxyManager.GetRoutes()

	// Convert to proto format
	protoRoutes := make([]*v1.ProxyRoute, 0, len(routes))
	for _, route := range routes {
		protoRoutes = append(protoRoutes, &v1.ProxyRoute{
			ServerId:    route.ServerID,
			Hostname:    route.Hostname,
			BackendHost: route.BackendHost,
			BackendPort: int32(route.BackendPort),
			Active:      route.Active,
		})
	}

	return connect.NewResponse(&v1.GetProxyRoutesResponse{
		Routes: protoRoutes,
	}), nil
}

// GetProxyStatus gets proxy status
func (s *ProxyService) GetProxyStatus(ctx context.Context, req *connect.Request[v1.GetProxyStatusRequest]) (*connect.Response[v1.GetProxyStatusResponse], error) {
	// Load proxy config from database
	proxyConfig, _, err := s.store.GetProxyConfig(ctx)
	if err != nil {
		s.log.Error("Failed to load proxy configuration: %v", err)
		proxyConfig = &storage.ProxyConfig{
			Enabled: s.config.Proxy.Enabled,
			BaseURL: s.config.Proxy.BaseURL,
		}
	}

	// Get listeners
	listeners, err := s.store.GetProxyListeners(ctx)
	if err != nil {
		s.log.Error("Failed to load proxy listeners: %v", err)
		listeners = []*storage.ProxyListener{}
	}

	// Convert listeners to proto format and ports array
	protoListeners := make([]*v1.ProxyListener, len(listeners))
	listenPorts := make([]int32, len(listeners))
	for i, l := range listeners {
		listenPorts[i] = int32(l.Port)
		protoListeners[i] = &v1.ProxyListener{
			Id:          l.ID,
			Name:        l.Name,
			Description: l.Description,
			Port:        int32(l.Port),
			Enabled:     l.Enabled,
			IsDefault:   l.IsDefault,
			CreatedAt:   timestamppb.New(l.CreatedAt),
			UpdatedAt:   timestamppb.New(l.UpdatedAt),
		}
	}

	// Primary port
	var primaryPort int32
	if len(listenPorts) > 0 {
		primaryPort = listenPorts[0]
	}

	// Get running status and active routes count
	running := false
	activeRoutes := int32(0)
	if s.proxyManager != nil {
		running = s.proxyManager.IsRunning()
		routes := s.proxyManager.GetRoutes()
		activeRoutes = int32(len(routes))
	}

	return connect.NewResponse(&v1.GetProxyStatusResponse{
		Enabled:      proxyConfig.Enabled,
		BaseUrl:      proxyConfig.BaseURL,
		ListenPorts:  listenPorts,
		Listeners:    protoListeners,
		ListenPort:   primaryPort,
		Running:      running,
		ActiveRoutes: activeRoutes,
	}), nil
}

// UpdateProxyConfig updates proxy configuration
func (s *ProxyService) UpdateProxyConfig(ctx context.Context, req *connect.Request[v1.UpdateProxyConfigRequest]) (*connect.Response[v1.UpdateProxyConfigResponse], error) {
	msg := req.Msg

	// Save to database
	proxyConfig := &storage.ProxyConfig{
		ID:      "default",
		Enabled: msg.Enabled,
		BaseURL: msg.BaseUrl,
	}

	if err := s.store.SaveProxyConfig(ctx, proxyConfig); err != nil {
		s.log.Error("Failed to save proxy configuration: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save proxy configuration"))
	}

	// Update in-memory configuration
	s.config.Proxy.Enabled = msg.Enabled
	s.config.Proxy.BaseURL = msg.BaseUrl

	s.log.Info("Proxy configuration saved to database: enabled=%v, base_url=%v", msg.Enabled, msg.BaseUrl)

	// Return updated status (same as GetProxyStatus response)
	statusResp, err := s.GetProxyStatus(ctx, connect.NewRequest(&v1.GetProxyStatusRequest{}))
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&v1.UpdateProxyConfigResponse{
		Enabled:      statusResp.Msg.Enabled,
		BaseUrl:      statusResp.Msg.BaseUrl,
		ListenPorts:  statusResp.Msg.ListenPorts,
		Listeners:    statusResp.Msg.Listeners,
		ListenPort:   statusResp.Msg.ListenPort,
		Running:      statusResp.Msg.Running,
		ActiveRoutes: statusResp.Msg.ActiveRoutes,
	}), nil
}

// GetProxyListeners gets proxy listeners
func (s *ProxyService) GetProxyListeners(ctx context.Context, req *connect.Request[v1.GetProxyListenersRequest]) (*connect.Response[v1.GetProxyListenersResponse], error) {
	listeners, err := s.store.GetProxyListeners(ctx)
	if err != nil {
		s.log.Error("Failed to get proxy listeners: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get proxy listeners"))
	}

	// Get all servers to count usage
	servers, _ := s.store.ListServers(ctx)

	// Convert to proto format with server count
	protoListeners := make([]*v1.ProxyListenerWithCount, len(listeners))
	for i, listener := range listeners {
		// Count servers using this listener
		count := int32(0)
		for _, server := range servers {
			if server.ProxyListenerID == listener.ID {
				count++
			}
		}

		protoListeners[i] = &v1.ProxyListenerWithCount{
			Listener: &v1.ProxyListener{
				Id:          listener.ID,
				Name:        listener.Name,
				Description: listener.Description,
				Port:        int32(listener.Port),
				Enabled:     listener.Enabled,
				IsDefault:   listener.IsDefault,
				CreatedAt:   timestamppb.New(listener.CreatedAt),
				UpdatedAt:   timestamppb.New(listener.UpdatedAt),
			},
			ServerCount: count,
		}
	}

	return connect.NewResponse(&v1.GetProxyListenersResponse{
		Listeners: protoListeners,
	}), nil
}

// CreateProxyListener creates a proxy listener
func (s *ProxyService) CreateProxyListener(ctx context.Context, req *connect.Request[v1.CreateProxyListenerRequest]) (*connect.Response[v1.CreateProxyListenerResponse], error) {
	msg := req.Msg

	// Validate port
	if msg.Port < 1 || msg.Port > 65535 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid port number"))
	}

	// Check if port is already in use
	existing, _ := s.store.GetProxyListenerByPort(ctx, int(msg.Port))
	if existing != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("port already in use by another listener"))
	}

	// Check if port is used by a non-proxied server
	servers, _ := s.store.ListServers(ctx)
	for _, server := range servers {
		if server.ProxyHostname == "" && server.Port == int(msg.Port) {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("port already in use by a non-proxied server"))
		}
	}

	listener := &storage.ProxyListener{
		Name:        msg.Name,
		Description: msg.Description,
		Port:        int(msg.Port),
		Enabled:     msg.Enabled,
		IsDefault:   msg.IsDefault,
	}

	if err := s.store.CreateProxyListener(ctx, listener); err != nil {
		s.log.Error("Failed to create proxy listener: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create proxy listener"))
	}

	// Add the listener to the proxy manager if it's running
	if s.proxyManager != nil {
		if err := s.proxyManager.AddListener(listener); err != nil {
			s.log.Error("Failed to add listener to proxy manager: %v", err)
			// Not critical - proxy can be restarted to pick it up
		}
	}

	return connect.NewResponse(&v1.CreateProxyListenerResponse{
		Listener: &v1.ProxyListener{
			Id:          listener.ID,
			Name:        listener.Name,
			Description: listener.Description,
			Port:        int32(listener.Port),
			Enabled:     listener.Enabled,
			IsDefault:   listener.IsDefault,
			CreatedAt:   timestamppb.New(listener.CreatedAt),
			UpdatedAt:   timestamppb.New(listener.UpdatedAt),
		},
	}), nil
}

// UpdateProxyListener updates a proxy listener
func (s *ProxyService) UpdateProxyListener(ctx context.Context, req *connect.Request[v1.UpdateProxyListenerRequest]) (*connect.Response[v1.UpdateProxyListenerResponse], error) {
	msg := req.Msg

	listener, err := s.store.GetProxyListener(ctx, msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("listener not found"))
	}

	// Update fields
	listener.Name = msg.Name
	listener.Description = msg.Description
	listener.Enabled = msg.Enabled
	listener.IsDefault = msg.IsDefault

	// If setting as default, unset other defaults
	if msg.IsDefault {
		listeners, _ := s.store.GetProxyListeners(ctx)
		for _, l := range listeners {
			if l.ID != msg.Id && l.IsDefault {
				l.IsDefault = false
				s.store.UpdateProxyListener(ctx, l)
			}
		}
	}

	oldPort := listener.Port
	if msg.Port != 0 && msg.Port != int32(oldPort) {
		listener.Port = int(msg.Port) // Update port if provided and different
	}

	if err := s.store.UpdateProxyListener(ctx, listener); err != nil {
		s.log.Error("Failed to update proxy listener: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update proxy listener"))
	}

	// Handle proxy manager updates if running
	if s.proxyManager != nil {
		// If port changed, remove old and add new
		if oldPort != listener.Port {
			s.proxyManager.RemoveListener(oldPort)
			if listener.Enabled {
				if err := s.proxyManager.AddListener(listener); err != nil {
					s.log.Error("Failed to add updated listener to proxy manager: %v", err)
				}
			}
		} else if !listener.Enabled {
			// If disabled, remove it
			s.proxyManager.RemoveListener(listener.Port)
		} else if listener.Enabled {
			// If enabled and port didn't change, try to add it (in case it wasn't there)
			s.proxyManager.AddListener(listener)
		}
	}

	return connect.NewResponse(&v1.UpdateProxyListenerResponse{
		Listener: &v1.ProxyListener{
			Id:          listener.ID,
			Name:        listener.Name,
			Description: listener.Description,
			Port:        int32(listener.Port),
			Enabled:     listener.Enabled,
			IsDefault:   listener.IsDefault,
			CreatedAt:   timestamppb.New(listener.CreatedAt),
			UpdatedAt:   timestamppb.New(listener.UpdatedAt),
		},
	}), nil
}

// DeleteProxyListener deletes a proxy listener
func (s *ProxyService) DeleteProxyListener(ctx context.Context, req *connect.Request[v1.DeleteProxyListenerRequest]) (*connect.Response[v1.DeleteProxyListenerResponse], error) {
	// Get the listener first to know which port to remove from proxy manager
	listener, err := s.store.GetProxyListener(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("listener not found"))
	}

	if err := s.store.DeleteProxyListener(ctx, req.Msg.Id); err != nil {
		if strings.Contains(err.Error(), "servers are using it") {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		} else {
			s.log.Error("Failed to delete proxy listener: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete proxy listener"))
		}
	}

	// Remove the listener from the proxy manager if it's running
	if s.proxyManager != nil {
		if err := s.proxyManager.RemoveListener(listener.Port); err != nil {
			s.log.Error("Failed to remove listener from proxy manager: %v", err)
			// Not critical - proxy can be restarted to clean it up
		}
	}

	return connect.NewResponse(&v1.DeleteProxyListenerResponse{
		Status: "deleted",
	}), nil
}

// GetServerRouting gets server routing configuration
func (s *ProxyService) GetServerRouting(ctx context.Context, req *connect.Request[v1.GetServerRoutingRequest]) (*connect.Response[v1.GetServerRoutingResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Get suggested hostname if not set
	suggestedHostname := ""
	if server.ProxyHostname == "" && s.config.Proxy.BaseURL != "" {
		suggestedHostname = strings.ToLower(strings.ReplaceAll(server.Name, " ", "-")) + "." + s.config.Proxy.BaseURL
	}

	// Check if proxy is enabled and get current route
	var currentRoute *v1.ServerRoute
	if s.proxyManager != nil {
		routes := s.proxyManager.GetRoutes()
		for hostname, route := range routes {
			if route.ServerID == server.ID {
				currentRoute = &v1.ServerRoute{
					Hostname: hostname,
					Active:   route.Active,
				}
				break
			}
		}
	}

	// Get listen port from the listener if assigned
	listenPort := int32(s.config.Proxy.ListenPort)
	if server.ProxyListenerID != "" {
		if listener, err := s.store.GetProxyListener(ctx, server.ProxyListenerID); err == nil {
			listenPort = int32(listener.Port)
		}
	}

	return connect.NewResponse(&v1.GetServerRoutingResponse{
		ProxyEnabled:      s.config.Proxy.Enabled,
		ProxyHostname:     server.ProxyHostname,
		ProxyListenerId:   server.ProxyListenerID,
		SuggestedHostname: suggestedHostname,
		BaseUrl:           s.config.Proxy.BaseURL,
		ListenPort:        listenPort,
		CurrentRoute:      currentRoute,
	}), nil
}

// UpdateServerRouting updates server routing configuration
func (s *ProxyService) UpdateServerRouting(ctx context.Context, req *connect.Request[v1.UpdateServerRoutingRequest]) (*connect.Response[v1.UpdateServerRoutingResponse], error) {
	msg := req.Msg

	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Store old values to detect changes
	oldProxyHostname := server.ProxyHostname
	oldProxyListenerID := server.ProxyListenerID

	// Validate and normalize hostname
	hostname := strings.TrimSpace(strings.ToLower(msg.ProxyHostname))
	if hostname != "" {
		if strings.Contains(hostname, " ") || strings.Contains(hostname, "://") {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid hostname format"))
		}

		// Check for conflicts with other servers
		servers, err := s.store.ListServers(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check hostname conflicts"))
		}
		for _, srv := range servers {
			if srv.ID != server.ID && srv.ProxyHostname == hostname {
				return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("hostname already in use by another server"))
			}
		}
	}

	// Determine new listener ID
	listenerID := msg.ProxyListenerId
	if listenerID == "" && hostname != "" {
		// If enabling proxy but no listener specified, use existing or get default
		if oldProxyListenerID != "" {
			listenerID = oldProxyListenerID
		} else {
			// Find default listener
			listeners, err := s.store.GetProxyListeners(ctx)
			if err == nil {
				for _, l := range listeners {
					if l.IsDefault && l.Enabled {
						listenerID = l.ID
						break
					}
				}
				// If no default, use first enabled listener
				if listenerID == "" {
					for _, l := range listeners {
						if l.Enabled {
							listenerID = l.ID
							break
						}
					}
				}
			}
		}
		if listenerID == "" && hostname != "" {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("no proxy listener available"))
		}
	}

	// Clear listener if disabling proxy
	if hostname == "" {
		listenerID = ""
	}

	// Detect what changed
	hostnameChanged := oldProxyHostname != hostname
	listenerChanged := oldProxyListenerID != listenerID
	proxyModeChanged := (oldProxyHostname == "") != (hostname == "")

	// Container recreation needed if proxy mode changes OR listener changes while proxy is enabled
	needsRecreation := proxyModeChanged || (listenerChanged && hostname != "" && oldProxyHostname != "")

	// Removes old route BEFORE updating server in DB and using its hostname
	if hostnameChanged && oldProxyHostname != "" && s.proxyManager != nil {
		if err := s.proxyManager.RemoveRouteByHostname(oldProxyHostname, oldProxyListenerID); err != nil {
			s.log.Error("Failed to remove old proxy route: %v", err)
		}
	}

	// Update server fields
	server.ProxyHostname = hostname
	server.ProxyListenerID = listenerID

	// Handle container recreation if needed
	if needsRecreation && server.ContainerID != "" && s.docker != nil {
		serverConfig, err := s.store.GetServerConfig(ctx, server.ID)
		if err != nil {
			s.log.Error("Failed to get server config: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get server configuration"))
		}

		result, err := s.docker.RecreateContainer(ctx, server.ContainerID, server, serverConfig)
		if err != nil {
			s.log.Error("Failed to recreate container for proxy change: %v", err)
			if result != nil && result.NewContainerID != "" {
				server.ContainerID = result.NewContainerID
				server.Status = storage.StatusError
			} else {
				server.Status = storage.StatusError
				server.ContainerID = ""
			}
		} else {
			server.ContainerID = result.NewContainerID
			if result.WasRunning {
				server.Status = storage.StatusRunning
			} else {
				server.Status = storage.StatusStopped
			}
		}

		s.log.Info("Container recreated for server %s (proxy: %q -> %q, listener: %s -> %s)",
			server.Name, oldProxyHostname, hostname, oldProxyListenerID, listenerID)
	}

	// Save server to database
	if err := s.store.UpdateServer(ctx, server); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update server"))
	}

	// Add/update new route if proxy is enabled
	if hostname != "" && s.proxyManager != nil {
		if err := s.proxyManager.UpdateServerRoute(server); err != nil {
			s.log.Error("Failed to update proxy route: %v", err)
		}
	}

	return connect.NewResponse(&v1.UpdateServerRoutingResponse{
		Status:          "Routing updated successfully",
		Hostname:        hostname,
		ProxyListenerId: listenerID,
	}), nil
}
