package indexers

import (
	"context"
	"time"
)

// ModpackIndexer defines the interface for modpack indexing services
type ModpackIndexer interface {
	// SearchModpacks searches for modpacks with the given criteria
	SearchModpacks(ctx context.Context, query string, gameVersion string, modLoader string, offset, limit int) (*SearchResult, error)
	
	// GetModpack retrieves detailed information about a specific modpack
	GetModpack(ctx context.Context, modpackID string) (*Modpack, error)
	
	// GetModpackFiles retrieves all available files for a modpack
	GetModpackFiles(ctx context.Context, modpackID string) ([]ModpackFile, error)
	
	// GetIndexerName returns the name of this indexer (e.g., "fuego", "modrinth")
	GetIndexerName() string
}

// SearchResult contains the results of a modpack search
type SearchResult struct {
	Modpacks   []Modpack
	TotalCount int
	PageSize   int
	Offset     int
}

// Modpack represents a modpack from any indexer
type Modpack struct {
	ID              string    `json:"id"`
	IndexerID       string    `json:"indexer_id"` // Original ID from the indexer
	Indexer         string    `json:"indexer"`    // Which indexer this came from
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	Summary         string    `json:"summary"`
	Description     string    `json:"description"`
	LogoURL         string    `json:"logo_url"`
	WebsiteURL      string    `json:"website_url"`
	DownloadCount   int64     `json:"download_count"`
	Categories      []string  `json:"categories"`
	GameVersions    []string  `json:"game_versions"`
	ModLoaders      []string  `json:"mod_loaders"`
	LatestFileID    string    `json:"latest_file_id"`
	DateCreated     time.Time `json:"date_created"`
	DateModified    time.Time `json:"date_modified"`
	DateReleased    time.Time `json:"date_released"`
}

// ModpackFile represents a downloadable file for a modpack
type ModpackFile struct {
	ID               string    `json:"id"`
	ModpackID        string    `json:"modpack_id"`
	DisplayName      string    `json:"display_name"`
	FileName         string    `json:"file_name"`
	FileDate         time.Time `json:"file_date"`
	FileLength       int64     `json:"file_length"`
	ReleaseType      string    `json:"release_type"` // "release", "beta", "alpha"
	DownloadURL      string    `json:"download_url"`
	GameVersions     []string  `json:"game_versions"`
	ModLoader        string    `json:"mod_loader"`
	ServerPackFileID *string   `json:"server_pack_file_id,omitempty"`
}
