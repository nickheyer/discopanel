package services

import (
	"context"

	"connectrpc.com/connect"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
)

// Compile-time check that ProxyService implements the interface
var _ discopanelv1connect.ProxyServiceHandler = (*ProxyService)(nil)

// ProxyService implements the Proxy service
type ProxyService struct {
	store        *storage.Store
	proxyManager *proxy.Manager
	log          *logger.Logger
}

// NewProxyService creates a new proxy service
func NewProxyService(store *storage.Store, proxyManager *proxy.Manager, log *logger.Logger) *ProxyService {
	return &ProxyService{
		store:        store,
		proxyManager: proxyManager,
		log:          log,
	}
}

// GetProxyRoutes gets proxy routes
func (s *ProxyService) GetProxyRoutes(ctx context.Context, req *connect.Request[v1.GetProxyRoutesRequest]) (*connect.Response[v1.GetProxyRoutesResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetProxyStatus gets proxy status
func (s *ProxyService) GetProxyStatus(ctx context.Context, req *connect.Request[v1.GetProxyStatusRequest]) (*connect.Response[v1.GetProxyStatusResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UpdateProxyConfig updates proxy configuration
func (s *ProxyService) UpdateProxyConfig(ctx context.Context, req *connect.Request[v1.UpdateProxyConfigRequest]) (*connect.Response[v1.UpdateProxyConfigResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetProxyListeners gets proxy listeners
func (s *ProxyService) GetProxyListeners(ctx context.Context, req *connect.Request[v1.GetProxyListenersRequest]) (*connect.Response[v1.GetProxyListenersResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// CreateProxyListener creates a proxy listener
func (s *ProxyService) CreateProxyListener(ctx context.Context, req *connect.Request[v1.CreateProxyListenerRequest]) (*connect.Response[v1.CreateProxyListenerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UpdateProxyListener updates a proxy listener
func (s *ProxyService) UpdateProxyListener(ctx context.Context, req *connect.Request[v1.UpdateProxyListenerRequest]) (*connect.Response[v1.UpdateProxyListenerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// DeleteProxyListener deletes a proxy listener
func (s *ProxyService) DeleteProxyListener(ctx context.Context, req *connect.Request[v1.DeleteProxyListenerRequest]) (*connect.Response[v1.DeleteProxyListenerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetServerRouting gets server routing configuration
func (s *ProxyService) GetServerRouting(ctx context.Context, req *connect.Request[v1.GetServerRoutingRequest]) (*connect.Response[v1.GetServerRoutingResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UpdateServerRouting updates server routing configuration
func (s *ProxyService) UpdateServerRouting(ctx context.Context, req *connect.Request[v1.UpdateServerRoutingRequest]) (*connect.Response[v1.UpdateServerRoutingResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}