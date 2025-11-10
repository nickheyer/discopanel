package services

import (
	"context"

	"connectrpc.com/connect"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
)

// Compile-time check that ModpackService implements the interface
var _ discopanelv1connect.ModpackServiceHandler = (*ModpackService)(nil)

// ModpackService implements the Modpack service
type ModpackService struct {
	store *storage.Store
	log   *logger.Logger
}

// NewModpackService creates a new modpack service
func NewModpackService(store *storage.Store, log *logger.Logger) *ModpackService {
	return &ModpackService{
		store: store,
		log:   log,
	}
}

// SearchModpacks searches for modpacks
func (s *ModpackService) SearchModpacks(ctx context.Context, req *connect.Request[v1.SearchModpacksRequest]) (*connect.Response[v1.SearchModpacksResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetModpack gets a specific modpack
func (s *ModpackService) GetModpack(ctx context.Context, req *connect.Request[v1.GetModpackRequest]) (*connect.Response[v1.GetModpackResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetModpackConfig gets modpack configuration
func (s *ModpackService) GetModpackConfig(ctx context.Context, req *connect.Request[v1.GetModpackConfigRequest]) (*connect.Response[v1.GetModpackConfigResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetModpackFiles gets modpack files
func (s *ModpackService) GetModpackFiles(ctx context.Context, req *connect.Request[v1.GetModpackFilesRequest]) (*connect.Response[v1.GetModpackFilesResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetModpackVersions gets modpack versions
func (s *ModpackService) GetModpackVersions(ctx context.Context, req *connect.Request[v1.GetModpackVersionsRequest]) (*connect.Response[v1.GetModpackVersionsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// SyncModpacks syncs modpacks
func (s *ModpackService) SyncModpacks(ctx context.Context, req *connect.Request[v1.SyncModpacksRequest]) (*connect.Response[v1.SyncModpacksResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UploadModpack uploads a modpack
func (s *ModpackService) UploadModpack(ctx context.Context, req *connect.Request[v1.UploadModpackRequest]) (*connect.Response[v1.UploadModpackResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// DeleteModpack deletes a modpack
func (s *ModpackService) DeleteModpack(ctx context.Context, req *connect.Request[v1.DeleteModpackRequest]) (*connect.Response[v1.DeleteModpackResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// ToggleFavorite toggles modpack favorite status
func (s *ModpackService) ToggleFavorite(ctx context.Context, req *connect.Request[v1.ToggleFavoriteRequest]) (*connect.Response[v1.ToggleFavoriteResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// ListFavorites lists favorite modpacks
func (s *ModpackService) ListFavorites(ctx context.Context, req *connect.Request[v1.ListFavoritesRequest]) (*connect.Response[v1.ListFavoritesResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetIndexerStatus gets indexer status
func (s *ModpackService) GetIndexerStatus(ctx context.Context, req *connect.Request[v1.GetIndexerStatusRequest]) (*connect.Response[v1.GetIndexerStatusResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// SyncModpackFiles syncs modpack files
func (s *ModpackService) SyncModpackFiles(ctx context.Context, req *connect.Request[v1.SyncModpackFilesRequest]) (*connect.Response[v1.SyncModpackFilesResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}