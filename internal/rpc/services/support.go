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

// Compile-time check that SupportService implements the interface
var _ discopanelv1connect.SupportServiceHandler = (*SupportService)(nil)

// SupportService implements the Support service
type SupportService struct {
	store  *storage.Store
	docker *docker.Client
	log    *logger.Logger
}

// NewSupportService creates a new support service
func NewSupportService(store *storage.Store, docker *docker.Client, log *logger.Logger) *SupportService {
	return &SupportService{
		store:  store,
		docker: docker,
		log:    log,
	}
}

// GenerateSupportBundle generates a support bundle
func (s *SupportService) GenerateSupportBundle(ctx context.Context, req *connect.Request[v1.GenerateSupportBundleRequest]) (*connect.Response[v1.GenerateSupportBundleResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// DownloadSupportBundle downloads a support bundle
func (s *SupportService) DownloadSupportBundle(ctx context.Context, req *connect.Request[v1.DownloadSupportBundleRequest]) (*connect.Response[v1.DownloadSupportBundleResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}