package services

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	"github.com/nickheyer/discopanel/pkg/logger"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/proto/discopanel/v1/discopanelv1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Compile-time check that ModpackService implements the interface
var _ discopanelv1connect.ModpackServiceHandler = (*ModpackService)(nil)

// ModpackService implements the Modpack service
type ModpackService struct {
	store  *storage.Store
	config *config.Config
	log    *logger.Logger
}

// NewModpackService creates a new modpack service
func NewModpackService(store *storage.Store, cfg *config.Config, log *logger.Logger) *ModpackService {
	return &ModpackService{
		store:  store,
		config: cfg,
		log:    log,
	}
}

// Helper: Find most recent Minecraft version from list
func findMostRecentMinecraftVersion(versions []string) string {
	for i := len(versions) - 1; i >= 0; i-- {
		hasLetter := false
		for _, ch := range versions[i] {
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
				hasLetter = true
				break
			}
		}
		if !hasLetter {
			return versions[i]
		}
	}
	if len(versions) > 0 {
		return versions[len(versions)-1]
	}
	return ""
}

// Helper: Convert DB modpack to proto
func dbModpackToProto(modpack *storage.IndexedModpack, isFavorited bool) *v1.IndexedModpack {
	if modpack == nil {
		return nil
	}
	return &v1.IndexedModpack{
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
		JavaVersion:    int32(modpack.JavaVersion),
		DockerImage:    modpack.DockerImage,
		RecommendedRam: int32(modpack.RecommendedRAM),
		IsFavorited:    isFavorited,
	}
}

// Helper: Convert DB modpack file to proto
func dbModpackFileToProto(file *storage.IndexedModpackFile) *v1.ModpackFile {
	if file == nil {
		return nil
	}
	var gameVersions []string
	json.Unmarshal([]byte(file.GameVersions), &gameVersions)

	return &v1.ModpackFile{
		Id:           file.ID,
		ModpackId:    file.ModpackID,
		DisplayName:  file.DisplayName,
		FileName:     file.FileName,
		FileDate:     timestamppb.New(file.FileDate),
		FileLength:   file.FileLength,
		ReleaseType:  file.ReleaseType,
		DownloadUrl:  file.DownloadURL,
		GameVersions: gameVersions,
		SortIndex:    0, // Will be set if needed
	}
}

// SearchModpacks searches for modpacks
func (s *ModpackService) SearchModpacks(ctx context.Context, req *connect.Request[v1.SearchModpacksRequest]) (*connect.Response[v1.SearchModpacksResponse], error) {
	msg := req.Msg
	page := int(msg.Page)
	if page < 1 {
		page = 1
	}
	pageSize := int(msg.PageSize)
	if pageSize <= 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	modpacks, total, err := s.store.SearchIndexedModpacks(ctx, msg.Query, msg.GameVersion, msg.ModLoader, msg.Indexer, offset, pageSize)
	if err != nil {
		s.log.Error("Failed to search modpacks: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to search modpacks"))
	}

	result := make([]*v1.IndexedModpack, len(modpacks))
	for i, modpack := range modpacks {
		isFavorited, _ := s.store.IsModpackFavorited(ctx, modpack.ID)
		result[i] = dbModpackToProto(modpack, isFavorited)
	}

	return connect.NewResponse(&v1.SearchModpacksResponse{
		Modpacks: result,
		Total:    int32(total),
		Page:     int32(page),
		PageSize: int32(pageSize),
	}), nil
}

// GetModpack gets a specific modpack
func (s *ModpackService) GetModpack(ctx context.Context, req *connect.Request[v1.GetModpackRequest]) (*connect.Response[v1.GetModpackResponse], error) {
	modpack, err := s.store.GetIndexedModpack(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("modpack not found"))
	}

	isFavorited, _ := s.store.IsModpackFavorited(ctx, req.Msg.Id)

	return connect.NewResponse(&v1.GetModpackResponse{
		Modpack: dbModpackToProto(modpack, isFavorited),
	}), nil
}

// GetModpackConfig gets modpack configuration
func (s *ModpackService) GetModpackConfig(ctx context.Context, req *connect.Request[v1.GetModpackConfigRequest]) (*connect.Response[v1.GetModpackConfigResponse], error) {
	modpack, err := s.store.GetIndexedModpack(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("modpack not found"))
	}

	modLoader := modpack.Indexer
	switch modpack.Indexer {
	case "manual":
		var modLoaders []string
		if err := json.Unmarshal([]byte(modpack.ModLoaders), &modLoaders); err == nil && len(modLoaders) > 0 {
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
		"memory":       fmt.Sprintf("%d", modpack.RecommendedRAM),
		"docker_image": modpack.DockerImage,
		"modpack_file": modpack.LatestFileID,
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
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get modpack files"))
	}

	result := make([]*v1.ModpackFile, len(files))
	for i, file := range files {
		result[i] = dbModpackFileToProto(file)
	}

	return connect.NewResponse(&v1.GetModpackFilesResponse{
		Files: result,
	}), nil
}

// GetModpackVersions gets modpack versions
func (s *ModpackService) GetModpackVersions(ctx context.Context, req *connect.Request[v1.GetModpackVersionsRequest]) (*connect.Response[v1.GetModpackVersionsResponse], error) {
	modpack, err := s.store.GetIndexedModpack(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("modpack not found"))
	}

	var indexerClient indexers.ModpackIndexer
	switch modpack.Indexer {
	case "fuego":
		globalSettings, _, err := s.store.GetGlobalSettings(ctx)
		if err != nil || globalSettings == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get global settings"))
		}
		apiKey := ""
		if globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}
		if apiKey == "" {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("CurseForge API key not configured"))
		}
		indexerClient = fuego.NewIndexer(apiKey)
	case "modrinth":
		indexerClient = modrinth.NewIndexer()
	case "manual":
		return connect.NewResponse(&v1.GetModpackVersionsResponse{
			Versions: []*v1.Version{},
		}), nil
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown indexer: "+modpack.Indexer))
	}

	files, err := indexerClient.GetModpackFiles(ctx, modpack.IndexerID)
	if err != nil {
		s.log.Error("Failed to get modpack files from %s: %v", modpack.Indexer, err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get modpack versions"))
	}

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

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].SortIndex < versions[j].SortIndex
	})

	return connect.NewResponse(&v1.GetModpackVersionsResponse{
		Versions: versions,
	}), nil
}

// SyncModpacks syncs modpacks from external sources
func (s *ModpackService) SyncModpacks(ctx context.Context, req *connect.Request[v1.SyncModpacksRequest]) (*connect.Response[v1.SyncModpacksResponse], error) {
	msg := req.Msg

	indexerName := msg.Indexer
	if indexerName == "" {
		indexerName = "fuego"
	}

	var indexerClient indexers.ModpackIndexer

	switch indexerName {
	case "fuego":
		globalSettings, _, err := s.store.GetGlobalSettings(ctx)
		if err != nil {
			s.log.Error("Failed to get global settings: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get global settings"))
		}

		apiKey := ""
		if globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}
		if apiKey == "" {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Fuego API key not configured in global settings"))
		}
		indexerClient = fuego.NewIndexer(apiKey)
	case "modrinth":
		indexerClient = modrinth.NewIndexer()
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown indexer: "+indexerName))
	}

	searchResp, err := indexerClient.SearchModpacks(ctx, msg.Query, msg.GameVersion, msg.ModLoader, 0, 50)
	if err != nil {
		s.log.Error("Failed to search %s: %v", indexerName, err)
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to search %s: %w", indexerName, err))
	}

	synced := 0
	for _, modpack := range searchResp.Modpacks {
		categoriesJSON, _ := json.Marshal(modpack.Categories)
		gameVersionsJSON, _ := json.Marshal(modpack.GameVersions)
		modLoadersJSON, _ := json.Marshal(modpack.ModLoaders)

		mcVersion := findMostRecentMinecraftVersion(modpack.GameVersions)

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
			MCVersion:     mcVersion,
			JavaVersion:   javaVersion,
			DockerImage:   dockerImage,
			RecommendedRAM: 6144,
		}

		if err := s.store.UpsertIndexedModpack(ctx, dbModpack); err != nil {
			s.log.Error("Failed to store modpack %s: %v", modpack.ID, err)
			continue
		}
		synced++
	}

	return connect.NewResponse(&v1.SyncModpacksResponse{
		SyncedCount: int32(synced),
		Message:     fmt.Sprintf("Synced %d modpacks from %s", synced, indexerName),
	}), nil
}

// UploadModpack uploads a modpack
func (s *ModpackService) UploadModpack(ctx context.Context, req *connect.Request[v1.UploadModpackRequest]) (*connect.Response[v1.UploadModpackResponse], error) {
	msg := req.Msg

	if msg.Filename == "" || len(msg.Content) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("filename and content are required"))
	}

	if !strings.HasSuffix(strings.ToLower(msg.Filename), ".zip") {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("modpack must be a ZIP file"))
	}

	// Open zip from bytes
	zipReader, err := zip.NewReader(bytes.NewReader(msg.Content), int64(len(msg.Content)))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid ZIP file"))
	}

	// Look for manifest.json
	var manifestFile *zip.File
	for _, f := range zipReader.File {
		if f.Name == "manifest.json" || strings.HasSuffix(f.Name, "/manifest.json") {
			manifestFile = f
			break
		}
	}

	if manifestFile == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("no manifest.json found in modpack"))
	}

	// Read manifest
	manifestReader, err := manifestFile.Open()
	if err != nil {
		s.log.Error("Failed to open manifest: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to read manifest"))
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
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid manifest.json"))
	}

	// Generate ID
	modpackID := uuid.New().String()

	// Determine mod loader
	var modLoader storage.ModLoader
	for _, ml := range manifest.Minecraft.ModLoaders {
		if strings.Contains(ml.ID, "neoforge") {
			modLoader = storage.ModLoaderNeoForge
			break
		} else if strings.Contains(ml.ID, "fabric") {
			modLoader = storage.ModLoaderFabric
			break
		} else if strings.Contains(ml.ID, "forge") {
			modLoader = storage.ModLoaderForge
			break
		} else if strings.Contains(ml.ID, "quilt") {
			modLoader = storage.ModLoaderQuilt
			break
		} else {
			modLoader = storage.ModLoaderVanilla
		}
	}

	javaVersion := docker.GetRequiredJavaVersion(manifest.Minecraft.Version, modLoader)
	dockerImage := docker.GetOptimalDockerTag(manifest.Minecraft.Version, modLoader, false)

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
		LogoURL:        "",
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
		RecommendedRAM: 6144,
	}

	if err := s.store.UpsertIndexedModpack(ctx, dbModpack); err != nil {
		s.log.Error("Failed to store modpack: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to store modpack"))
	}

	// Store ZIP file
	manualDir := filepath.Join(s.config.Storage.DataDir, "modpacks", "manual")
	if err := os.MkdirAll(manualDir, 0755); err != nil {
		s.log.Error("Failed to create manual modpacks directory: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to store modpack file"))
	}

	destPath := filepath.Join(manualDir, modpackID+".zip")
	if err := os.WriteFile(destPath, msg.Content, 0644); err != nil {
		s.log.Error("Failed to write modpack file: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to store modpack file"))
	}

	// Create file entry
	dbFile := &storage.IndexedModpackFile{
		ID:               modpackID,
		ModpackID:        modpackID,
		DisplayName:      msg.Filename,
		FileName:         msg.Filename,
		FileDate:         time.Now(),
		FileLength:       int64(len(msg.Content)),
		ReleaseType:      "1",
		DownloadURL:      destPath,
		GameVersions:     string(gameVersionsJSON),
		ModLoader:        string(modLoader),
		ServerPackFileID: nil,
	}

	if err := s.store.UpsertIndexedModpackFile(ctx, dbFile); err != nil {
		s.log.Error("Failed to store modpack file entry: %v", err)
	}

	isFavorited, _ := s.store.IsModpackFavorited(ctx, modpackID)

	return connect.NewResponse(&v1.UploadModpackResponse{
		Modpack: dbModpackToProto(dbModpack, isFavorited),
		Message: "Modpack uploaded successfully",
	}), nil
}

// DeleteModpack deletes a modpack
func (s *ModpackService) DeleteModpack(ctx context.Context, req *connect.Request[v1.DeleteModpackRequest]) (*connect.Response[v1.DeleteModpackResponse], error) {
	modpack, err := s.store.GetIndexedModpack(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("modpack not found"))
	}

	if modpack.Indexer != "manual" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only custom uploaded modpacks can be deleted"))
	}

	serversInUse, err := s.store.CheckModpackInUse(ctx, req.Msg.Id)
	if err != nil {
		s.log.Error("Failed to check modpack usage: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to check modpack usage"))
	}

	if len(serversInUse) > 0 {
		serverNames := make([]string, len(serversInUse))
		for i, srv := range serversInUse {
			serverNames[i] = srv.Name
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("cannot delete modpack: currently in use by servers: %s", strings.Join(serverNames, ", ")))
	}

	files, err := s.store.GetIndexedModpackFiles(ctx, req.Msg.Id)
	if err != nil {
		s.log.Error("Failed to get modpack files: %v", err)
	}

	if len(files) > 0 && files[0].DownloadURL != "" {
		filePath := files[0].DownloadURL
		if err := os.Remove(filePath); err != nil {
			s.log.Error("Failed to delete modpack file %s: %v", filePath, err)
		} else {
			s.log.Info("Deleted modpack file: %s", filePath)
		}
	}

	if err := s.store.DeleteIndexedModpack(ctx, req.Msg.Id); err != nil {
		s.log.Error("Failed to delete modpack from database: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to delete modpack"))
	}

	s.log.Info("Successfully deleted modpack %s (%s)", modpack.Name, req.Msg.Id)
	return connect.NewResponse(&v1.DeleteModpackResponse{
		Message: "Modpack deleted successfully",
	}), nil
}

// ToggleFavorite toggles modpack favorite status
func (s *ModpackService) ToggleFavorite(ctx context.Context, req *connect.Request[v1.ToggleFavoriteRequest]) (*connect.Response[v1.ToggleFavoriteResponse], error) {
	isFavorited, err := s.store.IsModpackFavorited(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to check favorite status"))
	}

	if isFavorited {
		if err := s.store.RemoveModpackFavorite(ctx, req.Msg.Id); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to remove favorite"))
		}
	} else {
		if err := s.store.AddModpackFavorite(ctx, req.Msg.Id); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to add favorite"))
		}
	}

	return connect.NewResponse(&v1.ToggleFavoriteResponse{
		IsFavorited: !isFavorited,
		Message:     "Favorite status updated",
	}), nil
}

// ListFavorites lists favorite modpacks
func (s *ModpackService) ListFavorites(ctx context.Context, req *connect.Request[v1.ListFavoritesRequest]) (*connect.Response[v1.ListFavoritesResponse], error) {
	modpacks, err := s.store.ListFavoriteModpacks(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to list favorites"))
	}

	result := make([]*v1.IndexedModpack, len(modpacks))
	for i, modpack := range modpacks {
		result[i] = dbModpackToProto(modpack, true)
	}

	return connect.NewResponse(&v1.ListFavoritesResponse{
		Modpacks: result,
	}), nil
}

// GetIndexerStatus gets indexer status
func (s *ModpackService) GetIndexerStatus(ctx context.Context, req *connect.Request[v1.GetIndexerStatusRequest]) (*connect.Response[v1.GetIndexerStatusResponse], error) {
	globalSettings, _, err := s.store.GetGlobalSettings(ctx)
	if err != nil {
		s.log.Error("Failed to get global settings: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get global settings"))
	}

	apiKeyConfigured := false
	if globalSettings.CFAPIKey != nil && *globalSettings.CFAPIKey != "" {
		apiKeyConfigured = true
	}

	indexersAvailable := map[string]bool{
		"fuego":    apiKeyConfigured,
		"modrinth": true,
	}

	return connect.NewResponse(&v1.GetIndexerStatusResponse{
		IndexersAvailable: indexersAvailable,
		TotalModpacks:     0,
		ModpacksByIndexer: map[string]int32{},
	}), nil
}

// SyncModpackFiles syncs modpack files from external source
func (s *ModpackService) SyncModpackFiles(ctx context.Context, req *connect.Request[v1.SyncModpackFilesRequest]) (*connect.Response[v1.SyncModpackFilesResponse], error) {
	modpack, err := s.store.GetIndexedModpack(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("modpack not found"))
	}

	var indexerClient indexers.ModpackIndexer

	switch modpack.Indexer {
	case "fuego":
		globalSettings, _, err := s.store.GetGlobalSettings(ctx)
		if err != nil {
			s.log.Error("Failed to get global settings: %v", err)
			return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get global settings"))
		}

		apiKey := ""
		if globalSettings.CFAPIKey != nil {
			apiKey = *globalSettings.CFAPIKey
		}
		if apiKey == "" {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("Fuego API key not configured in global settings"))
		}
		indexerClient = fuego.NewIndexer(apiKey)
	case "modrinth":
		indexerClient = modrinth.NewIndexer()
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown indexer: "+modpack.Indexer))
	}

	files, err := indexerClient.GetModpackFiles(ctx, modpack.IndexerID)
	if err != nil {
		s.log.Error("Failed to get modpack files: %v", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to get modpack files"))
	}

	synced := 0
	for _, file := range files {
		gameVersionsJSON, _ := json.Marshal(file.GameVersions)

		dbFile := &storage.IndexedModpackFile{
			ID:               file.ID,
			ModpackID:        req.Msg.Id,
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
		Message:     fmt.Sprintf("Synced %d files", synced),
	}), nil
}