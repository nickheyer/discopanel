package minecraft

import (
	"path/filepath"
	"slices"
	"strings"

	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	utils "github.com/nickheyer/discopanel/pkg/utils"
)

// One row of loader facts keyed by proto enum
// Adding a loader is one enum value plus one row
// Dialects nil means the install on disk testifies instead
// Builtins, MavenRanges, Facets, Markers live on defining rows
// Info is the wire row itself, names come from enum annotations
type LoaderInfo struct {
	Info        *v1.ModLoaderInfo
	Dialects    []string      // Manifest formats read, native first
	Builtins    []string      // Dep ids the platform itself provides
	MavenRanges bool          // Native manifest speaks maven ranges
	Facets      []string      // Indexer loader names that source jars
	Markers     []string      // Data-dir paths proving the platform installed
	Pack        *PackPlatform // Present on loaders that install packs
}

// Proto enum this row describes
func (r LoaderInfo) Loader() v1.ModLoader {
	return r.Info.Loader
}

// Pack platform facts shared by every loader on that platform
type PackPlatform struct {
	Source string
}

var curseforgePlatform = &PackPlatform{Source: "curseforge"}

var modrinthPlatform = &PackPlatform{Source: "modrinth"}

// Rows in display order, forks precede nothing they depend on
var registry = []LoaderInfo{
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_VANILLA, Category: "Vanilla"},
	},
	{
		Info:        &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_FORGE, Category: "Forge", ModsDirectory: "mods"},
		Dialects:    []string{"forge"},
		Builtins:    []string{"forge", "fml", "minecraft", "java", "mixin"},
		MavenRanges: true,
		Facets:      []string{"forge"},
		Markers:     []string{"libraries/net/minecraftforge"},
	},
	{
		Info:        &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_NEOFORGE, Category: "Forge", ModsDirectory: "mods"},
		Dialects:    []string{"neoforge", "forge"},
		Builtins:    []string{"neoforge"},
		MavenRanges: true,
		Facets:      []string{"neoforge"},
		Markers:     []string{"libraries/net/neoforged"},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_FABRIC, Category: "Fabric", ModsDirectory: "mods"},
		Dialects: []string{"fabric"},
		Builtins: []string{"fabricloader", "minecraft", "java", "mixin"},
		Facets:   []string{"fabric"},
		Markers: []string{
			"libraries/net/fabricmc/fabric-loader",
			"fabric-server-launch.jar",
		},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_QUILT, Category: "Fabric", ModsDirectory: "mods"},
		Dialects: []string{"quilt", "fabric"},
		Builtins: []string{"quilt_loader", "quilt_base"},
		Facets:   []string{"quilt", "fabric"},
		Markers: []string{
			"libraries/org/quiltmc/quilt-loader",
			"quilt-server-launch.jar",
		},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_BUKKIT, Category: "Bukkit", ModsDirectory: "plugins"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_SPIGOT, Category: "Bukkit", ModsDirectory: "plugins"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_PAPER, Category: "Paper", ModsDirectory: "plugins"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_PURPUR, Category: "Paper", ModsDirectory: "plugins"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_PUFFERFISH, Category: "Paper", ModsDirectory: "plugins"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_FOLIA, Category: "Paper", ModsDirectory: "plugins"},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_MAGMA, Category: "Hybrid", ModsDirectory: "mods"},
		Dialects: []string{"forge"},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_MAGMA_MAINTAINED, Category: "Hybrid", ModsDirectory: "mods"},
		Dialects: []string{"forge"},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_KETTING, Category: "Hybrid", ModsDirectory: "mods"},
		Dialects: []string{"forge"},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_MOHIST, Category: "Hybrid", ModsDirectory: "mods"},
		Dialects: []string{"forge"},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_YOUER, Category: "Hybrid", ModsDirectory: "mods"},
		Dialects: []string{"neoforge", "forge"},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_BANNER, Category: "Hybrid", ModsDirectory: "mods"},
		Dialects: []string{"fabric"},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_CATSERVER, Category: "Hybrid", ModsDirectory: "mods"},
		Dialects: []string{"forge"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_ARCLIGHT, Category: "Hybrid", ModsDirectory: "mods"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_SPONGE_VANILLA, Category: "Sponge", ModsDirectory: "mods"},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_SPONGE_FORGE, Category: "Sponge", ModsDirectory: "mods"},
		Dialects: []string{"forge"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_LIMBO, Category: "Lightweight"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_NANO_LIMBO, Category: "Lightweight"},
	},
	{
		Info:     &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_CRUCIBLE, Category: "Other", ModsDirectory: "mods"},
		Dialects: []string{"forge"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_GLOWSTONE, Category: "Other", ModsDirectory: "plugins"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_CUSTOM, Category: "Other", ModsDirectory: "mods"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_AUTO_CURSEFORGE, Category: "Modpack", ModsDirectory: "mods"},
		Pack: curseforgePlatform,
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_CURSEFORGE, Category: "Modpack", ModsDirectory: "mods"},
		Pack: curseforgePlatform,
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_FTBA, Category: "Modpack", ModsDirectory: "mods"},
	},
	{
		Info: &v1.ModLoaderInfo{Loader: v1.ModLoader_MOD_LOADER_MODRINTH, Category: "Modpack", ModsDirectory: "mods"},
		Pack: modrinthPlatform,
	},
}

// Pack platform for a loader, nil when packs never install
func PackPlatformFor(loader v1.ModLoader) *PackPlatform {
	if row, ok := loaderIndex[loader]; ok {
		return row.Pack
	}
	return nil
}

var (
	loaderIndex  = map[v1.ModLoader]*LoaderInfo{}
	nameIndex    = map[string]*LoaderInfo{}
	dialectIndex = map[string]*LoaderInfo{}
)

func init() {
	for i := range registry {
		row := &registry[i]
		l := row.Info.Loader
		row.Info.Name = l.Name()
		row.Info.DisplayName = l.Label()
		row.Info.Description = l.Desc()
		row.Info.SupportsMods = row.Info.ModsDirectory != ""
		row.Info.SupportsPlugins = row.Info.ModsDirectory == "plugins"
		loaderIndex[l] = row
		nameIndex[l.Name()] = row
		if len(row.Dialects) > 0 && row.Dialects[0] == l.Name() {
			dialectIndex[row.Dialects[0]] = row
		}
	}
}

// Row defining a manifest format, nil for unknown formats
func definingLoader(dialect string) *LoaderInfo {
	return dialectIndex[dialect]
}

// Returns every registry row in display order
func Loaders() []LoaderInfo {
	return slices.Clone(registry)
}

// Returns a loader's row, unknown yields bare
func LoaderFor(loader v1.ModLoader) LoaderInfo {
	if row, ok := loaderIndex[loader]; ok {
		return *row
	}
	return LoaderInfo{Info: &v1.ModLoaderInfo{
		Loader:      loader,
		Name:        loader.Name(),
		DisplayName: loader.Label(),
		Description: "Unknown mod loader",
		Category:    "Other",
	}}
}

// Maps an indexed modpack to the loader a server runs
func ServerLoaderForModpack(indexer string) (v1.ModLoader, bool) {
	switch indexer {
	case "fuego", "manual":
		return v1.ModLoader_MOD_LOADER_AUTO_CURSEFORGE, true
	case "modrinth":
		return v1.ModLoader_MOD_LOADER_MODRINTH, true
	}
	return v1.ModLoader_MOD_LOADER_UNSPECIFIED, false
}

// Splits manifest loader ids like forge-47.2.0
func CutPackLoaderID(loaderID string) (v1.ModLoader, string, bool) {
	name, version, _ := strings.Cut(loaderID, "-")
	row, ok := nameIndex[strings.ToLower(name)]
	if !ok {
		return v1.ModLoader_MOD_LOADER_UNSPECIFIED, "", false
	}
	return row.Loader(), version, true
}

// Loaders that define a manifest format, modpacks build on these
func PackLoaderNames() []string {
	var out []string
	for i := range registry {
		name := registry[i].Loader().Name()
		if len(registry[i].Dialects) > 0 && registry[i].Dialects[0] == name {
			out = append(out, name)
		}
	}
	return out
}

// Returns the mods storage path for a server
func GetModsPath(serverDataPath string, loader v1.ModLoader) string {
	dir := LoaderFor(loader).Info.ModsDirectory
	if dir == "" {
		return ""
	}
	return filepath.Join(serverDataPath, dir)
}

// Checks if a file is a valid mod for loader
func IsValidModFile(filename string, loader v1.ModLoader) bool {
	if LoaderFor(loader).Info.ModsDirectory == "" {
		return false
	}
	return strings.EqualFold(filepath.Ext(filename), ".jar")
}

// Fuzzy weights for mod loader matches
const (
	modLoaderMatchThreshold     = 0.5
	modpackLoaderMatchThreshold = 0.6
)

func MatchModLoader(input string) (v1.ModLoader, bool) {
	row, score, ok := utils.BestFunc(input, registry, func(r LoaderInfo) string {
		return r.Loader().Name()
	})
	if !ok || score < modLoaderMatchThreshold {
		return v1.ModLoader_MOD_LOADER_UNSPECIFIED, false
	}
	return row.Loader(), true
}

// Inspects candidate strings for modloader identification
func DetectModpackLoader(candidates ...string) (v1.ModLoader, bool) {
	best := ""
	bestScore := 0.0
	for _, c := range candidates {
		if m, ok := utils.Best(c, PackLoaderNames()); ok && m.Score > bestScore {
			best, bestScore = m.Value, m.Score
		}
	}
	if best == "" || bestScore < modpackLoaderMatchThreshold {
		return v1.ModLoader_MOD_LOADER_UNSPECIFIED, false
	}
	return nameIndex[best].Loader(), true
}
