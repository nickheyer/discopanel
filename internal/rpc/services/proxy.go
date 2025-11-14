package services

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
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
	proxyManager *proxy.Manager
	config       *config.Config
	log          *logger.Logger
}

// NewProxyService creates a new proxy service
func NewProxyService(store *storage.Store, proxyManager *proxy.Manager, cfg *config.Config, log *logger.Logger) *ProxyService {
	return &ProxyService{
		store:        store,
		proxyManager: proxyManager,
		config:       cfg,
		log:          log,
	}
}

// Helper functions for proxy service

// dbProxyListenerToProto converts a database ProxyListener to proto
func dbProxyListenerToProto(listener *storage.ProxyListener) *v1.ProxyListener {
	if listener == nil {
		return nil
	}

	return &v1.ProxyListener{
		Id:          listener.ID,
		Name:        listener.Name,
		Description: listener.Description,
		Port:        int32(listener.Port),
		Enabled:     listener.Enabled,
		IsDefault:   listener.IsDefault,
		CreatedAt:   timestamppb.New(listener.CreatedAt),
		UpdatedAt:   timestamppb.New(listener.UpdatedAt),
	}
}

// proxyRouteToProto converts a proxy Route to proto
func proxyRouteToProto(route *proxy.Route) *v1.ProxyRoute {
	if route == nil {
		return nil
	}

	return &v1.ProxyRoute{
		ServerId:    route.ServerID,
		Hostname:    route.Hostname,
		BackendHost: route.BackendHost,
		BackendPort: int32(route.BackendPort),
		Active:      route.Active,
	}
}

// GetProxyRoutes gets proxy routes
func (s *ProxyService) GetProxyRoutes(ctx context.Context, req *connect.Request[v1.GetProxyRoutesRequest]) (*connect.Response[v1.GetProxyRoutesResponse], error) {
	if s.proxyManager == nil {
		return nil, connect.NewError(connect.CodeUnavailable, nil)
	}

	routes := s.proxyManager.GetRoutes()

	// Convert routes to proto format
	protoRoutes := make([]*v1.ProxyRoute, 0, len(routes))
	for _, route := range routes {
		protoRoutes = append(protoRoutes, proxyRouteToProto(route))
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
		// Fall back to in-memory config
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

	// Convert listeners to proto
	protoListeners := make([]*v1.ProxyListener, len(listeners))
	listenPorts := make([]int32, len(listeners))
	for i, l := range listeners {
		protoListeners[i] = dbProxyListenerToProto(l)
		listenPorts[i] = int32(l.Port)
	}

	response := &v1.GetProxyStatusResponse{
		Enabled:     proxyConfig.Enabled,
		BaseUrl:     proxyConfig.BaseURL,
		ListenPorts: listenPorts,
		Listeners:   protoListeners,
	}

	// Set primary port if listeners exist
	if len(listenPorts) > 0 {
		response.ListenPort = listenPorts[0]
	}

	// Get proxy manager status
	if s.proxyManager != nil {
		response.Running = s.proxyManager.IsRunning()
		routes := s.proxyManager.GetRoutes()
		response.ActiveRoutes = int32(len(routes))
	} else {
		response.Running = false
		response.ActiveRoutes = 0
	}

	return connect.NewResponse(response), nil
}

// UpdateProxyConfig updates proxy configuration
func (s *ProxyService) UpdateProxyConfig(ctx context.Context, req *connect.Request[v1.UpdateProxyConfigRequest]) (*connect.Response[v1.UpdateProxyConfigResponse], error) {
	// Save to database
	proxyConfig := &storage.ProxyConfig{
		ID:      "default",
		Enabled: req.Msg.Enabled,
		BaseURL: req.Msg.BaseUrl,
	}

	if err := s.store.SaveProxyConfig(ctx, proxyConfig); err != nil {
		s.log.Error("Failed to save proxy configuration: %v", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Update in-memory configuration
	s.config.Proxy.Enabled = req.Msg.Enabled
	s.config.Proxy.BaseURL = req.Msg.BaseUrl

	s.log.Info("Proxy configuration saved to database: enabled=%v, base_url=%v", req.Msg.Enabled, req.Msg.BaseUrl)

	// Return updated status using GetProxyStatus
	statusReq := connect.NewRequest(&v1.GetProxyStatusRequest{})
	statusResp, err := s.GetProxyStatus(ctx, statusReq)
	if err != nil {
		return nil, err
	}

	// Convert GetProxyStatusResponse to UpdateProxyConfigResponse
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
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Get all servers to count usage
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		s.log.Error("Failed to list servers for listener counts: %v", err)
		servers = []*storage.Server{} // Continue with empty list
	}

	// Build response with server counts
	response := make([]*v1.ProxyListenerWithCount, len(listeners))
	for i, listener := range listeners {
		// Count servers using this listener
		count := 0
		for _, server := range servers {
			if server.ProxyListenerID == listener.ID {
				count++
			}
		}

		response[i] = &v1.ProxyListenerWithCount{
			Listener:    dbProxyListenerToProto(listener),
			ServerCount: int32(count),
		}
	}

	return connect.NewResponse(&v1.GetProxyListenersResponse{
		Listeners: response,
	}), nil
}

// CreateProxyListener creates a proxy listener
func (s *ProxyService) CreateProxyListener(ctx context.Context, req *connect.Request[v1.CreateProxyListenerRequest]) (*connect.Response[v1.CreateProxyListenerResponse], error) {
	// Validate port
	if req.Msg.Port < 1 || req.Msg.Port > 65535 {
		return nil, connect.NewError(connect.CodeInvalidArgument, nil)
	}

	// Check if port is already in use by another listener
	existing, _ := s.store.GetProxyListenerByPort(ctx, int(req.Msg.Port))
	if existing != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, nil)
	}

	// Check if port is used by a non-proxied server
	servers, _ := s.store.ListServers(ctx)
	for _, server := range servers {
		if server.ProxyHostname == "" && server.Port == int(req.Msg.Port) {
			return nil, connect.NewError(connect.CodeAlreadyExists, nil)
		}
	}

	// Create listener
	listener := &storage.ProxyListener{
		Name:        req.Msg.Name,
		Description: req.Msg.Description,
		Port:        int(req.Msg.Port),
		Enabled:     req.Msg.Enabled,
		IsDefault:   req.Msg.IsDefault,
	}

	if err := s.store.CreateProxyListener(ctx, listener); err != nil {
		s.log.Error("Failed to create proxy listener: %v", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Add the listener to the proxy manager if it's running
	if s.proxyManager != nil {
		if err := s.proxyManager.AddListener(listener); err != nil {
			s.log.Error("Failed to add listener to proxy manager: %v", err)
			// Not critical - proxy can be restarted to pick it up
		}
	}

	return connect.NewResponse(&v1.CreateProxyListenerResponse{
		Listener: dbProxyListenerToProto(listener),
	}), nil
}

// UpdateProxyListener updates a proxy listener
func (s *ProxyService) UpdateProxyListener(ctx context.Context, req *connect.Request[v1.UpdateProxyListenerRequest]) (*connect.Response[v1.UpdateProxyListenerResponse], error) {
	// Get existing listener
	listener, err := s.store.GetProxyListener(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// Store old port for proxy manager updates
	oldPort := listener.Port

	// Update fields
	listener.Name = req.Msg.Name
	listener.Description = req.Msg.Description
	listener.Enabled = req.Msg.Enabled
	listener.IsDefault = req.Msg.IsDefault

	// Update port if provided and different
	if req.Msg.Port != 0 && req.Msg.Port != int32(oldPort) {
		listener.Port = int(req.Msg.Port)
	}

	// If setting as default, unset other defaults
	if req.Msg.IsDefault {
		listeners, _ := s.store.GetProxyListeners(ctx)
		for _, l := range listeners {
			if l.ID != req.Msg.Id && l.IsDefault {
				l.IsDefault = false
				s.store.UpdateProxyListener(ctx, l)
			}
		}
	}

	if err := s.store.UpdateProxyListener(ctx, listener); err != nil {
		s.log.Error("Failed to update proxy listener: %v", err)
		return nil, connect.NewError(connect.CodeInternal, err)
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
		Listener: dbProxyListenerToProto(listener),
	}), nil
}

// DeleteProxyListener deletes a proxy listener
func (s *ProxyService) DeleteProxyListener(ctx context.Context, req *connect.Request[v1.DeleteProxyListenerRequest]) (*connect.Response[v1.DeleteProxyListenerResponse], error) {
	// Get the listener first to know which port to remove from proxy manager
	listener, err := s.store.GetProxyListener(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	if err := s.store.DeleteProxyListener(ctx, req.Msg.Id); err != nil {
		if strings.Contains(err.Error(), "servers are using it") {
			return nil, connect.NewError(connect.CodeFailedPrecondition, err)
		}
		s.log.Error("Failed to delete proxy listener: %v", err)
		return nil, connect.NewError(connect.CodeInternal, err)
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
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// Get suggested hostname if not set
	suggestedHostname := ""
	if server.ProxyHostname == "" && s.config.Proxy.BaseURL != "" {
		suggestedHostname = strings.ToLower(strings.ReplaceAll(server.Name, " ", "-")) + "." + s.config.Proxy.BaseURL
	}

	response := &v1.GetServerRoutingResponse{
		ProxyEnabled:      s.config.Proxy.Enabled,
		ProxyHostname:     server.ProxyHostname,
		SuggestedHostname: suggestedHostname,
		BaseUrl:           s.config.Proxy.BaseURL,
		ListenPort:        int32(s.config.Proxy.ListenPort),
	}

	// Check if proxy is enabled and get current route
	if s.proxyManager != nil {
		routes := s.proxyManager.GetRoutes()
		for hostname, route := range routes {
			if route.ServerID == server.ID {
				response.CurrentRoute = &v1.ServerRoute{
					Hostname: hostname,
					Active:   route.Active,
				}
				break
			}
		}
	}

	return connect.NewResponse(response), nil
}

// UpdateServerRouting updates server routing configuration
func (s *ProxyService) UpdateServerRouting(ctx context.Context, req *connect.Request[v1.UpdateServerRoutingRequest]) (*connect.Response[v1.UpdateServerRoutingResponse], error) {
	server, err := s.store.GetServer(ctx, req.Msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	// Validate hostname
	hostname := strings.TrimSpace(strings.ToLower(req.Msg.ProxyHostname))
	if hostname != "" {
		// Basic hostname validation
		if strings.Contains(hostname, " ") || strings.Contains(hostname, "://") {
			return nil, connect.NewError(connect.CodeInvalidArgument, nil)
		}

		// Check for conflicts with other servers
		servers, err := s.store.ListServers(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}

		for _, srv := range servers {
			if srv.ID != server.ID && srv.ProxyHostname == hostname {
				return nil, connect.NewError(connect.CodeAlreadyExists, nil)
			}
		}
	}

	// Update server hostname
	server.ProxyHostname = hostname
	if err := s.store.UpdateServer(ctx, server); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Update proxy route
	if s.proxyManager != nil {
		if err := s.proxyManager.UpdateServerRoute(server); err != nil {
			s.log.Error("Failed to update proxy route: %v", err)
		}
	}

	return connect.NewResponse(&v1.UpdateServerRoutingResponse{
		Status:   "Routing updated successfully",
		Hostname: hostname,
	}), nil
}