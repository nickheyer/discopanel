package fuego

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/nickheyer/discopanel/pkg/indexers"
	"github.com/nickheyer/discopanel/pkg/minecraft"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	"github.com/nickheyer/discopanel/pkg/utils"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func init() {
	indexers.RegisterIndexer("fuego", func(apiKey string, userAgent string) indexers.ModpackIndexer {
		return NewIndexer(apiKey, userAgent)
	})
}

// Implements ModpackIndexer
var _ indexers.ModpackIndexer = (*FuegoIndexer)(nil)

// Adapts the Fuego client to the ModpackIndexer interface
type FuegoIndexer struct {
	client *Client
}

// Creates a new Fuego indexer
func NewIndexer(apiKey string, userAgent string) *FuegoIndexer {
	return &FuegoIndexer{
		client: NewClient(apiKey, userAgent),
	}
}

// Returns the name of this indexer
func (f *FuegoIndexer) GetIndexerName() string {
	return "fuego"
}

// Maps a loader name onto the CurseForge loader type
func loaderType(modLoader string) ModLoaderType {
	switch strings.ToLower(modLoader) {
	case "forge":
		return ModLoaderForge
	case "fabric":
		return ModLoaderFabric
	case "neoforge":
		return ModLoaderNeoForge
	case "quilt":
		return ModLoaderQuilt
	default:
		return ModLoaderAny
	}
}

// Search for modpacks
func (f *FuegoIndexer) SearchModpacks(ctx context.Context, query string, gameVersion string, modLoader string, offset, limit int) (*indexers.SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	// CurseForge index counts items, not pages
	resp, err := f.client.SearchModpacks(ctx, query, gameVersion, loaderType(modLoader), offset, limit)
	if err != nil {
		return nil, err
	}

	// Normalizes Fuego modpacks into proto rows
	modpacks := make([]*v1.IndexedModpack, len(resp.Data))
	for i, fm := range resp.Data {
		// Extract categories
		categories := make([]string, len(fm.Categories))
		for j, cat := range fm.Categories {
			categories[j] = cat.Name
		}

		// Extract game versions and mod loaders from files
		gameVersions := []string{}
		modLoaders := []string{}

		for _, file := range fm.LatestFiles {
			gameVersions = append(gameVersions, file.GameVersions...)
		}

		for _, fileIndex := range fm.LatestFilesIndexes {
			if fileIndex.ModLoader != nil {
				switch *fileIndex.ModLoader {
				case 1:
					modLoaders = append(modLoaders, "forge")
				case 4:
					modLoaders = append(modLoaders, "fabric")
				case 5:
					modLoaders = append(modLoaders, "quilt")
				case 6:
					modLoaders = append(modLoaders, "neoforge")
				}
			}
		}

		// Deduplicate
		gameVersions = utils.DeduplicateStrings(gameVersions)
		modLoaders = utils.DeduplicateStrings(modLoaders)

		logoURL := ""
		if fm.Logo.ThumbnailURL != "" {
			logoURL = fm.Logo.ThumbnailURL
		}

		// Encodes slice columns as JSON strings
		categoriesJSON, _ := json.Marshal(categories)
		gameVersionsJSON, _ := json.Marshal(gameVersions)
		modLoadersJSON, _ := json.Marshal(modLoaders)

		modpacks[i] = &v1.IndexedModpack{
			Id:            fmt.Sprintf("fuego-%d", fm.ID),
			IndexerId:     strconv.Itoa(fm.ID),
			Indexer:       "fuego",
			Name:          fm.Name,
			Slug:          fm.Slug,
			Summary:       fm.Summary,
			Description:   fm.Summary, // Fuego doesn't provide separate description in search
			LogoUrl:       logoURL,
			WebsiteUrl:    fm.Links.WebsiteURL,
			DownloadCount: int32(fm.DownloadCount),
			Categories:    string(categoriesJSON),
			GameVersions:  string(gameVersionsJSON),
			ModLoaders:    string(modLoadersJSON),
			LatestFileId:  strconv.Itoa(fm.MainFileID),
			DateCreated:   timestamppb.New(fm.DateCreated),
			DateModified:  timestamppb.New(fm.DateModified),
			DateReleased:  timestamppb.New(fm.DateReleased),
		}
	}

	return &indexers.SearchResult{
		Modpacks:   modpacks,
		TotalCount: resp.Pagination.TotalCount,
		PageSize:   limit,
		Offset:     offset,
	}, nil
}

// Get a specific modpack
func (f *FuegoIndexer) GetModpack(ctx context.Context, modpackID string) (*v1.IndexedModpack, error) {
	// Convert string ID to int
	id, err := strconv.Atoi(modpackID)
	if err != nil {
		return nil, fmt.Errorf("invalid modpack ID: %s", modpackID)
	}

	fm, err := f.client.GetModpack(ctx, id)
	if err != nil {
		return nil, err
	}

	// Extract categories
	categories := make([]string, len(fm.Categories))
	for i, cat := range fm.Categories {
		categories[i] = cat.Name
	}

	// Extract game versions and mod loaders from files
	gameVersions := []string{}
	modLoaders := []string{}

	for _, file := range fm.LatestFiles {
		gameVersions = append(gameVersions, file.GameVersions...)
	}

	for _, fileIndex := range fm.LatestFilesIndexes {
		if fileIndex.ModLoader != nil {
			switch *fileIndex.ModLoader {
			case 1:
				modLoaders = append(modLoaders, "forge")
			case 4:
				modLoaders = append(modLoaders, "fabric")
			case 5:
				modLoaders = append(modLoaders, "quilt")
			case 6:
				modLoaders = append(modLoaders, "neoforge")
			}
		}
	}

	// Deduplicate
	gameVersions = utils.DeduplicateStrings(gameVersions)
	modLoaders = utils.DeduplicateStrings(modLoaders)

	logoURL := ""
	if fm.Logo.ThumbnailURL != "" {
		logoURL = fm.Logo.ThumbnailURL
	}

	// Encodes slice columns as JSON strings
	categoriesJSON, _ := json.Marshal(categories)
	gameVersionsJSON, _ := json.Marshal(gameVersions)
	modLoadersJSON, _ := json.Marshal(modLoaders)

	return &v1.IndexedModpack{
		Id:            fmt.Sprintf("fuego-%d", fm.ID),
		IndexerId:     strconv.Itoa(fm.ID),
		Indexer:       "fuego",
		Name:          fm.Name,
		Slug:          fm.Slug,
		Summary:       fm.Summary,
		Description:   fm.Summary, // Fuego doesn't provide separate description in search
		LogoUrl:       logoURL,
		WebsiteUrl:    fm.Links.WebsiteURL,
		DownloadCount: int32(fm.DownloadCount),
		Categories:    string(categoriesJSON),
		GameVersions:  string(gameVersionsJSON),
		ModLoaders:    string(modLoadersJSON),
		LatestFileId:  strconv.Itoa(fm.MainFileID),
		DateCreated:   timestamppb.New(fm.DateCreated),
		DateModified:  timestamppb.New(fm.DateModified),
		DateReleased:  timestamppb.New(fm.DateReleased),
	}, nil
}

// Get files for a modpack
func (f *FuegoIndexer) GetModpackFiles(ctx context.Context, modpackID string, gameVersion string, modLoader string) ([]*v1.IndexedModpackFile, error) {
	// Convert string ID to int
	id, err := strconv.Atoi(modpackID)
	if err != nil {
		return nil, fmt.Errorf("invalid modpack ID: %s", modpackID)
	}

	files, err := f.client.GetModpackFiles(ctx, id, gameVersion, loaderType(modLoader))
	if err != nil {
		return nil, err
	}

	// Normalizes Fuego files into proto rows
	result := make([]*v1.IndexedModpackFile, len(files))
	for i, file := range files {
		// Determine release type
		releaseType := "release"
		switch file.ReleaseType {
		case 2:
			releaseType = "beta"
		case 3:
			releaseType = "alpha"
		}

		// Extract primary mod loader w/ best effort score matching
		fileLoader := ""
		if loader, ok := minecraft.DetectModpackLoader(file.GameVersions...); ok {
			fileLoader = loader.Name()
		}

		serverPackID := ""
		if file.ServerPackFileID != nil {
			serverPackID = strconv.Itoa(*file.ServerPackFileID)
		}

		gameVersionsJSON, _ := json.Marshal(file.GameVersions)

		result[i] = &v1.IndexedModpackFile{
			Id:               strconv.Itoa(file.ID),
			ModpackId:        fmt.Sprintf("fuego-%s", modpackID),
			DisplayName:      file.DisplayName,
			FileName:         file.FileName,
			FileDate:         timestamppb.New(file.FileDate),
			FileLength:       file.FileLength,
			ReleaseType:      releaseType,
			DownloadUrl:      file.DownloadURL,
			GameVersions:     string(gameVersionsJSON),
			ModLoader:        fileLoader,
			ServerPackFileId: &serverPackID,
		}
	}

	return result, nil
}
