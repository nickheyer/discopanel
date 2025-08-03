package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	models "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/minecraft"
)

func (s *Server) handleListMods(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	mods, err := s.store.ListServerMods(ctx, serverID)
	if err != nil {
		s.log.Error("Failed to list mods: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to list mods")
		return
	}

	s.respondJSON(w, http.StatusOK, mods)
}

func (s *Server) handleUploadMod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Parse multipart form
	err := r.ParseMultipartForm(100 << 20) // 100 MB max
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Failed to parse upload")
		return
	}

	file, header, err := r.FormFile("mod")
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "No mod file provided")
		return
	}
	defer file.Close()

	// Get server to find data path and mod loader
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Validate file is appropriate for this mod loader
	if !minecraft.IsValidModFile(header.Filename, server.ModLoader) {
		s.respondError(w, http.StatusBadRequest, "Invalid file type for this mod loader")
		return
	}

	// Get the correct mods directory based on mod loader
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		s.respondError(w, http.StatusBadRequest, "This server type does not support mods")
		return
	}

	if err := os.MkdirAll(modsDir, 0755); err != nil {
		s.log.Error("Failed to create mods directory: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to create mods directory")
		return
	}

	// Save file
	modPath := filepath.Join(modsDir, header.Filename)
	dst, err := os.Create(modPath)
	if err != nil {
		s.log.Error("Failed to create mod file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to save mod")
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		s.log.Error("Failed to write mod file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to save mod")
		return
	}

	// Create mod record
	mod := &models.Mod{
		ID:          uuid.New().String(),
		ServerID:    serverID,
		Name:        r.FormValue("name"),
		FileName:    header.Filename,
		Version:     r.FormValue("version"),
		ModID:       r.FormValue("mod_id"),
		Description: r.FormValue("description"),
		Enabled:     true,
		FileSize:    written,
	}

	if mod.Name == "" {
		mod.Name = header.Filename
	}

	// Save to database
	if err := s.store.AddMod(ctx, mod); err != nil {
		s.log.Error("Failed to save mod record: %v", err)
		os.Remove(modPath) // Clean up file
		s.respondError(w, http.StatusInternalServerError, "Failed to save mod")
		return
	}

	s.respondJSON(w, http.StatusCreated, mod)
}

func (s *Server) handleGetMod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	modID := vars["modId"]

	mod, err := s.store.GetMod(ctx, modID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Mod not found")
		return
	}

	s.respondJSON(w, http.StatusOK, mod)
}

func (s *Server) handleUpdateMod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	modID := vars["modId"]

	mod, err := s.store.GetMod(ctx, modID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Mod not found")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description"`
		Enabled     *bool  `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update fields
	if req.Name != "" {
		mod.Name = req.Name
	}
	if req.Version != "" {
		mod.Version = req.Version
	}
	if req.Description != "" {
		mod.Description = req.Description
	}
	if req.Enabled != nil {
		mod.Enabled = *req.Enabled

		// If disabling, move file out of mods directory
		server, err := s.store.GetServer(ctx, mod.ServerID)
		if err == nil {
			modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
			disabledDir := modsDir + "_disabled"

			if !mod.Enabled {
				// Move to disabled directory
				os.MkdirAll(disabledDir, 0755)
				oldPath := filepath.Join(modsDir, mod.FileName)
				newPath := filepath.Join(disabledDir, mod.FileName)
				os.Rename(oldPath, newPath)
			} else {
				// Move back to mods directory
				oldPath := filepath.Join(disabledDir, mod.FileName)
				newPath := filepath.Join(modsDir, mod.FileName)
				os.Rename(oldPath, newPath)
			}
		}
	}

	if err := s.store.UpdateMod(ctx, mod); err != nil {
		s.log.Error("Failed to update mod: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to update mod")
		return
	}

	s.respondJSON(w, http.StatusOK, mod)
}

func (s *Server) handleDeleteMod(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	modID := vars["modId"]

	mod, err := s.store.GetMod(ctx, modID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Mod not found")
		return
	}

	// Get server to find file path
	server, err := s.store.GetServer(ctx, mod.ServerID)
	if err == nil {
		modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
		
		// Delete mod file from active directory
		modPath := filepath.Join(modsDir, mod.FileName)
		if err := os.Remove(modPath); err != nil {
			s.log.Error("Failed to delete mod file: %v", err)
		}

		// Also check disabled directory
		disabledPath := filepath.Join(modsDir+"_disabled", mod.FileName)
		os.Remove(disabledPath)
	}

	// Delete from database
	if err := s.store.DeleteMod(ctx, modID); err != nil {
		s.log.Error("Failed to delete mod: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to delete mod")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
