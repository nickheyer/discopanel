package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/nickheyer/discopanel/internal/minecraft"
)

func (s *Server) handleGetServerConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Get server info for data path
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Check if server.properties exists
	propertiesPath := filepath.Join(server.DataPath, "server.properties")
	if _, err := os.Stat(propertiesPath); os.IsNotExist(err) {
		// Return default properties if file doesn't exist yet
		properties := minecraft.GetDefaultServerProperties()
		s.respondJSON(w, http.StatusOK, properties)
		return
	}

	// Load actual server.properties
	properties, err := minecraft.LoadServerProperties(server.DataPath)
	if err != nil {
		s.log.Error("Failed to load server.properties: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to load server configuration")
		return
	}

	s.respondJSON(w, http.StatusOK, properties)
}

func (s *Server) handleUpdateServerConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Get server info for data path
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Load current properties or use defaults
	var properties minecraft.ServerProperties
	propertiesPath := filepath.Join(server.DataPath, "server.properties")
	if _, err := os.Stat(propertiesPath); os.IsNotExist(err) {
		// Use defaults if file doesn't exist
		properties = minecraft.GetDefaultServerProperties()
	} else {
		// Load existing properties
		properties, err = minecraft.LoadServerProperties(server.DataPath)
		if err != nil {
			s.log.Error("Failed to load server.properties: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to load server configuration")
			return
		}
	}

	// Decode request body - expecting key-value pairs matching server.properties keys
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update properties with new values
	for key, value := range updates {
		switch v := value.(type) {
		case string:
			properties[key] = v
		case float64:
			properties.SetInt(key, int(v))
		case bool:
			properties.SetBool(key, v)
		default:
			// Convert to string representation
			properties[key] = fmt.Sprintf("%v", value)
		}
	}

	// Ensure data directory exists
	if err := os.MkdirAll(server.DataPath, 0755); err != nil {
		s.log.Error("Failed to create server data directory: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create server directory")
		return
	}

	// Save updated properties
	if err := minecraft.SaveServerProperties(server.DataPath, properties); err != nil {
		s.log.Error("Failed to save server.properties: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to save server configuration")
		return
	}

	// If server is running, we could send RCON commands to reload config
	// For now, just return the updated properties
	s.respondJSON(w, http.StatusOK, properties)
}