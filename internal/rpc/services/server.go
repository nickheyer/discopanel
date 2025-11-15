package services

import (
	"context"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/proxy"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
)

// Compile-time check that ServerService implements the interface
var _ discopanelv1connect.ServerServiceHandler = (*ServerService)(nil)

// ServerService implements the Server service
type ServerService struct {
	store  *storage.Store
	docker *docker.Client
	config *config.Config
	proxy  *proxy.Manager
	log    *logger.Logger
}

// NewServerService creates a new server service
func NewServerService(store *storage.Store, docker *docker.Client, config *config.Config, proxy *proxy.Manager, log *logger.Logger) *ServerService {
	return &ServerService{
		store:  store,
		docker: docker,
		config: config,
		proxy:  proxy,
		log:    log,
	}
}

// ListServers lists all servers
func (s *ServerService) ListServers(ctx context.Context, req *connect.Request[v1.ListServersRequest]) (*connect.Response[v1.ListServersResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetServer gets a specific server
func (s *ServerService) GetServer(ctx context.Context, req *connect.Request[v1.GetServerRequest]) (*connect.Response[v1.GetServerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// CreateServer creates a new server
func (s *ServerService) CreateServer(ctx context.Context, req *connect.Request[v1.CreateServerRequest]) (*connect.Response[v1.CreateServerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UpdateServer updates a server
func (s *ServerService) UpdateServer(ctx context.Context, req *connect.Request[v1.UpdateServerRequest]) (*connect.Response[v1.UpdateServerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// DeleteServer deletes a server
func (s *ServerService) DeleteServer(ctx context.Context, req *connect.Request[v1.DeleteServerRequest]) (*connect.Response[v1.DeleteServerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// StartServer starts a server
func (s *ServerService) StartServer(ctx context.Context, req *connect.Request[v1.StartServerRequest]) (*connect.Response[v1.StartServerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// StopServer stops a server
func (s *ServerService) StopServer(ctx context.Context, req *connect.Request[v1.StopServerRequest]) (*connect.Response[v1.StopServerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// RestartServer restarts a server
func (s *ServerService) RestartServer(ctx context.Context, req *connect.Request[v1.RestartServerRequest]) (*connect.Response[v1.RestartServerResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// SendCommand sends a command to a server
func (s *ServerService) SendCommand(ctx context.Context, req *connect.Request[v1.SendCommandRequest]) (*connect.Response[v1.SendCommandResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetServerLogs gets server logs
func (s *ServerService) GetServerLogs(ctx context.Context, req *connect.Request[v1.GetServerLogsRequest]) (*connect.Response[v1.GetServerLogsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// ClearServerLogs clears server logs
func (s *ServerService) ClearServerLogs(ctx context.Context, req *connect.Request[v1.ClearServerLogsRequest]) (*connect.Response[v1.ClearServerLogsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetNextAvailablePort gets the next available port
func (s *ServerService) GetNextAvailablePort(ctx context.Context, req *connect.Request[v1.GetNextAvailablePortRequest]) (*connect.Response[v1.GetNextAvailablePortResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}
