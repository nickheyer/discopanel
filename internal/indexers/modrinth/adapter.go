package modrinth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nickheyer/discopanel/internal/indexers"
)

// Ensure ModrinthIndexer implements ModpackIndexer
var _ indexers.ModpackIndexer = (*ModrinthIndexer)(nil)

// ModrinthIndexer adapts the Modrinth client to the ModpackIndexer interface
type ModrinthIndexer struct {
	client *Client
}

// NewIndexer creates a new Modrinth indexer
// Note: Modrinth API does not require an API key for public operations
func NewIndexer() *ModrinthIndexer {
	return &ModrinthIndexer{
		client: NewClient(),
	}
}

// GetIndexerName returns the name of this indexer
func (m *ModrinthIndexer) GetIndexerName() string {
	return "modrinth"
}

// SearchModpacks searches for modpacks
func (m *ModrinthIndexer) SearchModpacks(ctx context.Context, query string, gameVersion string, modLoader string, offset, limit int) (*indexers.SearchResult, error) {
	// Search using Modrinth API
	resp, err := m.client.SearchModpacks(ctx, query, gameVersion, modLoader, offset, limit)
	if err != nil {
		return nil, err
	}

	// Convert Modrinth projects to generic modpacks
	modpacks := make([]indexers.Modpack, len(resp.Hits))
	for i, project := range resp.Hits {
		modpacks[i] = m.convertSearchProject(project)
	}

	return &indexers.SearchResult{
		Modpacks:   modpacks,
		TotalCount: resp.TotalHits,
		PageSize:   limit,
		Offset:     offset,
	}, nil
}

// GetModpack retrieves a specific modpack
func (m *ModrinthIndexer) GetModpack(ctx context.Context, modpackID string) (*indexers.Modpack, error) {
	// Get full project details
	project, err := m.client.GetModpack(ctx, modpackID)
	if err != nil {
		return nil, err
	}

	result := m.convertProject(*project)
	return &result, nil
}

// GetModpackFiles retrieves files for a modpack
func (m *ModrinthIndexer) GetModpackFiles(ctx context.Context, modpackID string) ([]indexers.ModpackFile, error) {
	versions, err := m.client.GetModpackVersions(ctx, modpackID)
	if err != nil {
		return nil, err
	}

	// Convert Modrinth versions to generic files
	// Each version can have multiple files, but we treat the primary file as the main one
	result := make([]indexers.ModpackFile, 0, len(versions))
	for _, version := range versions {
		if len(version.Files) > 0 {
			// Find primary file or use first one
			var primaryFile *File
			for i := range version.Files {
				if version.Files[i].Primary {
					primaryFile = &version.Files[i]
					break
				}
			}
			if primaryFile == nil {
				primaryFile = &version.Files[0]
			}

			result = append(result, m.convertVersionToFile(version, *primaryFile, modpackID))
		}
	}

	return result, nil
}

// convertSearchProject converts a Modrinth search project to a generic modpack
func (m *ModrinthIndexer) convertSearchProject(project Project) indexers.Modpack {
	// Parse dates
	dateCreated, _ := time.Parse(time.RFC3339, project.DateCreated)
	dateModified, _ := time.Parse(time.RFC3339, project.DateModified)

	// Extract mod loaders from categories (Modrinth puts loaders in categories)
	modLoaders := []string{}
	for _, cat := range project.Categories {
		lower := strings.ToLower(cat)
		if lower == "forge" || lower == "fabric" || lower == "quilt" || lower == "neoforge" {
			modLoaders = append(modLoaders, lower)
		}
	}

	// Use display_categories if available, otherwise use categories
	categories := project.DisplayCategories
	if len(categories) == 0 {
		categories = project.Categories
	}

	return indexers.Modpack{
		ID:            fmt.Sprintf("modrinth-%s", project.ProjectID),
		IndexerID:     project.ProjectID,
		Indexer:       "modrinth",
		Name:          project.Title,
		Slug:          project.Slug,
		Summary:       project.Description,
		Description:   project.Description,
		LogoURL:       project.IconURL,
		WebsiteURL:    fmt.Sprintf("https://modrinth.com/modpack/%s", project.Slug),
		DownloadCount: project.Downloads,
		Categories:    categories,
		GameVersions:  project.Versions,
		ModLoaders:    modLoaders,
		LatestFileID:  project.LatestVersion,
		DateCreated:   dateCreated,
		DateModified:  dateModified,
		DateReleased:  dateCreated, // Modrinth doesn't have separate release date in search
	}
}

// convertProject converts a full Modrinth project to a generic modpack
func (m *ModrinthIndexer) convertProject(project ProjectDetails) indexers.Modpack {
	// Parse dates
	dateCreated, _ := time.Parse(time.RFC3339, project.Published)
	dateModified, _ := time.Parse(time.RFC3339, project.Updated)

	// Use project.Loaders for mod loaders
	modLoaders := make([]string, len(project.Loaders))
	for i, loader := range project.Loaders {
		modLoaders[i] = strings.ToLower(loader)
	}

	// Use display categories (non-loader categories)
	categories := []string{}
	for _, cat := range project.Categories {
		lower := strings.ToLower(cat)
		// Skip loader categories
		if lower != "forge" && lower != "fabric" && lower != "quilt" && lower != "neoforge" {
			categories = append(categories, cat)
		}
	}
	if len(project.AdditionalCategories) > 0 {
		categories = append(categories, project.AdditionalCategories...)
	}

	// Get latest version ID
	latestVersionID := ""
	if len(project.Versions) > 0 {
		latestVersionID = project.Versions[0] // Modrinth returns versions in latest-first order
	}

	return indexers.Modpack{
		ID:            fmt.Sprintf("modrinth-%s", project.ID),
		IndexerID:     project.ID,
		Indexer:       "modrinth",
		Name:          project.Title,
		Slug:          project.Slug,
		Summary:       project.Description,
		Description:   project.Body, // Full description from project body
		LogoURL:       project.IconURL,
		WebsiteURL:    fmt.Sprintf("https://modrinth.com/modpack/%s", project.Slug),
		DownloadCount: project.Downloads,
		Categories:    categories,
		GameVersions:  project.GameVersions,
		ModLoaders:    modLoaders,
		LatestFileID:  latestVersionID,
		DateCreated:   dateCreated,
		DateModified:  dateModified,
		DateReleased:  dateCreated,
	}
}

// convertVersionToFile converts a Modrinth version to a generic modpack file
func (m *ModrinthIndexer) convertVersionToFile(version Version, file File, modpackID string) indexers.ModpackFile {
	// Parse date
	fileDate, _ := time.Parse(time.RFC3339, version.DatePublished)

	// Convert version type to release type
	releaseType := "release"
	switch strings.ToLower(version.VersionType) {
	case "beta":
		releaseType = "beta"
	case "alpha":
		releaseType = "alpha"
	}

	// Get primary mod loader
	modLoader := ""
	if len(version.Loaders) > 0 {
		modLoader = strings.ToLower(version.Loaders[0])
	}

	// Modrinth doesn't have separate server pack files in the same way CurseForge does
	// But versions can have multiple files - we're using the primary one
	var serverPackID *string

	return indexers.ModpackFile{
		ID:               version.ID,
		ModpackID:        modpackID,
		DisplayName:      version.Name,
		FileName:         file.Filename,
		FileDate:         fileDate,
		FileLength:       file.Size,
		ReleaseType:      releaseType,
		DownloadURL:      file.URL,
		GameVersions:     version.GameVersions,
		ModLoader:        modLoader,
		ServerPackFileID: serverPackID,
	}
}
