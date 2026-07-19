package indexers

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Interface for modpack indexing services
type ModpackIndexer interface {
	// Searches for modpacks matching given criteria
	SearchModpacks(ctx context.Context, query string, gameVersion string, modLoader string, offset, limit int) (*SearchResult, error)

	// Retrieves detailed info for one modpack
	GetModpack(ctx context.Context, modpackID string) (*Modpack, error)

	// Retrieves available files, empty filters mean all
	GetModpackFiles(ctx context.Context, modpackID string, gameVersion string, modLoader string) ([]ModpackFile, error)

	// Name of this indexer, e.g. fuego or modrinth
	GetIndexerName() string
}

// Results of a modpack search
type SearchResult struct {
	Modpacks   []Modpack
	TotalCount int
	PageSize   int
	Offset     int
}

// A modpack from any indexer
type Modpack struct {
	ID            string    `json:"id"`
	IndexerID     string    `json:"indexer_id"` // Original ID from the indexer
	Indexer       string    `json:"indexer"`    // Which indexer this came from
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	Summary       string    `json:"summary"`
	Description   string    `json:"description"`
	LogoURL       string    `json:"logo_url"`
	WebsiteURL    string    `json:"website_url"`
	DownloadCount int64     `json:"download_count"`
	Categories    []string  `json:"categories"`
	GameVersions  []string  `json:"game_versions"`
	ModLoaders    []string  `json:"mod_loaders"`
	LatestFileID  string    `json:"latest_file_id"`
	DateCreated   time.Time `json:"date_created"`
	DateModified  time.Time `json:"date_modified"`
	DateReleased  time.Time `json:"date_released"`
}

// A downloadable file for a modpack
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
	SortIndex        int       `json:"sort_index"`
	VersionNumber    string    `json:"version_number"` // Human-readable version for Modrinth
}

// Creates a ModpackIndexer from an API key and user agent
type IndexerFactory func(apiKey string, userAgent string) ModpackIndexer

var (
	registryMu sync.RWMutex
	registry   = make(map[string]IndexerFactory)
)

// Registers an IndexerFactory under a given name
func RegisterIndexer(name string, factory IndexerFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

// Creates a ModpackIndexer by name from the registry
func NewIndexer(name string, apiKey string, userAgent string) (ModpackIndexer, error) {
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown indexer: %s", name)
	}
	return factory(apiKey, userAgent), nil
}
