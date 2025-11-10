package services

import (
	"context"

	"connectrpc.com/connect"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
)

// Compile-time check that ModService implements the interface
var _ discopanelv1connect.ModServiceHandler = (*ModService)(nil)

// ModService implements the Mod service
type ModService struct {
	store  *storage.Store
	docker *docker.Client
	log    *logger.Logger
}

// NewModService creates a new mod service
func NewModService(store *storage.Store, docker *docker.Client, log *logger.Logger) *ModService {
	return &ModService{
		store:  store,
		docker: docker,
		log:    log,
	}
}

// ListMods lists mods for a server
func (s *ModService) ListMods(ctx context.Context, req *connect.Request[v1.ListModsRequest]) (*connect.Response[v1.ListModsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetMod gets a specific mod
func (s *ModService) GetMod(ctx context.Context, req *connect.Request[v1.GetModRequest]) (*connect.Response[v1.GetModResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UploadMod uploads a new mod
func (s *ModService) UploadMod(ctx context.Context, req *connect.Request[v1.UploadModRequest]) (*connect.Response[v1.UploadModResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UpdateMod updates a mod
func (s *ModService) UpdateMod(ctx context.Context, req *connect.Request[v1.UpdateModRequest]) (*connect.Response[v1.UpdateModResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// DeleteMod deletes a mod
func (s *ModService) DeleteMod(ctx context.Context, req *connect.Request[v1.DeleteModRequest]) (*connect.Response[v1.DeleteModResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}