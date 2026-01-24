package minecraft

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// v2 API
	versionManifestV2URL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

	// Cache for 1 hour
	cacheDuration = time.Hour
)

type VersionManifestV2 struct {
	Latest   LatestVersions `json:"latest"`
	Versions []Version      `json:"versions"`
}

type LatestVersions struct {
	Release  string `json:"release"`
	Snapshot string `json:"snapshot"`
}

// A single Minecraft version entry
type Version struct {
	ID              string    `json:"id"`
	Type            string    `json:"type"`
	URL             string    `json:"url"`
	Time            time.Time `json:"time"`
	ReleaseTime     time.Time `json:"releaseTime"`
	SHA1            string    `json:"sha1"`
	ComplianceLevel int       `json:"complianceLevel"`
}

// Metadata for a specific version
type VersionMetadata struct {
	JavaVersion struct {
		Component    string `json:"component"`
		MajorVersion int    `json:"majorVersion"`
	} `json:"javaVersion"`
}

// Manifest data
type versionCache struct {
	mu            sync.RWMutex
	manifest      *VersionManifestV2
	lastFetchTime time.Time
	javaVersions  map[string]string // Cache for Java versions by MC version ID
}

var cache = &versionCache{
	javaVersions: make(map[string]string),
}

// Fetches the version manifest from the Mojang API
func fetchVersionManifest() (*VersionManifestV2, error) {
	// Check cache first
	cache.mu.RLock()
	if cache.manifest != nil && time.Since(cache.lastFetchTime) < cacheDuration {
		manifest := cache.manifest
		cache.mu.RUnlock()
		return manifest, nil
	}
	cache.mu.RUnlock()

	// Fetch new manifest
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(versionManifestV2URL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch version manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch version manifest: status code %d", resp.StatusCode)
	}

	var manifest VersionManifestV2
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode version manifest: %w", err)
	}

	// Update cache
	cache.mu.Lock()
	cache.manifest = &manifest
	cache.lastFetchTime = time.Now()
	cache.mu.Unlock()

	return &manifest, nil
}

// Returns the latest Minecraft release version
func GetLatestVersion() string {
	manifest, err := fetchVersionManifest()
	if err != nil {
		return "0"
	}

	if manifest.Latest.Release != "" {
		return manifest.Latest.Release
	}

	// Fallback to first release in the list
	for _, version := range manifest.Versions {
		if version.Type == "release" {
			return version.ID
		}
	}

	return "0"
}

// Returns a list of all Minecraft release versions
func GetVersions() []string {
	manifest, err := fetchVersionManifest()
	if err != nil {
		return []string{}
	}

	var versions []string
	for _, version := range manifest.Versions {
		if version.Type == "release" {
			versions = append(versions, version.ID)
		}
	}

	return versions
}

// Returns all versions including snapshots
func GetAllVersions() []string {
	manifest, err := fetchVersionManifest()
	if err != nil {
		return []string{}
	}

	var versions []string
	for _, version := range manifest.Versions {
		versions = append(versions, version.ID)
	}

	return versions
}

// Checks if a given version string is a valid Minecraft version
func IsValidVersion(version string) bool {
	manifest, err := fetchVersionManifest()
	if err != nil {
		return false
	}

	for _, v := range manifest.Versions {
		if v.ID == version {
			return true
		}
	}

	return false
}

// Returns the release date of a Minecraft version
func GetVersionDate(version string) (time.Time, error) {
	manifest, err := fetchVersionManifest()
	if err != nil {
		return time.Time{}, err
	}

	for _, v := range manifest.Versions {
		if v.ID == version {
			return v.ReleaseTime, nil
		}
	}

	return time.Time{}, fmt.Errorf("version %s not found", version)
}

// Returns detailed information about a specific version
func GetVersionInfo(version string) (*Version, error) {
	manifest, err := fetchVersionManifest()
	if err != nil {
		return nil, err
	}

	for _, v := range manifest.Versions {
		if v.ID == version {
			return &v, nil
		}
	}

	return nil, fmt.Errorf("version %s not found", version)
}

// Returns the latest snapshot version
func GetLatestSnapshot() string {
	manifest, err := fetchVersionManifest()
	if err != nil {
		return ""
	}

	return manifest.Latest.Snapshot
}

// Checks if a version is a snapshot
func IsSnapshot(version string) bool {
	manifest, err := fetchVersionManifest()
	if err != nil {
		return false
	}

	for _, v := range manifest.Versions {
		if v.ID == version {
			return v.Type == "snapshot"
		}
	}

	return false
}

// Fetches required Java version for a specific Minecraft version
func GetJavaVersion(mcVersion string) (string, error) {
	// Check cache first
	cache.mu.RLock()
	if javaVer, ok := cache.javaVersions[mcVersion]; ok {
		cache.mu.RUnlock()
		return javaVer, nil
	}
	cache.mu.RUnlock()

	// Get version URL from manifest
	versionInfo, err := GetVersionInfo(mcVersion)
	if err != nil {
		return "0", fmt.Errorf("version %s not found in manifest", mcVersion)
	}

	// Fetch version metadata
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(versionInfo.URL)
	if err != nil {
		return "0", fmt.Errorf("failed to fetch version metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "0", fmt.Errorf("failed to fetch version metadata: status code %d", resp.StatusCode)
	}

	var metadata VersionMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return "0", fmt.Errorf("failed to decode version metadata: %w", err)
	}

	javaVersion := strconv.Itoa(metadata.JavaVersion.MajorVersion)

	// Cache the result
	cache.mu.Lock()
	cache.javaVersions[mcVersion] = javaVersion
	cache.mu.Unlock()

	return javaVersion, nil
}

func FindMostRecentMinecraftVersion(versions []string) string {
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
		return versions[len(versions)-1] // Return last because we don't have a choice now
	}
	return ""
}

const (
	protocolVersionsURL = "https://raw.githubusercontent.com/PrismarineJS/minecraft-data/master/data/pc/common/protocolVersions.json"
)

type ProtocolVersionEntry struct {
	MinecraftVersion string `json:"minecraftVersion"`
	Version          int    `json:"version"`
	DataVersion      int    `json:"dataVersion"`
	UsesNetty        bool   `json:"usesNetty"`
	MajorVersion     string `json:"majorVersion"`
	ReleaseType      string `json:"releaseType"`
}

// Protocol cache
type protocolCache struct {
	mu            sync.RWMutex
	versions      map[string]int // mcVersion -> protocolVersion
	lastFetchTime time.Time
}

var protoCache = &protocolCache{
	versions: make(map[string]int),
}

// Fetches PrismarineJS minecraft-data
func fetchProtocolVersions() (map[string]int, error) {
	// Check cache first
	protoCache.mu.RLock()
	if len(protoCache.versions) > 0 && time.Since(protoCache.lastFetchTime) < cacheDuration {
		versions := protoCache.versions
		protoCache.mu.RUnlock()
		return versions, nil
	}
	protoCache.mu.RUnlock()

	// Fetch new data
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(protocolVersionsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch protocol versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch protocol versions: status code %d", resp.StatusCode)
	}

	var entries []ProtocolVersionEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("failed to decode protocol versions: %w", err)
	}

	// Build version map
	versions := make(map[string]int)
	for _, entry := range entries {
		versions[entry.MinecraftVersion] = entry.Version
	}

	// Update cache
	protoCache.mu.Lock()
	protoCache.versions = versions
	protoCache.lastFetchTime = time.Now()
	protoCache.mu.Unlock()

	return versions, nil
}

// Get protocol version for a Minecraft version string
func GetProtocolVersion(mcVersion string) int {
	versions, err := fetchProtocolVersions()
	if err != nil {
		return -1
	}

	if proto, ok := versions[mcVersion]; ok {
		return proto
	}

	// Return -1 for unknown versions - servers accept this for status pings
	return -1
}
