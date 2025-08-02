package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nickheyer/discopanel/internal/models"
)


func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	servers, err := s.store.ListServers(ctx)
	if err != nil {
		s.log.Error("Failed to list servers: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to list servers")
		return
	}

	// Update status from Docker
	for _, server := range servers {
		if server.ContainerID != "" {
			status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
			if err == nil {
				server.Status = status
			}
		}
	}

	s.respondJSON(w, http.StatusOK, servers)
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req struct {
		Name        string           `json:"name"`
		Description string           `json:"description"`
		ModLoader   models.ModLoader `json:"mod_loader"`
		MCVersion   string           `json:"mc_version"`
		Port        int              `json:"port"`
		MaxPlayers  int              `json:"max_players"`
		Memory      int              `json:"memory"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	if req.Name == "" || req.MCVersion == "" {
		s.respondError(w, http.StatusBadRequest, "Name and MC version are required")
		return
	}

	// Check if port is already in use
	existing, err := s.store.GetServerByPort(ctx, req.Port)
	if err != nil {
		s.log.Error("Failed to check port: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to check port availability")
		return
	}
	if existing != nil {
		s.respondError(w, http.StatusBadRequest, "Port already in use")
		return
	}

	// Create server object
	server := &models.Server{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		ModLoader:   req.ModLoader,
		MCVersion:   req.MCVersion,
		Status:      models.StatusStopped,
		Port:        req.Port,
		MaxPlayers:  req.MaxPlayers,
		Memory:      req.Memory,
		DataPath:    filepath.Join(s.dataDir, "servers", req.Name),
		JavaVersion: getJavaVersionForMC(req.MCVersion),
	}

	// Set defaults
	if server.MaxPlayers == 0 {
		server.MaxPlayers = 20
	}
	if server.Memory == 0 {
		server.Memory = 2048
	}
	if server.ModLoader == "" {
		server.ModLoader = models.ModLoaderVanilla
	}

	// Create data directory
	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		s.log.Error("Failed to create data directory: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create server directory")
		return
	}

	// Save to database
	if err := s.store.CreateServer(ctx, server); err != nil {
		s.log.Error("Failed to create server: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create server")
		return
	}

	// Create Docker container
	containerID, err := s.docker.CreateContainer(ctx, server)
	if err != nil {
		s.log.Error("Failed to create container: %v", err)
		// Don't fail the whole operation, just log the error
		server.Status = models.StatusError
	} else {
		server.ContainerID = containerID
		// Update server with container ID
		if err := s.store.UpdateServer(ctx, server); err != nil {
			s.log.Error("Failed to update server with container ID: %v", err)
		}
	}

	s.respondJSON(w, http.StatusCreated, server)
}

func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Update status from Docker
	if server.ContainerID != "" {
		status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
		if err == nil {
			server.Status = status
		}
	}

	s.respondJSON(w, http.StatusOK, server)
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		MaxPlayers  int    `json:"max_players"`
		Memory      int    `json:"memory"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update fields
	if req.Name != "" {
		server.Name = req.Name
	}
	if req.Description != "" {
		server.Description = req.Description
	}
	if req.MaxPlayers > 0 {
		server.MaxPlayers = req.MaxPlayers
	}
	if req.Memory > 0 {
		server.Memory = req.Memory
	}

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to update server")
		return
	}

	s.respondJSON(w, http.StatusOK, server)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Stop and remove container
	if server.ContainerID != "" {
		if err := s.docker.StopContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to stop container: %v", err)
		}
		if err := s.docker.RemoveContainer(ctx, server.ContainerID); err != nil {
			s.log.Error("Failed to remove container: %v", err)
		}
	}

	// Delete from database
	if err := s.store.DeleteServer(ctx, id); err != nil {
		s.log.Error("Failed to delete server: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to delete server")
		return
	}

	// Delete data directory
	if err := os.RemoveAll(server.DataPath); err != nil {
		s.log.Error("Failed to delete server data: %v", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStartServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	if server.ContainerID == "" {
		s.respondError(w, http.StatusBadRequest, "Server container not created")
		return
	}

	// Start container
	if err := s.docker.StartContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to start container: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to start server")
		return
	}

	// Update server status
	now := time.Now()
	server.Status = models.StatusStarting
	server.LastStarted = &now

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "starting"})
}

func (s *Server) handleStopServer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	if server.ContainerID == "" {
		s.respondError(w, http.StatusBadRequest, "Server container not created")
		return
	}

	// Stop container
	if err := s.docker.StopContainer(ctx, server.ContainerID); err != nil {
		s.log.Error("Failed to stop container: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to stop server")
		return
	}

	// Update server status
	server.Status = models.StatusStopping

	if err := s.store.UpdateServer(ctx, server); err != nil {
		s.log.Error("Failed to update server status: %v", err)
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
}

func (s *Server) handleRestartServer(w http.ResponseWriter, r *http.Request) {
	// First stop
	s.handleStopServer(w, r)
	if w.Header().Get("Content-Type") != "" {
		// If stop failed, return
		return
	}

	// Wait a bit for clean shutdown
	time.Sleep(2 * time.Second)

	// Then start
	s.handleStartServer(w, r)
}

func (s *Server) handleGetServerLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	id := vars["id"]

	// Parse tail parameter
	tail := 100
	if t := r.URL.Query().Get("tail"); t != "" {
		if parsed, err := strconv.Atoi(t); err == nil {
			tail = parsed
		}
	}

	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	if server.ContainerID == "" {
		s.respondError(w, http.StatusBadRequest, "Server container not created")
		return
	}

	logs, err := s.docker.GetContainerLogs(ctx, server.ContainerID, tail)
	if err != nil {
		s.log.Error("Failed to get container logs: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to get server logs")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]string{"logs": logs})
}

// Helper function to determine Java version
func getJavaVersionForMC(mcVersion string) string {
	// This is a simplified version - you might want to expand this
	// based on the actual Minecraft version requirements
	parts := []rune(mcVersion)
	if len(parts) >= 4 && parts[2] == '.' {
		minor, _ := strconv.Atoi(string(parts[3:]))
		if minor >= 17 {
			return "17"
		}
	}
	return "8"
}
