package api

import (
	"encoding/json"
	"net/http"
	"time"

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

	// Check server access for client users
	if !s.checkServerAccess(ctx, serverID) {
		s.respondError(w, http.StatusForbidden, "Access denied to this server")
		return
	}

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

	// Add command to log stream BEFORE exec
	commandTime := time.Now()
	s.logStreamer.AddCommandEntry(server.ContainerID, req.Command, commandTime)

	// Execute command in container
	output, err := s.docker.ExecCommand(ctx, server.ContainerID, req.Command)
	success := err == nil

	// Add command out to log stream AFTER exec
	if output != "" || !success {
		s.logStreamer.AddCommandOutput(server.ContainerID, output, success, commandTime)
	}

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
