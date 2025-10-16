package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
)

type FileInfo struct {
	Name       string     `json:"name"`
	Path       string     `json:"path"`
	IsDir      bool       `json:"is_dir"`
	Size       int64      `json:"size"`
	Modified   int64      `json:"modified"`
	IsEditable bool       `json:"is_editable"`
	Children   []FileInfo `json:"children,omitempty"`
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Get server
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Get path parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "."
	}

	// Check if tree view is requested
	tree := r.URL.Query().Get("tree") == "true"

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, path)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	// List directory
	var files []FileInfo
	if tree {
		files, err = s.listDirectoryTree(fullPath, server.DataPath, 0, 10) // max depth 10
	} else {
		files, err = s.listDirectory(fullPath, server.DataPath)
	}

	if err != nil {
		s.log.Error("Failed to list files: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to list files")
		return
	}

	s.respondJSON(w, http.StatusOK, files)
}

func (s *Server) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Get server
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Parse multipart form
	err = r.ParseMultipartForm(100 << 20) // 100 MB max
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Failed to parse upload")
		return
	}

	// Get target path
	targetPath := r.FormValue("path")
	if targetPath == "" {
		targetPath = "."
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, targetPath)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	// Get file
	file, header, err := r.FormFile("file")
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "No file provided")
		return
	}
	defer file.Close()

	// Create directories if needed
	if err := os.MkdirAll(fullPath, 0755); err != nil {
		s.log.Error("Failed to create directory: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create directory")
		return
	}

	// Save file
	filePath := filepath.Join(fullPath, header.Filename)
	dst, err := os.Create(filePath)
	if err != nil {
		s.log.Error("Failed to create file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		s.log.Error("Failed to write file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to save file")
		return
	}

	// If uploaded file is a .mrpack (Modrinth pack), try extracting it
	lower := strings.ToLower(header.Filename)
	if strings.HasSuffix(lower, ".mrpack") || strings.HasSuffix(lower, ".zip") {
		// Try to extract into the target directory (create a subdir without extension)
		dirName := strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename))
		extractDir := filepath.Join(fullPath, dirName)
		if err := os.MkdirAll(extractDir, 0755); err == nil {
			if err := extractZip(filePath, extractDir); err != nil {
				s.log.Error("Failed to extract archive: %v", err)
			} else {
				// Optionally remove original archive after extraction
				_ = os.Remove(filePath)
				filePath = filepath.Join(targetPath, dirName)
			}
		}
	}

	s.respondJSON(w, http.StatusCreated, map[string]string{
		"message": "File uploaded successfully",
		"path":    filePath,
	})
}

// extractZip extracts a zip archive at src into dest directory
func extractZip(src, dest string) error {
	r, err := os.Open(src)
	if err != nil {
		return err
	}
	defer r.Close()

	// stat not needed

	// Use archive/zip via reading from file
	// We need to reopen with zip Reader using file path
	// Simpler: use zip.OpenReader
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		fpath := filepath.Join(dest, f.Name)

		// Prevent ZipSlip vulnerability
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, f.Mode())
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]
	filePath := vars["path"]

	// Get server
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, filePath)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	// Check if file exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.respondError(w, http.StatusNotFound, "File not found")
		} else {
			s.respondError(w, http.StatusInternalServerError, "Failed to access file")
		}
		return
	}

	// Don't serve directories
	if info.IsDir() {
		s.respondError(w, http.StatusBadRequest, "Path is a directory")
		return
	}

	// Serve file
	http.ServeFile(w, r, fullPath)
}

func (s *Server) handleUpdateFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]
	filePath := vars["path"]

	// Get server
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, filePath)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	// Read request body
	content, err := io.ReadAll(r.Body)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	// Create directories if needed
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		s.log.Error("Failed to create directory: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create directory")
		return
	}

	// Write file
	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		s.log.Error("Failed to write file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to update file")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{
		"message": "File updated successfully",
		"path":    filePath,
	})
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]
	filePath := vars["path"]

	// Get server
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, filePath)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	// Don't allow deleting root directory
	if fullPath == server.DataPath {
		s.respondError(w, http.StatusBadRequest, "Cannot delete server root directory")
		return
	}

	// Check if exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.respondError(w, http.StatusNotFound, "File not found")
		} else {
			s.respondError(w, http.StatusInternalServerError, "Failed to access file")
		}
		return
	}

	// Delete file or directory
	if info.IsDir() {
		err = os.RemoveAll(fullPath)
	} else {
		err = os.Remove(fullPath)
	}

	if err != nil {
		s.log.Error("Failed to delete file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to delete file")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRenameFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]
	filePath := vars["path"]

	// Parse request body
	var req struct {
		NewName string `json:"new_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate new name
	if req.NewName == "" {
		s.respondError(w, http.StatusBadRequest, "New name cannot be empty")
		return
	}

	// Ensure new name doesn't contain path separators
	if strings.Contains(req.NewName, "/") || strings.Contains(req.NewName, "\\") {
		s.respondError(w, http.StatusBadRequest, "Name cannot contain path separators")
		return
	}

	// Get server
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Clean and validate old path
	oldFullPath := filepath.Join(server.DataPath, filePath)
	if !strings.HasPrefix(oldFullPath, server.DataPath) {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	// Build new path
	dir := filepath.Dir(filePath)
	newPath := filepath.Join(dir, req.NewName)
	newFullPath := filepath.Join(server.DataPath, newPath)

	// Validate new path
	if !strings.HasPrefix(newFullPath, server.DataPath) {
		s.respondError(w, http.StatusBadRequest, "Invalid new path")
		return
	}

	// Check if source exists
	if _, err := os.Stat(oldFullPath); err != nil {
		if os.IsNotExist(err) {
			s.respondError(w, http.StatusNotFound, "File not found")
		} else {
			s.respondError(w, http.StatusInternalServerError, "Failed to access file")
		}
		return
	}

	// Check if destination already exists
	if _, err := os.Stat(newFullPath); err == nil {
		s.respondError(w, http.StatusConflict, "A file or folder with that name already exists")
		return
	}

	// Rename
	if err := os.Rename(oldFullPath, newFullPath); err != nil {
		s.log.Error("Failed to rename file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to rename file")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{
		"message":  "File renamed successfully",
		"old_path": filePath,
		"new_path": newPath,
	})
}

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
	for i := 0; i < n; i++ {
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

func (s *Server) listDirectory(path, basePath string) ([]FileInfo, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		relPath, _ := filepath.Rel(basePath, filepath.Join(path, entry.Name()))
		fullPath := filepath.Join(path, entry.Name())

		fileInfo := FileInfo{
			Name:       entry.Name(),
			Path:       relPath,
			IsDir:      entry.IsDir(),
			Size:       info.Size(),
			Modified:   info.ModTime().Unix(),
			IsEditable: !entry.IsDir() && isTextFile(fullPath),
		}

		files = append(files, fileInfo)
	}

	return files, nil
}

func (s *Server) listDirectoryTree(path, basePath string, depth, maxDepth int) ([]FileInfo, error) {
	if depth > maxDepth {
		return nil, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		relPath, _ := filepath.Rel(basePath, filepath.Join(path, entry.Name()))
		fullPath := filepath.Join(path, entry.Name())

		fileInfo := FileInfo{
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

		files = append(files, fileInfo)
	}

	return files, nil
}
