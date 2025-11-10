package services

import (
	"context"

	"connectrpc.com/connect"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
)

// Compile-time check that ConfigService implements the interface
var _ discopanelv1connect.ConfigServiceHandler = (*ConfigService)(nil)

// ConfigService implements the Config service
type ConfigService struct {
	store  *storage.Store
	config *config.Config
	log    *logger.Logger
}

// NewConfigService creates a new config service
func NewConfigService(store *storage.Store, cfg *config.Config, log *logger.Logger) *ConfigService {
	return &ConfigService{
		store:  store,
		config: cfg,
		log:    log,
	}
}

// GetServerConfig gets server configuration
func (s *ConfigService) GetServerConfig(ctx context.Context, req *connect.Request[v1.GetServerConfigRequest]) (*connect.Response[v1.GetServerConfigResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UpdateServerConfig updates server configuration
func (s *ConfigService) UpdateServerConfig(ctx context.Context, req *connect.Request[v1.UpdateServerConfigRequest]) (*connect.Response[v1.UpdateServerConfigResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetGlobalSettings gets the global settings
func (s *ConfigService) GetGlobalSettings(ctx context.Context, req *connect.Request[v1.GetGlobalSettingsRequest]) (*connect.Response[v1.GetGlobalSettingsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UpdateGlobalSettings updates the global settings
func (s *ConfigService) UpdateGlobalSettings(ctx context.Context, req *connect.Request[v1.UpdateGlobalSettingsRequest]) (*connect.Response[v1.UpdateGlobalSettingsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}