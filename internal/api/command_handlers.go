package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type CommandRequest struct {
	Command string `json:"command"`
}

type CommandResponse struct {
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) handleSendCommand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Get server from database
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.log.Error("Failed to get server: %v", err)
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Check if server is running
	if server.ContainerID == "" {
		s.respondError(w, http.StatusBadRequest, "Server container not found")
		return
	}

	status, err := s.docker.GetContainerStatus(ctx, server.ContainerID)
	if err != nil || status != "running" {
		s.respondError(w, http.StatusBadRequest, "Server is not running")
		return
	}

	// Parse request body
	var req CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Command == "" {
		s.respondError(w, http.StatusBadRequest, "Command is required")
		return
	}

	// Execute command in container
	output, err := s.docker.ExecCommand(ctx, server.ContainerID, req.Command)
	if err != nil {
		s.log.Error("Failed to execute command: %v", err)
		s.respondJSON(w, http.StatusOK, CommandResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	s.respondJSON(w, http.StatusOK, CommandResponse{
		Success: true,
		Output:  output,
	})
}