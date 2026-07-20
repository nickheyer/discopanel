package indexers

import (
	"context"
	"fmt"
	"sort"
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

// Asks an indexer for jars providing one mod id
type ModQuery struct {
	ModID     string
	McVersion string
	Loaders   []string // Loader facets the server can load
}

// One downloadable jar an indexer offers for a query
// Callers must verify the jar declares the queried id
type ModCandidate struct {
	Origin   string // Human label naming the source project
	FileName string
	URL      string
	HashAlgo string // Hash name, empty skips verification
	HashSum  string
}

// Optional capability sourcing single mod jars by id
// Candidates come best match first
type ModSourcer interface {
	SourceMod(ctx context.Context, q ModQuery) ([]ModCandidate, error)
}

// Creates a ModpackIndexer from an API key and user agent
type IndexerFactory func(apiKey string, userAgent string) ModpackIndexer

// Registration facts one indexer declares about itself
type IndexerInfo struct {
	Name                 string
	CredentialProperty   string // Property key holding this indexer's API key
	ForceIncludeProperty string // Property key holding protected file patterns
	PackSource           string // Modpack source string this indexer serves
}

// Tunes registration metadata for one indexer
type IndexerOption func(*IndexerInfo)

// Declares the property key holding the API key
func WithCredentialProperty(key string) IndexerOption {
	return func(i *IndexerInfo) { i.CredentialProperty = key }
}

// Declares the property key holding force include patterns
func WithForceIncludeProperty(key string) IndexerOption {
	return func(i *IndexerInfo) { i.ForceIncludeProperty = key }
}

// Declares the modpack source this indexer serves
func WithPackSource(source string) IndexerOption {
	return func(i *IndexerInfo) { i.PackSource = source }
}

type indexerEntry struct {
	info    IndexerInfo
	factory IndexerFactory
}

var (
	registryMu sync.RWMutex
	registry   = make(map[string]indexerEntry)
)

// Registers an IndexerFactory under a given name
func RegisterIndexer(name string, factory IndexerFactory, opts ...IndexerOption) {
	info := IndexerInfo{Name: name}
	for _, opt := range opts {
		opt(&info)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = indexerEntry{info: info, factory: factory}
}

// Creates a ModpackIndexer by name from the registry
func NewIndexer(name string, apiKey string, userAgent string) (ModpackIndexer, error) {
	registryMu.RLock()
	entry, ok := registry[name]
	registryMu.RUnlock()
	if entry.factory == nil || !ok {
		return nil, fmt.Errorf("unknown indexer: %s", name)
	}
	return entry.factory(apiKey, userAgent), nil
}

// Looks up one registered indexer's declared facts
func LookupIndexer(name string) (IndexerInfo, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	entry, ok := registry[name]
	return entry.info, ok
}

// Lists every registered indexer sorted by name
func Indexers() []IndexerInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]IndexerInfo, 0, len(registry))
	for _, entry := range registry {
		out = append(out, entry.info)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}
