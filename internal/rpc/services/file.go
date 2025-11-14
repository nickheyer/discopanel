package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/files"
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
	msg := req.Msg

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Get path parameter, default to current directory
	path := msg.Path
	if path == "" {
		path = "."
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, path)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path"))
	}

	// List directory
	var fileInfos []*v1.FileInfo
	if msg.Tree {
		fileInfos, err = s.listDirectoryTree(fullPath, server.DataPath, 0, 10) // max depth 10
	} else {
		fileInfos, err = s.listDirectory(fullPath, server.DataPath)
	}

	if err != nil {
		s.log.Error("Failed to list files: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list files"))
	}

	return connect.NewResponse(&v1.ListFilesResponse{
		Files: fileInfos,
	}), nil
}

// GetFile gets a file's content
func (s *FileService) GetFile(ctx context.Context, req *connect.Request[v1.GetFileRequest]) (*connect.Response[v1.GetFileResponse], error) {
	msg := req.Msg

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, msg.Path)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path"))
	}

	// Check if file exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to access file"))
	}

	// Don't serve directories
	if info.IsDir() {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is a directory"))
	}

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		s.log.Error("Failed to read file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read file"))
	}

	// Detect MIME type
	mimeType := detectMimeType(fullPath, content)

	return connect.NewResponse(&v1.GetFileResponse{
		Content:  content,
		MimeType: mimeType,
	}), nil
}

// UpdateFile updates a file's content
func (s *FileService) UpdateFile(ctx context.Context, req *connect.Request[v1.UpdateFileRequest]) (*connect.Response[v1.UpdateFileResponse], error) {
	msg := req.Msg

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, msg.Path)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path"))
	}

	// Create directories if needed
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		s.log.Error("Failed to create directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create directory"))
	}

	// Write file
	if err := os.WriteFile(fullPath, msg.Content, 0644); err != nil {
		s.log.Error("Failed to write file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update file"))
	}

	return connect.NewResponse(&v1.UpdateFileResponse{
		Message: "File updated successfully",
		Path:    msg.Path,
	}), nil
}

// DeleteFile deletes a file
func (s *FileService) DeleteFile(ctx context.Context, req *connect.Request[v1.DeleteFileRequest]) (*connect.Response[v1.DeleteFileResponse], error) {
	msg := req.Msg

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, msg.Path)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path"))
	}

	// Don't allow deleting root directory
	if fullPath == server.DataPath {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("cannot delete server root directory"))
	}

	// Check if exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to access file"))
	}

	// Delete file or directory
	if info.IsDir() {
		err = os.RemoveAll(fullPath)
	} else {
		err = os.Remove(fullPath)
	}

	if err != nil {
		s.log.Error("Failed to delete file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete file"))
	}

	return connect.NewResponse(&v1.DeleteFileResponse{}), nil
}

// UploadFile uploads a new file
func (s *FileService) UploadFile(ctx context.Context, req *connect.Request[v1.UploadFileRequest]) (*connect.Response[v1.UploadFileResponse], error) {
	msg := req.Msg

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Validate filename
	if msg.Filename == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("filename cannot be empty"))
	}

	// Get target path, default to current directory
	targetPath := msg.Path
	if targetPath == "" {
		targetPath = "."
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, targetPath)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path"))
	}

	// Create directories if needed
	if err := os.MkdirAll(fullPath, 0755); err != nil {
		s.log.Error("Failed to create directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create directory"))
	}

	// Save file
	filePath := filepath.Join(fullPath, msg.Filename)
	if err := os.WriteFile(filePath, msg.Content, 0644); err != nil {
		s.log.Error("Failed to save file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save file"))
	}

	return connect.NewResponse(&v1.UploadFileResponse{
		Message: "File uploaded successfully",
		Path:    filepath.Join(targetPath, msg.Filename),
	}), nil
}

// RenameFile renames a file
func (s *FileService) RenameFile(ctx context.Context, req *connect.Request[v1.RenameFileRequest]) (*connect.Response[v1.RenameFileResponse], error) {
	msg := req.Msg

	// Validate new name
	if msg.NewName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("new name cannot be empty"))
	}

	// Ensure new name doesn't contain path separators
	if strings.Contains(msg.NewName, "/") || strings.Contains(msg.NewName, "\\") {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name cannot contain path separators"))
	}

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Clean and validate old path
	oldFullPath := filepath.Join(server.DataPath, msg.Path)
	if !strings.HasPrefix(oldFullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path"))
	}

	// Build new path
	dir := filepath.Dir(msg.Path)
	newPath := filepath.Join(dir, msg.NewName)
	newFullPath := filepath.Join(server.DataPath, newPath)

	// Validate new path
	if !strings.HasPrefix(newFullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid new path"))
	}

	// Check if source exists
	if _, err := os.Stat(oldFullPath); err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to access file"))
	}

	// Check if destination already exists
	if _, err := os.Stat(newFullPath); err == nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("a file or folder with that name already exists"))
	}

	// Rename
	if err := os.Rename(oldFullPath, newFullPath); err != nil {
		s.log.Error("Failed to rename file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to rename file"))
	}

	return connect.NewResponse(&v1.RenameFileResponse{
		Message: "File renamed successfully",
		NewPath: newPath,
	}), nil
}

// ExtractArchive extracts an archive
func (s *FileService) ExtractArchive(ctx context.Context, req *connect.Request[v1.ExtractArchiveRequest]) (*connect.Response[v1.ExtractArchiveResponse], error) {
	msg := req.Msg

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("server not found"))
	}

	// Clean and validate archive path
	fullArchivePath := filepath.Join(server.DataPath, msg.Path)
	if !strings.HasPrefix(fullArchivePath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid archive path"))
	}

	// Check if archive exists
	info, err := os.Stat(fullArchivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("archive not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to access archive"))
	}

	// Ensure it's not a directory
	if info.IsDir() {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is a directory, not an archive"))
	}

	// Determine extraction destination (same directory as archive, folder named after archive without extension)
	archiveDir := filepath.Dir(fullArchivePath)
	archiveName := filepath.Base(msg.Path)

	// Remove extension(s) to create folder name
	folderName := strings.TrimSuffix(archiveName, filepath.Ext(archiveName))
	// Handle double extensions like .tar.gz
	if strings.HasSuffix(strings.ToLower(folderName), ".tar") {
		folderName = strings.TrimSuffix(folderName, ".tar")
	}

	destPath := filepath.Join(archiveDir, folderName)

	// Count files before extraction
	fileCount := 0
	countBeforeExtract := func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			fileCount++
		}
		return nil
	}
	if err := filepath.WalkDir(destPath, countBeforeExtract); err != nil && !os.IsNotExist(err) {
		// If directory doesn't exist yet, that's fine
		fileCount = 0
	}
	beforeCount := fileCount

	// Extract the archive
	if err := files.ExtractArchive(ctx, fullArchivePath, destPath); err != nil {
		s.log.Error("Failed to extract archive: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to extract archive: %v", err))
	}

	// Count files after extraction
	fileCount = 0
	countAfterExtract := func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			fileCount++
		}
		return nil
	}
	if err := filepath.WalkDir(destPath, countAfterExtract); err != nil {
		s.log.Error("Failed to count extracted files: %v", err)
		// Don't fail the operation, just log the error
		fileCount = 0
	}
	extractedCount := fileCount - beforeCount

	return connect.NewResponse(&v1.ExtractArchiveResponse{
		Message:        "Archive extracted successfully",
		FilesExtracted: int32(extractedCount),
	}), nil
}

// Helper functions

// detectMimeType detects the MIME type of a file
func detectMimeType(path string, content []byte) string {
	// First try to detect by extension
	ext := filepath.Ext(path)
	if ext != "" {
		mimeType := mime.TypeByExtension(ext)
		if mimeType != "" {
			return mimeType
		}
	}

	// Fall back to content detection
	if len(content) == 0 {
		return "application/octet-stream"
	}

	// Use http.DetectContentType for content-based detection
	mimeType := http.DetectContentType(content)
	return mimeType
}

// isTextFile checks if a file is a text file
func isTextFile(path string) bool {
	// Read first 512 bytes to detect content type
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	// Read up to 512 bytes
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false
	}

	if n == 0 {
		// Empty files are considered text
		return true
	}

	// Check for null bytes (binary indicator)
	if bytes.Contains(buffer[:n], []byte{0}) {
		return false
	}

	// Check if it's valid UTF-8 with printable characters
	for i := range n {
		b := buffer[i]
		// Allow printable ASCII, tabs, newlines, carriage returns
		if b < 32 && b != 9 && b != 10 && b != 13 {
			return false
		}
		// Reject high control characters
		if b == 127 {
			return false
		}
	}

	return true
}

// listDirectory lists files in a directory (non-recursive)
func (s *FileService) listDirectory(path, basePath string) ([]*v1.FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	fileInfos := make([]*v1.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		relPath, _ := filepath.Rel(basePath, filepath.Join(path, entry.Name()))
		fullPath := filepath.Join(path, entry.Name())

		fileInfo := &v1.FileInfo{
			Name:       entry.Name(),
			Path:       relPath,
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
			Modified:   info.ModTime().Unix(),
			IsEditable: !entry.IsDir() && isTextFile(fullPath),
		}

		fileInfos = append(fileInfos, fileInfo)
	}

	return fileInfos, nil
}

// listDirectoryTree lists files in a directory tree (recursive)
func (s *FileService) listDirectoryTree(path, basePath string, depth, maxDepth int) ([]*v1.FileInfo, error) {
	if depth > maxDepth {
		return nil, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	fileInfos := make([]*v1.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		relPath, _ := filepath.Rel(basePath, filepath.Join(path, entry.Name()))
		fullPath := filepath.Join(path, entry.Name())

		fileInfo := &v1.FileInfo{
			Name:       entry.Name(),
			Path:       relPath,
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
			Modified:   info.ModTime().Unix(),
			IsEditable: !entry.IsDir() && isTextFile(fullPath),
		}

		// If it's a directory and we haven't reached max depth, get children
		if entry.IsDir() && depth < maxDepth {
			childPath := filepath.Join(path, entry.Name())
			children, err := s.listDirectoryTree(childPath, basePath, depth+1, maxDepth)
			if err == nil {
				fileInfo.Children = children
			}
		}

		fileInfos = append(fileInfos, fileInfo)
	}

	return fileInfos, nil
}