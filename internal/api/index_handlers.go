package api

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/nickheyer/discopanel/internal/db"
	models "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/indexers"
	"github.com/nickheyer/discopanel/internal/indexers/fuego"
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
		globalSettings, _, err := s.store.GetGlobalSettings(ctx)
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
		// Extract first mod loader for docker tag selection
		modLoader := models.ModLoaderVanilla
		if len(modpack.ModLoaders) > 0 {
			switch modpack.ModLoaders[0] {
			case "forge":
				modLoader = models.ModLoaderForge
			case "fabric":
				modLoader = models.ModLoaderFabric
			case "neoforge":
				modLoader = models.ModLoaderNeoForge
			case "quilt":
				modLoader = models.ModLoaderQuilt
			}
		}

		javaVersion := strconv.Itoa(docker.GetRequiredJavaVersion(mcVersion, modLoader))
		dockerImage := docker.GetOptimalDockerTag(mcVersion, modLoader, false)

		dbModpack := &db.IndexedModpack{
			ID:            modpack.ID,
			IndexerID:     modpack.IndexerID,
			Indexer:       modpack.Indexer,
			Name:          modpack.Name,
			Slug:          modpack.Slug,
			Summary:       modpack.Summary,
			Description:   modpack.Description,
			LogoURL:       modpack.LogoURL,
			WebsiteURL:    modpack.WebsiteURL,
			DownloadCount: modpack.DownloadCount,
			Categories:    string(categoriesJSON),
			GameVersions:  string(gameVersionsJSON),
			ModLoaders:    string(modLoadersJSON),
			LatestFileID:  modpack.LatestFileID,
			DateCreated:   modpack.DateCreated,
			DateModified:  modpack.DateModified,
			DateReleased:  modpack.DateReleased,
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
		globalSettings, _, err := s.store.GetGlobalSettings(ctx)
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
	modLoader := "auto_curseforge" // Default for fuego modpacks
	if modpack.Indexer == "manual" {
		// For manual uploads, use the actual mod loader from the modpack
		var modLoaders []string
		if err := json.Unmarshal([]byte(modpack.ModLoaders), &modLoaders); err == nil && len(modLoaders) > 0 {
			// Use first mod loader from the list
			modLoader = modLoaders[0]
		}
	}

	config := map[string]any{
		"name":         modpack.Name,
		"description":  modpack.Summary,
		"mod_loader":   modLoader,
		"mc_version":   modpack.MCVersion,
		"memory":       modpack.RecommendedRAM,
		"docker_image": modpack.DockerImage,
		"modpack_file": modpack.LatestFileID, // For manual modpacks, this is the file ID
	}

	s.respondJSON(w, http.StatusOK, config)
}

func (s *Server) handleGetIndexerStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get global settings to check API key
	globalSettings, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to get global settings")
		return
	}

	apiKeyConfigured := false
	if globalSettings.CFAPIKey != nil && *globalSettings.CFAPIKey != "" {
		apiKeyConfigured = true
	}

	status := map[string]any{
		"indexers": map[string]any{
			"fuego": map[string]any{
				"name":             "CurseForge",
				"enabled":          apiKeyConfigured,
				"apiKeyConfigured": apiKeyConfigured,
				"apiKeyUrl":        "https://console.curseforge.com/#/api-keys",
			},
		},
	}

	s.respondJSON(w, http.StatusOK, status)
}

func (s *Server) handleUploadModpack(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse multipart form with max 500MB
	err := r.ParseMultipartForm(500 << 20)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Failed to parse form data")
		return
	}

	// Get the uploaded file
	file, header, err := r.FormFile("modpack")
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "No modpack file provided")
		return
	}
	defer file.Close()

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		s.respondError(w, http.StatusBadRequest, "Modpack must be a ZIP file")
		return
	}

	// Create temporary file
	tempFile, err := os.CreateTemp("", "modpack-*.zip")
	if err != nil {
		s.log.Error("Failed to create temp file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to process upload")
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Copy uploaded file to temp file
	if _, err := io.Copy(tempFile, file); err != nil {
		s.log.Error("Failed to copy uploaded file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to process upload")
		return
	}

	// Reset file pointer to beginning
	if _, err := tempFile.Seek(0, 0); err != nil {
		s.log.Error("Failed to seek temp file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to process upload")
		return
	}

	// Open zip file for reading
	zipReader, err := zip.OpenReader(tempFile.Name())
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid ZIP file")
		return
	}
	defer zipReader.Close()

	// Look for manifest.json to determine modpack type
	var manifestFile *zip.File
	for _, f := range zipReader.File {
		if f.Name == "manifest.json" || strings.HasSuffix(f.Name, "/manifest.json") {
			manifestFile = f
			break
		}
	}

	if manifestFile == nil {
		s.respondError(w, http.StatusBadRequest, "No manifest.json found in modpack")
		return
	}

	// Read manifest
	manifestReader, err := manifestFile.Open()
	if err != nil {
		s.log.Error("Failed to open manifest: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to read manifest")
		return
	}
	defer manifestReader.Close()

	var manifest struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Author      string `json:"author"`
		Description string `json:"description,omitempty"`
		Minecraft   struct {
			Version    string `json:"version"`
			ModLoaders []struct {
				ID      string `json:"id"`
				Primary bool   `json:"primary"`
			} `json:"modLoaders"`
		} `json:"minecraft"`
	}

	if err := json.NewDecoder(manifestReader).Decode(&manifest); err != nil {
		s.log.Error("Failed to parse manifest: %v", err)
		s.respondError(w, http.StatusBadRequest, "Invalid manifest.json")
		return
	}

	// Generate ID for the uploaded modpack
	modpackID := uuid.New().String()

	// Determine mod loader from manifest
	modLoader := ""
	for _, ml := range manifest.Minecraft.ModLoaders {
		if strings.Contains(ml.ID, "forge") {
			modLoader = "forge"
			break
		} else if strings.Contains(ml.ID, "fabric") {
			modLoader = "fabric"
			break
		} else if strings.Contains(ml.ID, "neoforge") {
			modLoader = "neoforge"
			break
		} else if strings.Contains(ml.ID, "quilt") {
			modLoader = "quilt"
			break
		}
	}

	// Get Java version and Docker image
	// Convert manual modpack mod loader string to enum
	manualModLoader := models.ModLoaderVanilla
	switch modLoader {
	case "forge":
		manualModLoader = models.ModLoaderForge
	case "fabric":
		manualModLoader = models.ModLoaderFabric
	case "neoforge":
		manualModLoader = models.ModLoaderNeoForge
	case "quilt":
		manualModLoader = models.ModLoaderQuilt
	}

	javaVersion := strconv.Itoa(docker.GetRequiredJavaVersion(manifest.Minecraft.Version, manualModLoader))
	dockerImage := docker.GetOptimalDockerTag(manifest.Minecraft.Version, manualModLoader, false)

	// Create database entry
	gameVersionsJSON, _ := json.Marshal([]string{manifest.Minecraft.Version})
	modLoadersJSON, _ := json.Marshal([]string{modLoader})

	dbModpack := &db.IndexedModpack{
		ID:             modpackID,
		IndexerID:      modpackID,
		Indexer:        "manual",
		Name:           manifest.Name,
		Slug:           strings.ToLower(strings.ReplaceAll(manifest.Name, " ", "-")),
		Summary:        fmt.Sprintf("Version %s by %s", manifest.Version, manifest.Author),
		Description:    manifest.Description,
		LogoURL:        "", // No logo for manual uploads
		WebsiteURL:     "",
		DownloadCount:  0,
		Categories:     "[]",
		GameVersions:   string(gameVersionsJSON),
		ModLoaders:     string(modLoadersJSON),
		LatestFileID:   modpackID,
		DateCreated:    time.Now(),
		DateModified:   time.Now(),
		DateReleased:   time.Now(),
		MCVersion:      manifest.Minecraft.Version,
		JavaVersion:    javaVersion,
		DockerImage:    dockerImage,
		RecommendedRAM: 6144, // 6GB for modpacks
	}

	if err := s.store.UpsertIndexedModpack(ctx, dbModpack); err != nil {
		s.log.Error("Failed to store modpack: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to store modpack")
		return
	}

	// Store the ZIP file
	// Create directory for manual modpacks if it doesn't exist
	manualDir := filepath.Join("/data", "modpacks", "manual")
	if err := os.MkdirAll(manualDir, 0755); err != nil {
		s.log.Error("Failed to create manual modpacks directory: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to store modpack file")
		return
	}

	// Copy ZIP to storage
	destPath := filepath.Join(manualDir, modpackID+".zip")
	destFile, err := os.Create(destPath)
	if err != nil {
		s.log.Error("Failed to create destination file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to store modpack file")
		return
	}
	defer destFile.Close()

	if _, err := tempFile.Seek(0, 0); err != nil {
		s.log.Error("Failed to seek temp file: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to store modpack file")
		return
	}

	if _, err := io.Copy(destFile, tempFile); err != nil {
		s.log.Error("Failed to copy modpack to storage: %v", err)
		s.respondError(w, http.StatusInternalServerError, "Failed to store modpack file")
		return
	}

	// Create a file entry for the uploaded modpack
	dbFile := &db.IndexedModpackFile{
		ID:               modpackID,
		ModpackID:        modpackID,
		DisplayName:      header.Filename,
		FileName:         header.Filename,
		FileDate:         time.Now(),
		FileLength:       header.Size,
		ReleaseType:      "1",      // Release
		DownloadURL:      destPath, // Store local path
		GameVersions:     string(gameVersionsJSON),
		ModLoader:        modLoader,
		ServerPackFileID: nil,
	}

	if err := s.store.UpsertIndexedModpackFile(ctx, dbFile); err != nil {
		s.log.Error("Failed to store modpack file entry: %v", err)
		// Don't fail the upload, just log the error
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"id":      modpackID,
		"name":    manifest.Name,
		"version": manifest.Version,
		"author":  manifest.Author,
	})
}
