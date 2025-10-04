package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/nickheyer/discopanel/internal/console"
)

type CommandRequest struct {
	Command string `json:"command"`
}

type CommandResponse struct {
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

type CommandHistoryResponse struct {
	Commands []*console.CommandEntry `json:"commands"`
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
		
		// Store failed command in history
		console.AddCommand(serverID, req.Command, "", false, err)
		
		s.respondJSON(w, http.StatusOK, CommandResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	// Store successful command in history
	console.AddCommand(serverID, req.Command, output, true, nil)

	s.respondJSON(w, http.StatusOK, CommandResponse{
		Success: true,
		Output:  output,
	})
}

func (s *Server) handleGetCommandHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Get limit from query parameter (default to 50)
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	// Get command history
	commands := console.GetCommands(serverID, limit)

	s.respondJSON(w, http.StatusOK, CommandHistoryResponse{
		Commands: commands,
	})
}