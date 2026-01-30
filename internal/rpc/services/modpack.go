package services

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/nickheyer/discopanel/internal/config"
	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
	"github.com/nickheyer/discopanel/internal/indexers"
	"github.com/nickheyer/discopanel/internal/indexers/fuego"
	"github.com/nickheyer/discopanel/internal/indexers/modrinth"
	"github.com/nickheyer/discopanel/internal/minecraft"
	"github.com/nickheyer/discopanel/pkg/files"
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"github.com/nickheyer/discopanel/pkg/upload"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that ModpackService implements the interface
var _ discopanelv1connect.ModpackServiceHandler = (*ModpackService)(nil)

// ModpackService implements the Modpack service
type ModpackService struct {
	store         *storage.Store
	config        *config.Config
	log           *logger.Logger
	uploadManager *upload.Manager
}

// NewModpackService creates a new modpack service
func NewModpackService(store *storage.Store, cfg *config.Config, uploadManager *upload.Manager, log *logger.Logger) *ModpackService {
	return &ModpackService{
		store:         store,
		config:        cfg,
		log:           log,
		uploadManager: uploadManager,
	}
}

// SearchModpacks searches for modpacks
func (s *ModpackService) SearchModpacks(ctx context.Context, req *connect.Request[v1.SearchModpacksRequest]) (*connect.Response[v1.SearchModpacksResponse], error) {
	msg := req.Msg

	// Default pagination
	page := int(msg.Page)
	if page < 1 {
		page = 1
	}
	pageSize := int(msg.PageSize)
	if pageSize <= 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// Search in local database
	modpacks, total, err := s.store.SearchIndexedModpacks(ctx, msg.Query, msg.GameVersion, msg.ModLoader, msg.Indexer, offset, pageSize)
	if err != nil {
		s.log.Error("Failed to search modpacks: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to search modpacks: %w", err))
	}

	// Convert to proto format and check if modpacks are favorited
	protoModpacks := make([]*v1.IndexedModpack, len(modpacks))
	for i, modpack := range modpacks {
		isFavorited, _ := s.store.IsModpackFavorited(ctx, modpack.ID)

		javaVersionInt, _ := strconv.Atoi(modpack.JavaVersion)

		protoModpacks[i] = &v1.IndexedModpack{
			Id:             modpack.ID,
			IndexerId:      modpack.IndexerID,
			Indexer:        modpack.Indexer,
			Name:           modpack.Name,
			Slug:           modpack.Slug,
			Summary:        modpack.Summary,
			Description:    modpack.Description,
			LogoUrl:        modpack.LogoURL,
			WebsiteUrl:     modpack.WebsiteURL,
			DownloadCount:  int32(modpack.DownloadCount),
			Categories:     modpack.Categories,
			GameVersions:   modpack.GameVersions,
			ModLoaders:     modpack.ModLoaders,
			LatestFileId:   modpack.LatestFileID,
			DateCreated:    timestamppb.New(modpack.DateCreated),
			DateModified:   timestamppb.New(modpack.DateModified),
			DateReleased:   timestamppb.New(modpack.DateReleased),
			McVersion:      modpack.MCVersion,
			JavaVersion:    int32(javaVersionInt),
			DockerImage:    modpack.DockerImage,
			RecommendedRam: int32(modpack.RecommendedRAM),
			IsFavorited:    isFavorited,
		}
	}

	return connect.NewResponse(&v1.SearchModpacksResponse{
		Modpacks: protoModpacks,
		Total:    int32(total),
		Page:     int32(page),
		PageSize: int32(pageSize),
	}), nil
}

// GetModpack gets a specific modpack
func (s *ModpackService) GetModpack(ctx context.Context, req *connect.Request[v1.GetModpackRequest]) (*connect.Response[v1.GetModpackResponse], error) {
	modpack, err := s.store.GetIndexedModpack(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("modpack not found"))
	}

	// Check if favorited
	isFavorited, _ := s.store.IsModpackFavorited(ctx, req.Msg.Id)

	javaVersionInt, _ := strconv.Atoi(modpack.JavaVersion)

	protoModpack := &v1.IndexedModpack{
		Id:             modpack.ID,
		IndexerId:      modpack.IndexerID,
		Indexer:        modpack.Indexer,
		Name:           modpack.Name,
		Slug:           modpack.Slug,
		Summary:        modpack.Summary,
		Description:    modpack.Description,
		LogoUrl:        modpack.LogoURL,
		WebsiteUrl:     modpack.WebsiteURL,
		DownloadCount:  int32(modpack.DownloadCount),
		Categories:     modpack.Categories,
		GameVersions:   modpack.GameVersions,
		ModLoaders:     modpack.ModLoaders,
		LatestFileId:   modpack.LatestFileID,
		DateCreated:    timestamppb.New(modpack.DateCreated),
		DateModified:   timestamppb.New(modpack.DateModified),
		DateReleased:   timestamppb.New(modpack.DateReleased),
		McVersion:      modpack.MCVersion,
		JavaVersion:    int32(javaVersionInt),
		DockerImage:    modpack.DockerImage,
		RecommendedRam: int32(modpack.RecommendedRAM),
		IsFavorited:    isFavorited,
	}

	return connect.NewResponse(&v1.GetModpackResponse{
		Modpack: protoModpack,
	}), nil
}

// GetModpackBySlug gets a modpack by its slug
func (s *ModpackService) GetModpackBySlug(ctx context.Context, req *connect.Request[v1.GetModpackBySlugRequest]) (*connect.Response[v1.GetModpackBySlugResponse], error) {
	modpack, err := s.store.GetModpackBySlug(ctx, req.Msg.Slug)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to lookup modpack"))
	}

	// Not found is okay - return empty response
	if modpack == nil {
		return connect.NewResponse(&v1.GetModpackBySlugResponse{}), nil
	}

	javaVersionInt, _ := strconv.Atoi(modpack.JavaVersion)

	protoModpack := &v1.IndexedModpack{
		Id:             modpack.ID,
		IndexerId:      modpack.IndexerID,
		Indexer:        modpack.Indexer,
		Name:           modpack.Name,
		Slug:           modpack.Slug,
		Summary:        modpack.Summary,
		Description:    modpack.Description,
		LogoUrl:        modpack.LogoURL,
		WebsiteUrl:     modpack.WebsiteURL,
		DownloadCount:  int32(modpack.DownloadCount),
		Categories:     modpack.Categories,
		GameVersions:   modpack.GameVersions,
		ModLoaders:     modpack.ModLoaders,
		LatestFileId:   modpack.LatestFileID,
		DateCreated:    timestamppb.New(modpack.DateCreated),
		DateModified:   timestamppb.New(modpack.DateModified),
		DateReleased:   timestamppb.New(modpack.DateReleased),
		McVersion:      modpack.MCVersion,
		JavaVersion:    int32(javaVersionInt),
		DockerImage:    modpack.DockerImage,
		RecommendedRam: int32(modpack.RecommendedRAM),
	}

	return connect.NewResponse(&v1.GetModpackBySlugResponse{
		Modpack: protoModpack,
	}), nil
}

// GetModpackByURL gets a modpack by its website URL
func (s *ModpackService) GetModpackByURL(ctx context.Context, req *connect.Request[v1.GetModpackByURLRequest]) (*connect.Response[v1.GetModpackByURLResponse], error) {
	modpack, err := s.store.GetModpackByWebsiteURL(ctx, req.Msg.Url)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to lookup modpack"))
	}

	if modpack == nil {
		return connect.NewResponse(&v1.GetModpackByURLResponse{}), nil
	}

	javaVersionInt, _ := strconv.Atoi(modpack.JavaVersion)

	protoModpack := &v1.IndexedModpack{
		Id:             modpack.ID,
		IndexerId:      modpack.IndexerID,
		Indexer:        modpack.Indexer,
		Name:           modpack.Name,
		Slug:           modpack.Slug,
		Summary:        modpack.Summary,
		Description:    modpack.Description,
		LogoUrl:        modpack.LogoURL,
		WebsiteUrl:     modpack.WebsiteURL,
		DownloadCount:  int32(modpack.DownloadCount),
		Categories:     modpack.Categories,
		GameVersions:   modpack.GameVersions,
		ModLoaders:     modpack.ModLoaders,
		LatestFileId:   modpack.LatestFileID,
		DateCreated:    timestamppb.New(modpack.DateCreated),
		DateModified:   timestamppb.New(modpack.DateModified),
		DateReleased:   timestamppb.New(modpack.DateReleased),
		McVersion:      modpack.MCVersion,
		JavaVersion:    int32(javaVersionInt),
		DockerImage:    modpack.DockerImage,
		RecommendedRam: int32(modpack.RecommendedRAM),
	}

	return connect.NewResponse(&v1.GetModpackByURLResponse{
		Modpack: protoModpack,
	}), nil
}

// GetModpackConfig gets modpack configuration
func (s *ModpackService) GetModpackConfig(ctx context.Context, req *connect.Request[v1.GetModpackConfigRequest]) (*connect.Response[v1.GetModpackConfigResponse], error) {
	modpack, err := s.store.GetIndexedModpack(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("modpack not found"))
	}

	modLoader := modpack.Indexer
	switch modpack.Indexer {
	case "manual":
		// For manual uploads, use the actual mod loader from the modpack
		var modLoaders []string
		if err := json.Unmarshal([]byte(modpack.ModLoaders), &modLoaders); err == nil && len(modLoaders) > 0 {
			// Use first mod loader from the list
			modLoader = modLoaders[0]
		}
	case "fuego":
		modLoader = "auto_curseforge"
	}

	config := map[string]string{
		"name":         modpack.Name,
		"description":  modpack.Summary,
		"mod_loader":   modLoader,
		"mc_version":   modpack.MCVersion,
		"memory":       strconv.Itoa(modpack.RecommendedRAM),
		"docker_image": modpack.DockerImage,
		"modpack_file": modpack.LatestFileID, // For manual modpacks, this is the file ID
	}

	return connect.NewResponse(&v1.GetModpackConfigResponse{
		Config: config,
	}), nil
}

// GetModpackFiles gets modpack files
func (s *ModpackService) GetModpackFiles(ctx context.Context, req *connect.Request[v1.GetModpackFilesRequest]) (*connect.Response[v1.GetModpackFilesResponse], error) {
	files, err := s.store.GetIndexedModpackFiles(ctx, req.Msg.Id)
	if err != nil {
		s.log.Error("Failed to get modpack files: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get modpack files"))
	}

	protoFiles := make([]*v1.ModpackFile, len(files))
	for i, file := range files {
		// Parse game versions from JSON
		var gameVersions []string
		if file.GameVersions != "" {
			json.Unmarshal([]byte(file.GameVersions), &gameVersions)
		}

		protoFiles[i] = &v1.ModpackFile{
			Id:           file.ID,
			ModpackId:    file.ModpackID,
			DisplayName:  file.DisplayName,
			FileName:     file.FileName,
			FileDate:     timestamppb.New(file.FileDate),
			FileLength:   file.FileLength,
			ReleaseType:  file.ReleaseType,
			DownloadUrl:  file.DownloadURL,
			GameVersions: gameVersions,
			SortIndex:    int32(i),
		}
	}

	return connect.NewResponse(&v1.GetModpackFilesResponse{
		Files: protoFiles,
	}), nil
}

// GetModpackVersions gets modpack versions
func (s *ModpackService) GetModpackVersions(ctx context.Context, req *connect.Request[v1.GetModpackVersionsRequest]) (*connect.Response[v1.GetModpackVersionsResponse], error) {
	// Get the modpack to determine its type
	modpack, err := s.store.GetIndexedModpack(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("modpack not found"))
	}

	// Get appropriate indexer client
	var indexerClient indexers.ModpackIndexer
	switch modpack.Indexer {
	case "fuego":
		// Get API key from global settings
		globalSettings, _, err := s.store.GetGlobalSettings(ctx)
		if err != nil || globalSettings == nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get global settings"))
		}

		apiKey := ""
		if globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}
		if apiKey == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("CurseForge API key not configured"))
		}
		indexerClient = fuego.NewIndexer(apiKey, s.config)
	case "modrinth":
		indexerClient = modrinth.NewIndexer(s.config)
	case "manual":
		// For manual modpacks, return empty list
		return connect.NewResponse(&v1.GetModpackVersionsResponse{
			Versions: []*v1.Version{},
		}), nil
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown indexer: %s", modpack.Indexer))
	}

	// Get files from the indexer
	files, err := indexerClient.GetModpackFiles(ctx, modpack.IndexerID)
	if err != nil {
		s.log.Error("Failed to get modpack files from %s: %v", modpack.Indexer, err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get modpack versions"))
	}

	// Convert files to versions
	versions := make([]*v1.Version, 0, len(files))
	for _, file := range files {
		versions = append(versions, &v1.Version{
			Id:            file.ID,
			DisplayName:   file.DisplayName,
			ReleaseType:   file.ReleaseType,
			FileDate:      timestamppb.New(file.FileDate),
			SortIndex:     int32(file.SortIndex),
			VersionNumber: file.VersionNumber,
		})
	}

	// Sort by SortIndex to maintain API order (lower index = newer version)
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].SortIndex < versions[j].SortIndex
	})

	return connect.NewResponse(&v1.GetModpackVersionsResponse{
		Versions: versions,
	}), nil
}

// SyncModpacks syncs modpacks
func (s *ModpackService) SyncModpacks(ctx context.Context, req *connect.Request[v1.SyncModpacksRequest]) (*connect.Response[v1.SyncModpacksResponse], error) {
	msg := req.Msg

	// Default to fuego if no indexer specified
	indexer := msg.Indexer
	if indexer == "" {
		indexer = "fuego"
	}

	var indexerClient indexers.ModpackIndexer

	switch indexer {
	case "fuego":
		// Get Fuego API key from global settings
		globalSettings, _, err := s.store.GetGlobalSettings(ctx)
		if err != nil {
			s.log.Error("Failed to get global settings: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get global settings"))
		}

		apiKey := ""
		if globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}

		if apiKey == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("fuego API key not configured in global settings"))
		}

		indexerClient = fuego.NewIndexer(apiKey, s.config)
	case "modrinth":
		// Modrinth doesn't require an API key for public operations
		indexerClient = modrinth.NewIndexer(s.config)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown indexer: %s", indexer))
	}

	// Search modpacks using the indexer
	searchResp, err := indexerClient.SearchModpacks(ctx, msg.Query, msg.GameVersion, msg.ModLoader, 0, 50)
	if err != nil {
		s.log.Error("Failed to search %s: %v", indexer, err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to search %s: %w", indexer, err))
	}

	// Store modpacks in database
	synced := 0
	for _, modpack := range searchResp.Modpacks {
		// Convert to JSON strings for storage
		categoriesJSON, _ := json.Marshal(modpack.Categories)
		gameVersionsJSON, _ := json.Marshal(modpack.GameVersions)
		modLoadersJSON, _ := json.Marshal(modpack.ModLoaders)

		// Find the most recent Minecraft version from the game versions list
		mcVersion := minecraft.FindMostRecentMinecraftVersion(modpack.GameVersions)

		modLoader := storage.ModLoaderVanilla
		if len(modpack.ModLoaders) > 0 {
			modLoader = storage.ModLoader(modpack.ModLoaders[0])
		}

		javaVersion := docker.GetRequiredJavaVersion(mcVersion, modLoader)
		dockerImage := docker.GetOptimalDockerTag(mcVersion, modLoader, false)

		dbModpack := &storage.IndexedModpack{
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

	return connect.NewResponse(&v1.SyncModpacksResponse{
		SyncedCount: int32(synced),
		Message:     fmt.Sprintf("Synced %d of %d modpacks", synced, searchResp.TotalCount),
	}), nil
}

// ImportUploadedModpack imports a modpack from a completed chunked upload session
func (s *ModpackService) ImportUploadedModpack(ctx context.Context, req *connect.Request[v1.ImportUploadedModpackRequest]) (*connect.Response[v1.ImportUploadedModpackResponse], error) {
	msg := req.Msg

	s.log.Info("Starting modpack upload import with sessionId: %s", msg.UploadSessionId)

	// Validate upload session
	if msg.UploadSessionId == "" {
		s.log.Error("Error during modpack upload import: upload_session_id is empty")
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("upload_session_id is required"))
	}

	// Get temp file path and original filename from upload manager
	tempPath, originalFilename, err := s.uploadManager.GetTempPath(msg.UploadSessionId)
	if err != nil {
		s.log.Error("Failed to get upload session %s: %v", msg.UploadSessionId, err)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("upload session not found or not completed"))
	}

	var modpackID string

	cleanupOnError := func() {
		if tempPath != "" { // Remove tmp file just in case session wiped by goroutine
			if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
				s.log.Error("Failed to remove temp file %s: %v", tempPath, err)
			}
		}
		s.uploadManager.Cancel(msg.UploadSessionId)
		if modpackID != "" {
			if err := s.store.DeleteIndexedModpack(ctx, modpackID); err != nil {
				s.log.Error("Failed to cleanup modpack %s from database: %v", modpackID, err)
			}
		}
	}

	// Validate file extension
	if !strings.HasSuffix(strings.ToLower(originalFilename), ".zip") {
		cleanupOnError()
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("modpack must be a ZIP file"))
	}

	// Open zip file for reading
	zipReader, err := zip.OpenReader(tempPath)
	if err != nil {
		cleanupOnError()
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid ZIP file"))
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
		cleanupOnError()
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("no manifest.json found in modpack"))
	}

	// Read manifest
	manifestReader, err := manifestFile.Open()
	if err != nil {
		s.log.Error("Failed to open manifest: %v", err)
		cleanupOnError()
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read manifest"))
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
		cleanupOnError()
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid manifest.json"))
	}

	// Generate ID for the uploaded modpack
	modpackID = uuid.New().String()

	// Determine mod loader from manifest
	allLoaders := minecraft.GetAllModLoaders()
	var modLoader storage.ModLoader

	for _, ml := range manifest.Minecraft.ModLoaders {
		for _, iml := range allLoaders {
			if strings.Contains(ml.ID, iml.Name) {
				modLoader = storage.ModLoader(iml.Name)
				break
			}
		}
	}

	if modLoader == "" {
		modLoader = storage.ModLoaderCustom
	}

	javaVersion := docker.GetRequiredJavaVersion(manifest.Minecraft.Version, modLoader)
	dockerImage := docker.GetOptimalDockerTag(manifest.Minecraft.Version, modLoader, false)

	// Create database entry
	gameVersionsJSON, _ := json.Marshal([]string{manifest.Minecraft.Version})
	modLoadersJSON, _ := json.Marshal([]string{string(modLoader)})

	dbModpack := &storage.IndexedModpack{
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
		cleanupOnError()
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to store modpack"))
	}

	// Store zip and create directory for manual modpacks if it doesn't exist
	manualDir := filepath.Join(s.config.Storage.DataDir, "modpacks", "manual")
	if err := os.MkdirAll(manualDir, 0755); err != nil {
		s.log.Error("Failed to create manual modpacks directory: %v", err)
		cleanupOnError()
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to store modpack file"))
	}

	// Get file size before moving
	fileInfo, err := os.Stat(tempPath)
	if err != nil {
		s.log.Error("Failed to stat temp file: %v", err)
		cleanupOnError()
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to process upload"))
	}
	fileSize := fileInfo.Size()

	// Move file from temp location to storage
	destPath := filepath.Join(manualDir, modpackID+".zip")
	if err := os.Rename(tempPath, destPath); err != nil {
		// If rename fails (cross-device), fall back to copy
		if err := files.CopyFile(tempPath, destPath); err != nil {
			s.log.Error("Failed to move modpack file: %v", err)
			cleanupOnError()
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to store modpack file"))
		}
		os.Remove(tempPath)
	}

	// Cleanup the upload session
	s.uploadManager.CleanupSession(msg.UploadSessionId)

	// Create a file entry for the uploaded modpack
	dbFile := &storage.IndexedModpackFile{
		ID:               modpackID,
		ModpackID:        modpackID,
		DisplayName:      originalFilename,
		FileName:         originalFilename,
		FileDate:         time.Now(),
		FileLength:       fileSize,
		ReleaseType:      "1",      // Release
		DownloadURL:      destPath, // Store local path
		GameVersions:     string(gameVersionsJSON),
		ModLoader:        string(modLoader),
		ServerPackFileID: nil,
	}

	if err := s.store.UpsertIndexedModpackFile(ctx, dbFile); err != nil {
		s.log.Error("Failed to store modpack file entry: %v", err)
		// Don't fail the upload, just log the error
	}

	s.log.Info("Successfully imported uploaded modpack '%s' (id: %s) from session %s", manifest.Name, modpackID, msg.UploadSessionId)

	// Convert to proto format for response
	javaVersionInt, _ := strconv.Atoi(dbModpack.JavaVersion)

	protoModpack := &v1.IndexedModpack{
		Id:             dbModpack.ID,
		IndexerId:      dbModpack.IndexerID,
		Indexer:        dbModpack.Indexer,
		Name:           dbModpack.Name,
		Slug:           dbModpack.Slug,
		Summary:        dbModpack.Summary,
		Description:    dbModpack.Description,
		LogoUrl:        dbModpack.LogoURL,
		WebsiteUrl:     dbModpack.WebsiteURL,
		DownloadCount:  int32(dbModpack.DownloadCount),
		Categories:     dbModpack.Categories,
		GameVersions:   dbModpack.GameVersions,
		ModLoaders:     dbModpack.ModLoaders,
		LatestFileId:   dbModpack.LatestFileID,
		DateCreated:    timestamppb.New(dbModpack.DateCreated),
		DateModified:   timestamppb.New(dbModpack.DateModified),
		DateReleased:   timestamppb.New(dbModpack.DateReleased),
		McVersion:      dbModpack.MCVersion,
		JavaVersion:    int32(javaVersionInt),
		DockerImage:    dbModpack.DockerImage,
		RecommendedRam: int32(dbModpack.RecommendedRAM),
		IsFavorited:    false,
	}

	return connect.NewResponse(&v1.ImportUploadedModpackResponse{
		Modpack: protoModpack,
		Message: fmt.Sprintf("Modpack '%s' v%s by %s uploaded successfully", manifest.Name, manifest.Version, manifest.Author),
	}), nil
}

// DeleteModpack deletes a modpack
func (s *ModpackService) DeleteModpack(ctx context.Context, req *connect.Request[v1.DeleteModpackRequest]) (*connect.Response[v1.DeleteModpackResponse], error) {
	modpackID := req.Msg.Id

	// Get the modpack to verify it exists and is a manual modpack
	modpack, err := s.store.GetIndexedModpack(ctx, modpackID)
	if err != nil {
		s.log.Error("Failed to get modpack: %v", err)
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("modpack not found"))
	}

	// Only allow deletion of manual modpacks
	if modpack.Indexer != "manual" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("only custom uploaded modpacks can be deleted"))
	}

	// Check if any servers are using this modpack
	serversInUse, err := s.store.CheckModpackInUse(ctx, modpackID)
	if err != nil {
		s.log.Error("Failed to check modpack usage: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check modpack usage"))
	}

	if len(serversInUse) > 0 {
		serverNames := make([]string, len(serversInUse))
		for i, srv := range serversInUse {
			serverNames[i] = srv.Name
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			fmt.Errorf("cannot delete modpack: currently in use by servers: %s", strings.Join(serverNames, ", ")))
	}

	// Get modpack files
	files, err := s.store.GetIndexedModpackFiles(ctx, modpackID)
	if err != nil {
		s.log.Error("Failed to get modpack files: %v", err)
	}

	// Delete the physical ZIP file
	if len(files) > 0 && files[0].DownloadURL != "" {
		filePath := files[0].DownloadURL
		if err := os.Remove(filePath); err != nil {
			s.log.Error("Failed to delete modpack file %s: %v", filePath, err)
			// Continue with database deletion even if file deletion fails
		} else {
			s.log.Info("Deleted modpack file: %s", filePath)
		}
	}

	// Delete from database
	if err := s.store.DeleteIndexedModpack(ctx, modpackID); err != nil {
		s.log.Error("Failed to delete modpack from database: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete modpack"))
	}

	s.log.Info("Successfully deleted modpack %s (%s)", modpack.Name, modpackID)
	return connect.NewResponse(&v1.DeleteModpackResponse{
		Message: fmt.Sprintf("Modpack '%s' deleted successfully", modpack.Name),
	}), nil
}

// ToggleFavorite toggles modpack favorite status
func (s *ModpackService) ToggleFavorite(ctx context.Context, req *connect.Request[v1.ToggleFavoriteRequest]) (*connect.Response[v1.ToggleFavoriteResponse], error) {
	modpackID := req.Msg.Id

	// Check if already favorited
	isFavorited, err := s.store.IsModpackFavorited(ctx, modpackID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check favorite status"))
	}

	if isFavorited {
		// Remove favorite
		if err := s.store.RemoveModpackFavorite(ctx, modpackID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to remove favorite"))
		}
	} else {
		// Add favorite
		if err := s.store.AddModpackFavorite(ctx, modpackID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to add favorite"))
		}
	}

	return connect.NewResponse(&v1.ToggleFavoriteResponse{
		IsFavorited: !isFavorited,
		Message:     fmt.Sprintf("Modpack %s", map[bool]string{true: "unfavorited", false: "favorited"}[isFavorited]),
	}), nil
}

// ListFavorites lists favorite modpacks
func (s *ModpackService) ListFavorites(ctx context.Context, req *connect.Request[v1.ListFavoritesRequest]) (*connect.Response[v1.ListFavoritesResponse], error) {
	modpacks, err := s.store.ListFavoriteModpacks(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list favorites"))
	}

	// Convert to proto format
	protoModpacks := make([]*v1.IndexedModpack, len(modpacks))
	for i, modpack := range modpacks {
		javaVersionInt, _ := strconv.Atoi(modpack.JavaVersion)

		protoModpacks[i] = &v1.IndexedModpack{
			Id:             modpack.ID,
			IndexerId:      modpack.IndexerID,
			Indexer:        modpack.Indexer,
			Name:           modpack.Name,
			Slug:           modpack.Slug,
			Summary:        modpack.Summary,
			Description:    modpack.Description,
			LogoUrl:        modpack.LogoURL,
			WebsiteUrl:     modpack.WebsiteURL,
			DownloadCount:  int32(modpack.DownloadCount),
			Categories:     modpack.Categories,
			GameVersions:   modpack.GameVersions,
			ModLoaders:     modpack.ModLoaders,
			LatestFileId:   modpack.LatestFileID,
			DateCreated:    timestamppb.New(modpack.DateCreated),
			DateModified:   timestamppb.New(modpack.DateModified),
			DateReleased:   timestamppb.New(modpack.DateReleased),
			McVersion:      modpack.MCVersion,
			JavaVersion:    int32(javaVersionInt),
			DockerImage:    modpack.DockerImage,
			RecommendedRam: int32(modpack.RecommendedRAM),
			IsFavorited:    true, // All returned modpacks are favorited
		}
	}

	return connect.NewResponse(&v1.ListFavoritesResponse{
		Modpacks: protoModpacks,
	}), nil
}

// GetIndexerStatus gets indexer status
func (s *ModpackService) GetIndexerStatus(ctx context.Context, req *connect.Request[v1.GetIndexerStatusRequest]) (*connect.Response[v1.GetIndexerStatusResponse], error) {
	// Get global settings to check API key
	globalSettings, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get global settings"))
	}

	apiKeyConfigured := false
	if globalSettings.CFAPIKey != nil && *globalSettings.CFAPIKey != "" {
		apiKeyConfigured = true
	}

	// Get all modpacks and count
	modpacksByIndexer := make(map[string]int32)
	totalModpacks := int32(0)

	// Get all modpacks from database to count by indexer
	allModpacks, totalCount, err := s.store.ListIndexedModpacks(ctx, 0, 10000)
	if err == nil {
		totalModpacks = int32(totalCount)

		// Count by indexer
		for _, modpack := range allModpacks {
			if _, exists := modpacksByIndexer[modpack.Indexer]; !exists {
				modpacksByIndexer[modpack.Indexer] = 0
			}
			modpacksByIndexer[modpack.Indexer]++
		}
	}

	indexersAvailable := map[string]bool{
		"fuego":    apiKeyConfigured,
		"modrinth": true, // Modrinth doesn't require API key
		"manual":   true, // Manual uploads always available
	}

	return connect.NewResponse(&v1.GetIndexerStatusResponse{
		IndexersAvailable: indexersAvailable,
		TotalModpacks:     totalModpacks,
		ModpacksByIndexer: modpacksByIndexer,
	}), nil
}

// SyncModpackFiles syncs modpack files
func (s *ModpackService) SyncModpackFiles(ctx context.Context, req *connect.Request[v1.SyncModpackFilesRequest]) (*connect.Response[v1.SyncModpackFilesResponse], error) {
	modpackID := req.Msg.Id

	// Get the modpack to determine its indexer
	modpack, err := s.store.GetIndexedModpack(ctx, modpackID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("modpack not found"))
	}

	var indexerClient indexers.ModpackIndexer

	switch modpack.Indexer {
	case "fuego":
		// Get Fuego API key from global settings
		globalSettings, _, err := s.store.GetGlobalSettings(ctx)
		if err != nil {
			s.log.Error("Failed to get global settings: %v", err)
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get global settings"))
		}

		apiKey := ""
		if globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}

		if apiKey == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("fuego API key not configured in global settings"))
		}

		indexerClient = fuego.NewIndexer(apiKey, s.config)
	case "modrinth":
		// Modrinth doesn't require an API key for public operations
		indexerClient = modrinth.NewIndexer(s.config)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("unknown indexer: %s", modpack.Indexer))
	}

	// Get files from the indexer
	files, err := indexerClient.GetModpackFiles(ctx, modpack.IndexerID)
	if err != nil {
		s.log.Error("Failed to get modpack files: %v", err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get modpack files"))
	}

	// Store files in database
	synced := 0
	for _, file := range files {
		// Convert to JSON strings for storage
		gameVersionsJSON, _ := json.Marshal(file.GameVersions)

		dbFile := &storage.IndexedModpackFile{
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

	return connect.NewResponse(&v1.SyncModpackFilesResponse{
		SyncedCount: int32(synced),
		Message:     fmt.Sprintf("Synced %d of %d files", synced, len(files)),
	}), nil
}
