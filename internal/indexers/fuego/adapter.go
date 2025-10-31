package fuego

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/nickheyer/discopanel/internal/indexers"
)

// Implements ModpackIndexer
var _ indexers.ModpackIndexer = (*FuegoIndexer)(nil)

// Adapts the Fuego client to the ModpackIndexer interface
type FuegoIndexer struct {
	client *Client
}

// Creates a new Fuego indexer
func NewIndexer(apiKey string) *FuegoIndexer {
	return &FuegoIndexer{
		client: NewClient(apiKey),
	}
}

// GetIndexerName returns the name of this indexer
func (f *FuegoIndexer) GetIndexerName() string {
	return "fuego"
}

// Search for modpacks
func (f *FuegoIndexer) SearchModpacks(ctx context.Context, query string, gameVersion string, modLoader string, offset, limit int) (*indexers.SearchResult, error) {
	// Convert mod loader string to Fuego type
	modLoaderType := ModLoaderAny
	switch strings.ToLower(modLoader) {
	case "forge":
		modLoaderType = ModLoaderForge
	case "fabric":
		modLoaderType = ModLoaderFabric
	case "neoforge":
		modLoaderType = ModLoaderNeoForge
	case "quilt":
		modLoaderType = ModLoaderQuilt
	}

	// Calculate page index from offset
	pageIndex := offset / limit

	// Search using Fuego API
	resp, err := f.client.SearchModpacks(ctx, query, gameVersion, modLoaderType, pageIndex, limit)
	if err != nil {
		return nil, err
	}

	// Convert Fuego modpacks to generic modpacks
	modpacks := make([]indexers.Modpack, len(resp.Data))
	for i, fm := range resp.Data {
		modpacks[i] = f.convertModpack(fm)
	}

	return &indexers.SearchResult{
		Modpacks:   modpacks,
		TotalCount: resp.Pagination.TotalCount,
		PageSize:   limit,
		Offset:     offset,
	}, nil
}

// Get a specific modpack
func (f *FuegoIndexer) GetModpack(ctx context.Context, modpackID string) (*indexers.Modpack, error) {
	// Convert string ID to int
	id, err := strconv.Atoi(modpackID)
	if err != nil {
		return nil, fmt.Errorf("invalid modpack ID: %s", modpackID)
	}

	modpack, err := f.client.GetModpack(ctx, id)
	if err != nil {
		return nil, err
	}

	result := f.convertModpack(*modpack)
	return &result, nil
}

// Get files for a modpack
func (f *FuegoIndexer) GetModpackFiles(ctx context.Context, modpackID string) ([]indexers.ModpackFile, error) {
	// Convert string ID to int
	id, err := strconv.Atoi(modpackID)
	if err != nil {
		return nil, fmt.Errorf("invalid modpack ID: %s", modpackID)
	}

	files, err := f.client.GetModpackFiles(ctx, id)
	if err != nil {
		return nil, err
	}

	// Convert Fuego files to generic files
	result := make([]indexers.ModpackFile, len(files))
	for i, file := range files {
		converted := f.convertFile(file, modpackID)
		converted.SortIndex = i
		result[i] = converted
	}

	return result, nil
}

// Convert a Fuego modpack to a generic modpack
func (f *FuegoIndexer) convertModpack(fm Modpack) indexers.Modpack {
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
	gameVersions = deduplicateStrings(gameVersions)
	modLoaders = deduplicateStrings(modLoaders)

	logoURL := ""
	if fm.Logo.ThumbnailURL != "" {
		logoURL = fm.Logo.ThumbnailURL
	}

	return indexers.Modpack{
		ID:            fmt.Sprintf("fuego-%d", fm.ID),
		IndexerID:     strconv.Itoa(fm.ID),
		Indexer:       "fuego",
		Name:          fm.Name,
		Slug:          fm.Slug,
		Summary:       fm.Summary,
		Description:   fm.Summary, // Fuego doesn't provide separate description in search
		LogoURL:       logoURL,
		WebsiteURL:    fm.Links.WebsiteURL,
		DownloadCount: int64(fm.DownloadCount),
		Categories:    categories,
		GameVersions:  gameVersions,
		ModLoaders:    modLoaders,
		LatestFileID:  strconv.Itoa(fm.MainFileID),
		DateCreated:   fm.DateCreated,
		DateModified:  fm.DateModified,
		DateReleased:  fm.DateReleased,
	}
}

// Converts a Fuego file to a generic file
func (f *FuegoIndexer) convertFile(file File, modpackID string) indexers.ModpackFile {
	// Determine release type
	releaseType := "release"
	switch file.ReleaseType {
	case 2:
		releaseType = "beta"
	case 3:
		releaseType = "alpha"
	}

	// Extract primary mod loader
	modLoader := ""
	for _, gv := range file.GameVersions {
		loader := strings.ToLower(gv)
		if loader == "forge" || loader == "fabric" || loader == "quilt" || loader == "neoforge" {
			modLoader = loader
			break
		}
	}

	serverPackID := ""
	if file.ServerPackFileID != nil {
		serverPackID = strconv.Itoa(*file.ServerPackFileID)
	}

	return indexers.ModpackFile{
		ID:               strconv.Itoa(file.ID),
		ModpackID:        modpackID,
		DisplayName:      file.DisplayName,
		FileName:         file.FileName,
		FileDate:         file.FileDate,
		FileLength:       file.FileLength,
		ReleaseType:      releaseType,
		DownloadURL:      file.DownloadURL,
		GameVersions:     file.GameVersions,
		ModLoader:        modLoader,
		ServerPackFileID: &serverPackID,
	}
}

func deduplicateStrings(strings []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, str := range strings {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}

	return result
}
