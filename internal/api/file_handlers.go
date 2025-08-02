package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
)

type FileInfo struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"is_dir"`
	Size     int64       `json:"size"`
	Modified int64       `json:"modified"`
	Children []FileInfo  `json:"children,omitempty"`
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

	// Clean and validate path
	fullPath := filepath.Join(server.DataPath, path)
	if !strings.HasPrefix(fullPath, server.DataPath) {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	// List directory
	files, err := s.listDirectory(fullPath, server.DataPath)
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

	s.respondJSON(w, http.StatusCreated, map[string]string{
		"message": "File uploaded successfully",
		"path":    filepath.Join(targetPath, header.Filename),
	})
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
		
		fileInfo := FileInfo{
			Name:     entry.Name(),
			Path:     relPath,
			IsDir:    entry.IsDir(),
			Size:     info.Size(),
			Modified: info.ModTime().Unix(),
		}

		files = append(files, fileInfo)
	}

	return files, nil
}