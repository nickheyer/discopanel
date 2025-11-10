package services

import (
	"context"

	"connectrpc.com/connect"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
)

// Compile-time check that MinecraftService implements the interface
var _ discopanelv1connect.MinecraftServiceHandler = (*MinecraftService)(nil)

// MinecraftService implements the Minecraft service
type MinecraftService struct {
	store *storage.Store
	log   *logger.Logger
}

// NewMinecraftService creates a new minecraft service
func NewMinecraftService(store *storage.Store, log *logger.Logger) *MinecraftService {
	return &MinecraftService{
		store: store,
		log:   log,
	}
}

// GetMinecraftVersions gets available Minecraft versions
func (s *MinecraftService) GetMinecraftVersions(ctx context.Context, req *connect.Request[v1.GetMinecraftVersionsRequest]) (*connect.Response[v1.GetMinecraftVersionsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetModLoaders gets available mod loaders
func (s *MinecraftService) GetModLoaders(ctx context.Context, req *connect.Request[v1.GetModLoadersRequest]) (*connect.Response[v1.GetModLoadersResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetDockerImages gets available Docker images
func (s *MinecraftService) GetDockerImages(ctx context.Context, req *connect.Request[v1.GetDockerImagesRequest]) (*connect.Response[v1.GetDockerImagesResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}