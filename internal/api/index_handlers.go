package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/indexers"
	"github.com/nickheyer/discopanel/internal/indexers/fuego"
	"github.com/nickheyer/discopanel/internal/minecraft"
)

func (s *Server) handleSearchModpacks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get query parameters
	query := r.URL.Query().Get("q")
	gameVersion := r.URL.Query().Get("gameVersion")
	modLoader := r.URL.Query().Get("modLoader")
	indexer := r.URL.Query().Get("indexer") // Optional filter by indexer
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize := 20
	offset := (page - 1) * pageSize

	// Search in local database first
	modpacks, total, err := s.store.SearchIndexedModpacks(ctx, query, gameVersion, modLoader, indexer, offset, pageSize)
	if err != nil {
		s.log.Error("Failed to search modpacks: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to search modpacks")
		return
	}

	// Check if modpacks are favorited
	type ModpackWithFavorite struct {
		*db.IndexedModpack
		IsFavorited bool `json:"is_favorited"`
	}

	result := make([]ModpackWithFavorite, len(modpacks))
	for i, modpack := range modpacks {
		isFavorited, _ := s.store.IsModpackFavorited(ctx, modpack.ID)
		result[i] = ModpackWithFavorite{
			IndexedModpack: modpack,
			IsFavorited:    isFavorited,
		}
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"modpacks": result,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleSyncModpacks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req struct {
		Query       string `json:"query"`
		GameVersion string `json:"gameVersion"`
		ModLoader   string `json:"modLoader"`
		Indexer     string `json:"indexer"` // Which indexer to use
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Default to fuego if no indexer specified
	if req.Indexer == "" {
		req.Indexer = "fuego"
	}

	var indexerClient indexers.ModpackIndexer

	switch req.Indexer {
	case "fuego":
		// Get Fuego API key from global settings
		globalSettings, err := s.store.GetGlobalSettings(ctx)
		if err != nil {
			s.log.Error("Failed to get global settings: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to get global settings")
			return
		}

		apiKey := ""
		if globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}

		if apiKey == "" {
			s.respondError(w, http.StatusBadRequest, "Fuego API key not configured in global settings")
			return
		}

		indexerClient = fuego.NewIndexer(apiKey)
	default:
		s.respondError(w, http.StatusBadRequest, "Unknown indexer: "+req.Indexer)
		return
	}

	// Search modpacks using the indexer
	searchResp, err := indexerClient.SearchModpacks(ctx, req.Query, req.GameVersion, req.ModLoader, 0, 50)
	if err != nil {
		s.log.Error("Failed to search %s: %v", req.Indexer, err)
		s.respondError(w, http.StatusInternalServerError, "Failed to search "+req.Indexer+": "+err.Error())
		return
	}

	// Store modpacks in database
	synced := 0
	for _, modpack := range searchResp.Modpacks {
		// Convert to JSON strings for storage
		categoriesJSON, _ := json.Marshal(modpack.Categories)
		gameVersionsJSON, _ := json.Marshal(modpack.GameVersions)
		modLoadersJSON, _ := json.Marshal(modpack.ModLoaders)

		// Find the actual Minecraft version from the game versions list
		mcVersion := ""
		for _, version := range modpack.GameVersions {
			// Check if it's a valid Minecraft version (starts with digit)
			if len(version) > 0 && version[0] >= '0' && version[0] <= '9' {
				mcVersion = version
				break
			}
		}
		
		// If still no version found, log error
		if mcVersion == "" {
			s.log.Error("No valid Minecraft version found in modpack %s game versions: %v", modpack.ID, modpack.GameVersions)
			mcVersion = "1.21.1" // Fallback
		}

		// Determine Java version and Docker image
		javaVersion := minecraft.GetJavaVersionForMinecraft(mcVersion)
		dockerImage := "java8"
		switch javaVersion {
		case "21":
			dockerImage = "java21"
		case "17":
			dockerImage = "java17"
		case "11":
			dockerImage = "java11"
		}

		dbModpack := &db.IndexedModpack{
			ID:             modpack.ID,
			IndexerID:      modpack.IndexerID,
			Indexer:        modpack.Indexer,
			Name:           modpack.Name,
			Slug:           modpack.Slug,
			Summary:        modpack.Summary,
			Description:    modpack.Description,
			LogoURL:        modpack.LogoURL,
			WebsiteURL:     modpack.WebsiteURL,
			DownloadCount:  modpack.DownloadCount,
			Categories:     string(categoriesJSON),
			GameVersions:   string(gameVersionsJSON),
			ModLoaders:     string(modLoadersJSON),
			LatestFileID:   modpack.LatestFileID,
			DateCreated:    modpack.DateCreated,
			DateModified:   modpack.DateModified,
			DateReleased:   modpack.DateReleased,
			// Computed fields
			MCVersion:      mcVersion,
			JavaVersion:    javaVersion,
			DockerImage:    dockerImage,
			RecommendedRAM: 6144, // 6GB for modpacks
		}

		if err := s.store.UpsertIndexedModpack(ctx, dbModpack); err != nil {
			s.log.Error("Failed to store modpack %s: %v", modpack.ID, err)
			continue
		}
		synced++
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"synced": synced,
		"total":  searchResp.TotalCount,
	})
}

func (s *Server) handleGetModpack(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	modpackID := vars["id"]

	modpack, err := s.store.GetIndexedModpack(ctx, modpackID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Modpack not found")
		return
	}

	// Check if favorited
	isFavorited, _ := s.store.IsModpackFavorited(ctx, modpackID)

	s.respondJSON(w, http.StatusOK, map[string]any{
		"modpack":      modpack,
		"is_favorited": isFavorited,
	})
}

func (s *Server) handleToggleFavorite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	modpackID := vars["id"]

	// Check if already favorited
	isFavorited, err := s.store.IsModpackFavorited(ctx, modpackID)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to check favorite status")
		return
	}

	if isFavorited {
		// Remove favorite
		if err := s.store.RemoveModpackFavorite(ctx, modpackID); err != nil {
			s.respondError(w, http.StatusInternalServerError, "Failed to remove favorite")
			return
		}
	} else {
		// Add favorite
		if err := s.store.AddModpackFavorite(ctx, modpackID); err != nil {
			s.respondError(w, http.StatusInternalServerError, "Failed to add favorite")
			return
		}
	}

	s.respondJSON(w, http.StatusOK, map[string]bool{
		"is_favorited": !isFavorited,
	})
}

func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	modpacks, err := s.store.ListFavoriteModpacks(ctx)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to list favorites")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"modpacks": modpacks,
	})
}

func (s *Server) handleSyncModpackFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	modpackID := vars["id"]

	// Get the modpack to determine its indexer
	modpack, err := s.store.GetIndexedModpack(ctx, modpackID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Modpack not found")
		return
	}

	var indexerClient indexers.ModpackIndexer

	switch modpack.Indexer {
	case "fuego":
		// Get Fuego API key from global settings
		globalSettings, err := s.store.GetGlobalSettings(ctx)
		if err != nil {
			s.log.Error("Failed to get global settings: %v", err)
			s.respondError(w, http.StatusInternalServerError, "Failed to get global settings")
			return
		}

		apiKey := ""
		if globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}

		if apiKey == "" {
			s.respondError(w, http.StatusBadRequest, "Fuego API key not configured in global settings")
			return
		}

		indexerClient = fuego.NewIndexer(apiKey)
	default:
		s.respondError(w, http.StatusBadRequest, "Unknown indexer: "+modpack.Indexer)
		return
	}

	// Get files from the indexer
	files, err := indexerClient.GetModpackFiles(ctx, modpack.IndexerID)
	if err != nil {
		s.log.Error("Failed to get modpack files: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to get modpack files")
		return
	}

	// Store files in database
	synced := 0
	for _, file := range files {
		// Convert to JSON strings for storage
		gameVersionsJSON, _ := json.Marshal(file.GameVersions)

		dbFile := &db.IndexedModpackFile{
			ID:               file.ID,
			ModpackID:        modpackID,
			DisplayName:      file.DisplayName,
			FileName:         file.FileName,
			FileDate:         file.FileDate,
			FileLength:       file.FileLength,
			ReleaseType:      file.ReleaseType,
			DownloadURL:      file.DownloadURL,
			GameVersions:     string(gameVersionsJSON),
			ModLoader:        file.ModLoader,
			ServerPackFileID: file.ServerPackFileID,
		}

		if err := s.store.UpsertIndexedModpackFile(ctx, dbFile); err != nil {
			s.log.Error("Failed to store modpack file %s: %v", file.ID, err)
			continue
		}
		synced++
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"synced": synced,
		"total":  len(files),
	})
}

func (s *Server) handleGetModpackFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	modpackID := vars["id"]

	files, err := s.store.GetIndexedModpackFiles(ctx, modpackID)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, "Failed to get modpack files")
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"files": files,
	})
}

func (s *Server) handleGetModpackConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	modpackID := vars["id"]

	modpack, err := s.store.GetIndexedModpack(ctx, modpackID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Modpack not found")
		return
	}

	// Return configuration from the modpack's computed fields
	config := map[string]interface{}{
		"name":         modpack.Name,
		"description":  modpack.Summary,
		"mod_loader":   "auto_curseforge", // For fuego modpacks
		"mc_version":   modpack.MCVersion,
		"memory":       modpack.RecommendedRAM,
		"docker_image": modpack.DockerImage,
	}
	
	s.respondJSON(w, http.StatusOK, config)
}
