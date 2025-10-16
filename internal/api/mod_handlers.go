package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	models "github.com/RandomTechrate/discopanel-fork/internal/db"
	"github.com/RandomTechrate/discopanel-fork/internal/minecraft"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func (s *Server) handleListMods(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	serverID := vars["id"]

	// Get server to find data path and mod loader
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
		return
	}

	// Get the mods directory path
	modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
	if modsDir == "" {
		s.respondError(w, http.StatusBadRequest, "This server type does not support mods")
		return
	}

	// Check if mods directory exists
	if _, err := os.Stat(modsDir); os.IsNotExist(err) {
		// Return empty list if directory doesn't exist
		s.respondJSON(w, http.StatusOK, []models.Mod{})
		return
	}

	// Read mods from directory
	files, err := os.ReadDir(modsDir)
	if err != nil {
		s.log.Error("Failed to read mods directory: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to read mods directory")
		return
	}

	// Build list of mods from files
	mods := []models.Mod{}
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Check if this is a valid mod file
		if !minecraft.IsValidModFile(file.Name(), server.ModLoader) {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		// Create mod entry with consistent ID generation
		mod := models.Mod{
			ID:         uuid.NewSHA1(uuid.NameSpaceURL, []byte(serverID+file.Name())).String(),
			ServerID:   serverID,
			Name:       file.Name(),
			FileName:   file.Name(),
			Enabled:    true,
			FileSize:   info.Size(),
			UploadedAt: info.ModTime(),
		}

		// Extract mod name from filename (remove extension)
		baseName := file.Name()
		if ext := filepath.Ext(baseName); ext != "" {
			baseName = baseName[:len(baseName)-len(ext)]
		}
		mod.Name = baseName

		mods = append(mods, mod)
	}

	// Also check disabled mods directory
	disabledDir := modsDir + "_disabled"
	if files, err := os.ReadDir(disabledDir); err == nil {
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			if !minecraft.IsValidModFile(file.Name(), server.ModLoader) {
				continue
			}

			info, err := file.Info()
			if err != nil {
				continue
			}

			mod := models.Mod{
				ID:         uuid.NewSHA1(uuid.NameSpaceURL, []byte(serverID+file.Name())).String(),
				ServerID:   serverID,
				Name:       file.Name(),
				FileName:   file.Name(),
				Enabled:    false,
				FileSize:   info.Size(),
				UploadedAt: info.ModTime(),
			}

			// Extract mod name from filename
			baseName := file.Name()
			if ext := filepath.Ext(baseName); ext != "" {
				baseName = baseName[:len(baseName)-len(ext)]
			}
			mod.Name = baseName

			mods = append(mods, mod)
		}
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
	serverID := vars["id"]

	// Get server to find mod path
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Server not found")
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

	// For now, we only support enabling/disabling
	if req.Enabled != nil {
		modsDir := minecraft.GetModsPath(server.DataPath, server.ModLoader)
		disabledDir := modsDir + "_disabled"

		// Try to find the mod file by checking both directories
		var modFileName string
		var currentlyEnabled bool

		// First, scan the mods directory
		if files, err := os.ReadDir(modsDir); err == nil {
			for _, file := range files {
				if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
					// Generate the same ID as in listMods to match
					fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(serverID+file.Name())).String()
					if fileID == modID {
						modFileName = file.Name()
						currentlyEnabled = true
						break
					}
				}
			}
		}

		// If not found, check disabled directory
		if modFileName == "" {
			if files, err := os.ReadDir(disabledDir); err == nil {
				for _, file := range files {
					if !file.IsDir() && minecraft.IsValidModFile(file.Name(), server.ModLoader) {
						fileID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(serverID+file.Name())).String()
						if fileID == modID {
							modFileName = file.Name()
							currentlyEnabled = false
							break
						}
					}
				}
			}
		}

		if modFileName == "" {
			s.respondError(w, http.StatusNotFound, "Mod not found")
			return
		}

		// Now move the file if needed
		if *req.Enabled != currentlyEnabled {
			if *req.Enabled {
				// Move from disabled to mods directory
				oldPath := filepath.Join(disabledDir, modFileName)
				newPath := filepath.Join(modsDir, modFileName)
				if err := os.Rename(oldPath, newPath); err != nil {
					s.log.Error("Failed to enable mod: %v", err)
					s.respondError(w, http.StatusInternalServerError, "Failed to enable mod")
					return
				}
			} else {
				// Move from mods to disabled directory
				os.MkdirAll(disabledDir, 0755)
				oldPath := filepath.Join(modsDir, modFileName)
				newPath := filepath.Join(disabledDir, modFileName)
				if err := os.Rename(oldPath, newPath); err != nil {
					s.log.Error("Failed to disable mod: %v", err)
					s.respondError(w, http.StatusInternalServerError, "Failed to disable mod")
					return
				}
			}
		}
	}

	// Return success
	s.respondJSON(w, http.StatusOK, map[string]bool{"success": true})
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
