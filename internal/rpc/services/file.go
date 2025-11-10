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

// Compile-time check that FileService implements the interface
var _ discopanelv1connect.FileServiceHandler = (*FileService)(nil)

// FileService implements the File service
type FileService struct {
	store  *storage.Store
	docker *docker.Client
	log    *logger.Logger
}

// NewFileService creates a new file service
func NewFileService(store *storage.Store, docker *docker.Client, log *logger.Logger) *FileService {
	return &FileService{
		store:  store,
		docker: docker,
		log:    log,
	}
}

// ListFiles lists files in a directory
func (s *FileService) ListFiles(ctx context.Context, req *connect.Request[v1.ListFilesRequest]) (*connect.Response[v1.ListFilesResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// GetFile gets a file's content
func (s *FileService) GetFile(ctx context.Context, req *connect.Request[v1.GetFileRequest]) (*connect.Response[v1.GetFileResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UpdateFile updates a file's content
func (s *FileService) UpdateFile(ctx context.Context, req *connect.Request[v1.UpdateFileRequest]) (*connect.Response[v1.UpdateFileResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// DeleteFile deletes a file
func (s *FileService) DeleteFile(ctx context.Context, req *connect.Request[v1.DeleteFileRequest]) (*connect.Response[v1.DeleteFileResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// UploadFile uploads a new file
func (s *FileService) UploadFile(ctx context.Context, req *connect.Request[v1.UploadFileRequest]) (*connect.Response[v1.UploadFileResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// RenameFile renames a file
func (s *FileService) RenameFile(ctx context.Context, req *connect.Request[v1.RenameFileRequest]) (*connect.Response[v1.RenameFileResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

// ExtractArchive extracts an archive
func (s *FileService) ExtractArchive(ctx context.Context, req *connect.Request[v1.ExtractArchiveRequest]) (*connect.Response[v1.ExtractArchiveResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}