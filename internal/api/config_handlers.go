package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

func (s *Server) handleGetServerConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	config, err := s.store.GetServerConfig(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server config not found")
		return
	}

	s.respondJSON(w, http.StatusOK, config)
}

func (s *Server) handleUpdateServerConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Get existing config
	config, err := s.store.GetServerConfig(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server config not found")
		return
	}

	// Decode request body
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update fields
	if v, ok := req["difficulty"].(string); ok {
		config.Difficulty = v
	}
	if v, ok := req["gamemode"].(string); ok {
		config.Gamemode = v
	}
	if v, ok := req["level_name"].(string); ok {
		config.LevelName = v
	}
	if v, ok := req["level_seed"].(string); ok {
		config.LevelSeed = v
	}
	if v, ok := req["max_players"].(float64); ok {
		config.MaxPlayers = int(v)
	}
	if v, ok := req["view_distance"].(float64); ok {
		config.ViewDistance = int(v)
	}
	if v, ok := req["online_mode"].(bool); ok {
		config.OnlineMode = v
	}
	if v, ok := req["pvp"].(bool); ok {
		config.PVP = v
	}
	if v, ok := req["allow_nether"].(bool); ok {
		config.AllowNether = v
	}
	if v, ok := req["allow_flight"].(bool); ok {
		config.AllowFlight = v
	}
	if v, ok := req["spawn_animals"].(bool); ok {
		config.SpawnAnimals = v
	}
	if v, ok := req["spawn_monsters"].(bool); ok {
		config.SpawnMonsters = v
	}
	if v, ok := req["spawn_npcs"].(bool); ok {
		config.SpawnNPCs = v
	}
	if v, ok := req["generate_structures"].(bool); ok {
		config.GenerateStructures = v
	}
	if v, ok := req["motd"].(string); ok {
		config.MOTD = v
	}

	// Save updated config
	if err := s.store.UpdateServerConfig(ctx, config); err != nil {
		s.log.Error("Failed to update server config: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to update config")
		return
	}

	// TODO: Apply config to running server if needed

	s.respondJSON(w, http.StatusOK, config)
}