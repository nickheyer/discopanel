package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/pkg/files"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"github.com/nickheyer/discopanel/pkg/upload"
)

// Compile-time check that FileService implements the interface
var _ discopanelv1connect.FileServiceHandler = (*FileService)(nil)

// FileService implements the File service
type FileService struct {
	store         *storage.Store
	docker        *docker.Client
	log           *logger.Logger
	uploadManager *upload.Manager
}

// NewFileService creates a new file service
func NewFileService(store *storage.Store, docker *docker.Client, uploadManager *upload.Manager, log *logger.Logger) *FileService {
	return &FileService{
		store:         store,
		docker:        docker,
		log:           log,
		uploadManager: uploadManager,
	}
}

// ListFiles lists files in a directory
func (s *FileService) ListFiles(ctx context.Context, req *connect.Request[v1.ListFilesRequest]) (*connect.Response[v1.ListFilesResponse], error) {
	msg := req.Msg

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Get path parameter
	path := msg.Path
	if path == "" {
		path = "."
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, path)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid path"))
	}

	// List directory
	var files []*v1.FileInfo
	if msg.Tree {
		files, err = s.listDirectoryTree(fullPath, server.DataPath, 0, 10) // max depth 10
	} else {
		files, err = s.listDirectory(fullPath, server.DataPath)
	}

	if err != nil {
		s.log.Error("Failed to list files: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to list files"))
	}

	return connect.NewResponse(&v1.ListFilesResponse{
		Files: files,
	}), nil
}

// GetFile gets a file's content
func (s *FileService) GetFile(ctx context.Context, req *connect.Request[v1.GetFileRequest]) (*connect.Response[v1.GetFileResponse], error) {
	msg := req.Msg

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, msg.Path)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid path"))
	}

	// Check if file exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("file not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to access file"))
	}

	// Don't serve directories
	if info.IsDir() {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path is a directory"))
	}

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		s.log.Error("Failed to read file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to read file"))
	}

	// Detect MIME type
	mimeType := http.DetectContentType(content)

	return connect.NewResponse(&v1.GetFileResponse{
		Content:  content,
		MimeType: mimeType,
	}), nil
}

// SaveUploadedFile saves a file from a completed chunked upload session
func (s *FileService) SaveUploadedFile(ctx context.Context, req *connect.Request[v1.SaveUploadedFileRequest]) (*connect.Response[v1.SaveUploadedFileResponse], error) {
	msg := req.Msg

	// Validate upload session
	if msg.UploadSessionId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("upload_session_id is required"))
	}

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Get temp file path and original filename from upload manager
	tempPath, originalFilename, err := s.uploadManager.GetTempPath(msg.UploadSessionId)
	if err != nil {
		s.log.Error("Failed to get upload session: %v", err)
		return nil, connect.NewError(connect.CodeNotFound, errors.New("upload session not found or not completed"))
	}

	// Determine target filename
	targetFilename := msg.Filename
	if targetFilename == "" {
		targetFilename = originalFilename
	}

	// Validate filename doesn't contain path separators
	if strings.Contains(targetFilename, "/") || strings.Contains(targetFilename, "\\") {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("filename cannot contain path separators"))
	}

	// Get target path
	targetPath := msg.DestinationPath
	if targetPath == "" {
		targetPath = "."
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, targetPath)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid path"))
	}

	// Create directories if needed
	if err := os.MkdirAll(fullPath, 0755); err != nil {
		s.log.Error("Failed to create directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create directory"))
	}

	// Move file from temp location to destination
	destFilePath := filepath.Join(fullPath, targetFilename)
	if err := os.Rename(tempPath, destFilePath); err != nil {
		if err := files.CopyFile(tempPath, destFilePath); err != nil {
			s.log.Error("Failed to move file: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to save file"))
		}
		os.Remove(tempPath)
	}

	// Cleanup the upload session
	s.uploadManager.CleanupSession(msg.UploadSessionId)

	return connect.NewResponse(&v1.SaveUploadedFileResponse{
		Message: "File uploaded successfully",
		Path:    filepath.Join(targetPath, targetFilename),
	}), nil
}

// UpdateFile updates a file's content
func (s *FileService) UpdateFile(ctx context.Context, req *connect.Request[v1.UpdateFileRequest]) (*connect.Response[v1.UpdateFileResponse], error) {
	msg := req.Msg

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, msg.Path)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid path"))
	}

	// Create directories if needed
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		s.log.Error("Failed to create directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create directory"))
	}

	// Write file
	if err := os.WriteFile(fullPath, msg.Content, 0644); err != nil {
		s.log.Error("Failed to write file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to update file"))
	}

	return connect.NewResponse(&v1.UpdateFileResponse{
		Message: "File updated successfully",
		Path:    msg.Path,
	}), nil
}

// DeleteFile deletes a file or multiple files (bulk)
func (s *FileService) DeleteFile(ctx context.Context, req *connect.Request[v1.DeleteFileRequest]) (*connect.Response[v1.DeleteFileResponse], error) {
	msg := req.Msg

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Build list of paths to delete: prefer bulk paths, fall back to single path
	paths := msg.Paths
	if len(paths) == 0 && msg.Path != "" {
		paths = []string{msg.Path}
	}
	if len(paths) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no paths specified"))
	}

	for _, p := range paths {
		fullPath := filepath.Join(server.DataPath, p)
		if !strings.HasPrefix(fullPath, server.DataPath) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path: %s", p))
		}
		if fullPath == server.DataPath {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cannot delete server root directory"))
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Skip already-deleted files in bulk
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to access %s", p))
		}

		if info.IsDir() {
			err = os.RemoveAll(fullPath)
		} else {
			err = os.Remove(fullPath)
		}
		if err != nil {
			s.log.Error("Failed to delete %s: %v", p, err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete %s", p))
		}
	}

	return connect.NewResponse(&v1.DeleteFileResponse{}), nil
}

// RenameFile renames a file
func (s *FileService) RenameFile(ctx context.Context, req *connect.Request[v1.RenameFileRequest]) (*connect.Response[v1.RenameFileResponse], error) {
	msg := req.Msg

	// Validate new name
	if msg.NewName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("new name cannot be empty"))
	}

	// Ensure new name doesn't contain path separators
	if strings.Contains(msg.NewName, "/") || strings.Contains(msg.NewName, "\\") {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name cannot contain path separators"))
	}

	// Get server
	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Clean and validate old path
	oldFullPath := filepath.Join(server.DataPath, msg.Path)
	if !strings.HasPrefix(oldFullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid path"))
	}

	// Build new path
	dir := filepath.Dir(msg.Path)
	newPath := filepath.Join(dir, msg.NewName)
	newFullPath := filepath.Join(server.DataPath, newPath)

	// Validate new path
	if !strings.HasPrefix(newFullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid new path"))
	}

	// Check if source exists
	if _, err := os.Stat(oldFullPath); err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("file not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to access file"))
	}

	// Check if destination already exists
	if _, err := os.Stat(newFullPath); err == nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("a file or folder with that name already exists"))
	}

	// Rename
	if err := os.Rename(oldFullPath, newFullPath); err != nil {
		s.log.Error("Failed to rename file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to rename file"))
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
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	// Clean and validate archive path
	fullArchivePath := filepath.Join(server.DataPath, msg.Path)
	if !strings.HasPrefix(fullArchivePath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid archive path"))
	}

	// Check if archive exists
	info, err := os.Stat(fullArchivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("archive not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to access archive"))
	}

	// Ensure it's not a directory
	if info.IsDir() {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("path is a directory, not an archive"))
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

	// Extract the archive
	if err := files.ExtractArchive(ctx, fullArchivePath, destPath); err != nil {
		s.log.Error("Failed to extract archive: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to extract archive: %w", err))
	}

	// Count extracted files
	filesExtracted := 0
	err = filepath.Walk(destPath, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			filesExtracted++
		}
		return nil
	})
	if err != nil {
		s.log.Warn("Failed to count extracted files: %v", err)
	}

	return connect.NewResponse(&v1.ExtractArchiveResponse{
		Message:        "Archive extracted successfully",
		FilesExtracted: int32(filesExtracted),
	}), nil
}

// CreateFolder creates a new directory
func (s *FileService) CreateFolder(ctx context.Context, req *connect.Request[v1.CreateFolderRequest]) (*connect.Response[v1.CreateFolderResponse], error) {
	msg := req.Msg

	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	fullPath := filepath.Join(server.DataPath, msg.Path)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid path"))
	}

	if err := os.MkdirAll(fullPath, 0755); err != nil {
		s.log.Error("Failed to create folder: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create folder"))
	}

	return connect.NewResponse(&v1.CreateFolderResponse{
		Message: "Folder created successfully",
	}), nil
}

// MoveFile moves a file or directory to a new location
func (s *FileService) MoveFile(ctx context.Context, req *connect.Request[v1.MoveFileRequest]) (*connect.Response[v1.MoveFileResponse], error) {
	msg := req.Msg

	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	srcFull := filepath.Join(server.DataPath, msg.SourcePath)
	dstFull := filepath.Join(server.DataPath, msg.DestinationPath)

	if !strings.HasPrefix(srcFull, server.DataPath) || !strings.HasPrefix(dstFull, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid path"))
	}

	if _, err := os.Stat(srcFull); err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("source not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to access source"))
	}

	// Ensure destination parent exists
	if err := os.MkdirAll(filepath.Dir(dstFull), 0755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create destination directory"))
	}

	// Try rename first, fall back to copy+delete for cross-device moves
	if err := os.Rename(srcFull, dstFull); err != nil {
		srcInfo, statErr := os.Stat(srcFull)
		if statErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to move file"))
		}
		if srcInfo.IsDir() {
			if copyErr := files.CopyDir(srcFull, dstFull); copyErr != nil {
				s.log.Error("Failed to copy dir for move: %v", copyErr)
				return nil, connect.NewError(connect.CodeInternal, errors.New("failed to move directory"))
			}
		} else {
			if copyErr := files.CopyFile(srcFull, dstFull); copyErr != nil {
				s.log.Error("Failed to copy file for move: %v", copyErr)
				return nil, connect.NewError(connect.CodeInternal, errors.New("failed to move file"))
			}
		}
		os.RemoveAll(srcFull)
	}

	return connect.NewResponse(&v1.MoveFileResponse{
		Message: "File moved successfully",
	}), nil
}

// CopyFile copies a file or directory
func (s *FileService) CopyFile(ctx context.Context, req *connect.Request[v1.CopyFileRequest]) (*connect.Response[v1.CopyFileResponse], error) {
	msg := req.Msg

	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	srcFull := filepath.Join(server.DataPath, msg.SourcePath)
	dstFull := filepath.Join(server.DataPath, msg.DestinationPath)

	if !strings.HasPrefix(srcFull, server.DataPath) || !strings.HasPrefix(dstFull, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid path"))
	}

	srcInfo, err := os.Stat(srcFull)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("source not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to access source"))
	}

	// If src and dst are the same, generate a "copy" name to avoid truncation
	if srcFull == dstFull {
		dstFull = uniqueCopyPath(dstFull, srcInfo.IsDir())
	}

	if err := os.MkdirAll(filepath.Dir(dstFull), 0755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create destination directory"))
	}

	if srcInfo.IsDir() {
		if err := files.CopyDir(srcFull, dstFull); err != nil {
			s.log.Error("Failed to copy directory: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to copy directory"))
		}
	} else {
		if err := files.CopyFile(srcFull, dstFull); err != nil {
			s.log.Error("Failed to copy file: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to copy file"))
		}
	}

	return connect.NewResponse(&v1.CopyFileResponse{
		Message: "File copied successfully",
	}), nil
}

// CreateArchive creates a zip archive from selected paths
func (s *FileService) CreateArchive(ctx context.Context, req *connect.Request[v1.CreateArchiveRequest]) (*connect.Response[v1.CreateArchiveResponse], error) {
	msg := req.Msg

	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	if len(msg.Paths) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no paths specified"))
	}

	// Validate all paths
	for _, p := range msg.Paths {
		fullPath := filepath.Join(server.DataPath, p)
		if !strings.HasPrefix(fullPath, server.DataPath) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path: %s", p))
		}
	}

	// Determine archive name and destination
	archiveName := msg.ArchiveName
	if archiveName == "" {
		archiveName = fmt.Sprintf("archive_%s.zip", time.Now().Format("20060102_150405"))
	}
	if !strings.HasSuffix(archiveName, ".zip") {
		archiveName += ".zip"
	}

	destDir := msg.DestinationPath
	if destDir == "" {
		destDir = "."
	}
	destFull := filepath.Join(server.DataPath, destDir, archiveName)
	if !strings.HasPrefix(destFull, server.DataPath) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid destination path"))
	}

	count, err := files.CreateZipArchive(msg.Paths, server.DataPath, destFull)
	if err != nil {
		s.log.Error("Failed to create archive: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create archive"))
	}

	archivePath, _ := filepath.Rel(server.DataPath, destFull)
	return connect.NewResponse(&v1.CreateArchiveResponse{
		Message:       "Archive created successfully",
		ArchivePath:   archivePath,
		FilesArchived: int32(count),
	}), nil
}

// DownloadArchive creates a zip in memory and returns the bytes
func (s *FileService) DownloadArchive(ctx context.Context, req *connect.Request[v1.DownloadArchiveRequest]) (*connect.Response[v1.DownloadArchiveResponse], error) {
	msg := req.Msg

	server, err := s.store.GetServer(ctx, msg.ServerId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("server not found"))
	}

	if len(msg.Paths) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no paths specified"))
	}

	for _, p := range msg.Paths {
		fullPath := filepath.Join(server.DataPath, p)
		if !strings.HasPrefix(fullPath, server.DataPath) {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid path: %s", p))
		}
	}

	var buf bytes.Buffer
	_, err = files.CreateZipToWriter(msg.Paths, server.DataPath, &buf)
	if err != nil {
		s.log.Error("Failed to create download archive: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to create archive"))
	}

	filename := "download.zip"
	if len(msg.Paths) == 1 {
		base := filepath.Base(msg.Paths[0])
		filename = strings.TrimSuffix(base, filepath.Ext(base)) + ".zip"
	}

	return connect.NewResponse(&v1.DownloadArchiveResponse{
		Content:  buf.Bytes(),
		Filename: filename,
	}), nil
}

// uniqueCopyPath generates a non-colliding "name (copy).ext" path.
func uniqueCopyPath(fullPath string, isDir bool) string {
	dir := filepath.Dir(fullPath)
	base := filepath.Base(fullPath)

	var stem, ext string
	if isDir {
		stem = base
	} else {
		ext = filepath.Ext(base)
		stem = strings.TrimSuffix(base, ext)
	}

	candidate := filepath.Join(dir, stem+" (copy)"+ext)
	for i := 2; ; i++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s (copy %d)%s", stem, i, ext))
	}
}

// Helper functions
func (s *FileService) listDirectory(path, basePath string) ([]*v1.FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	lsFiles := make([]*v1.FileInfo, 0, len(entries))
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
			IsEditable: !entry.IsDir() && files.IsTextFile(fullPath),
		}

		lsFiles = append(lsFiles, fileInfo)
	}

	return lsFiles, nil
}

func (s *FileService) listDirectoryTree(path, basePath string, depth, maxDepth int) ([]*v1.FileInfo, error) {
	if depth > maxDepth {
		return nil, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	lsFiles := make([]*v1.FileInfo, 0, len(entries))
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
			IsEditable: !entry.IsDir() && files.IsTextFile(fullPath),
		}

		// If it's a directory and we haven't reached max depth, get children
		if entry.IsDir() && depth < maxDepth {
			childPath := filepath.Join(path, entry.Name())
			children, err := s.listDirectoryTree(childPath, basePath, depth+1, maxDepth)
			if err == nil {
				fileInfo.Children = children
			}
		}

		lsFiles = append(lsFiles, fileInfo)
	}

	return lsFiles, nil
}
