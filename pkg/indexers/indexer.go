package indexers

import (
	"context"
	"fmt"
	"sync"

	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
)

// Interface for modpack indexing services
type ModpackIndexer interface {
	// Searches for modpacks matching given criteria
	SearchModpacks(ctx context.Context, query string, gameVersion string, modLoader string, offset, limit int) (*SearchResult, error)

	// Retrieves detailed info for one modpack
	GetModpack(ctx context.Context, modpackID string) (*v1.IndexedModpack, error)

	// Retrieves available files, empty filters mean all
	GetModpackFiles(ctx context.Context, modpackID string, gameVersion string, modLoader string) ([]*v1.IndexedModpackFile, error)

	// Name of this indexer, e.g. fuego or modrinth
	GetIndexerName() string
}

// Results of a modpack search
type SearchResult struct {
	Modpacks   []*v1.IndexedModpack
	TotalCount int
	PageSize   int
	Offset     int
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
