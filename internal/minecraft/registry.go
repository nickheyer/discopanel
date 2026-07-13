package minecraft

import (
	"path/filepath"
	"slices"
	"strings"

	models "github.com/nickheyer/discopanel/internal/db"
	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	utils "github.com/nickheyer/discopanel/pkg/utils"
)

// One row of loader facts, the proto enum is the key
// Adding a loader is one enum value plus one row
// Dialects nil means the install on disk testifies instead
// Builtins, MavenRanges, Facets, Markers live on defining rows
type LoaderInfo struct {
	Loader        models.ModLoader
	Proto         v1.ModLoader
	DisplayName   string
	Description   string
	Category      string
	ModsDirectory string
	Dialects      []string // Manifest formats read, native first
	Builtins      []string // Dep ids the platform itself provides
	MavenRanges   bool     // Native manifest speaks maven ranges
	Facets        []string // Indexer loader names that source jars
	Markers       []string // Data-dir paths proving the platform installed
}

// Rows in display order, forks precede nothing they depend on
var registry = []LoaderInfo{
	{
		Loader:      models.ModLoaderVanilla,
		Proto:       v1.ModLoader_MOD_LOADER_VANILLA,
		DisplayName: "Vanilla",
		Description: "Vanilla Minecraft server without mod support",
		Category:    "Vanilla",
	},
	{
		Loader:        models.ModLoaderForge,
		Proto:         v1.ModLoader_MOD_LOADER_FORGE,
		DisplayName:   "Minecraft Forge",
		Description:   "The original and most widely used modding platform",
		Category:      "Forge",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
		Builtins:      []string{"forge", "fml", "minecraft", "java", "mixin"},
		MavenRanges:   true,
		Facets:        []string{"forge"},
		Markers:       []string{"libraries/net/minecraftforge"},
	},
	{
		Loader:        models.ModLoaderNeoForge,
		Proto:         v1.ModLoader_MOD_LOADER_NEOFORGE,
		DisplayName:   "NeoForge",
		Description:   "Modern fork of Forge with improved features",
		Category:      "Forge",
		ModsDirectory: "mods",
		Dialects:      []string{"neoforge", "forge"},
		Builtins:      []string{"neoforge"},
		MavenRanges:   true,
		Facets:        []string{"neoforge"},
		Markers:       []string{"libraries/net/neoforged"},
	},
	{
		Loader:        models.ModLoaderFabric,
		Proto:         v1.ModLoader_MOD_LOADER_FABRIC,
		DisplayName:   "Fabric",
		Description:   "Lightweight and fast modding platform",
		Category:      "Fabric",
		ModsDirectory: "mods",
		Dialects:      []string{"fabric"},
		Builtins:      []string{"fabricloader", "minecraft", "java", "mixin"},
		Facets:        []string{"fabric"},
		Markers: []string{
			"libraries/net/fabricmc/fabric-loader",
			"fabric-server-launch.jar",
		},
	},
	{
		Loader:        models.ModLoaderQuilt,
		Proto:         v1.ModLoader_MOD_LOADER_QUILT,
		DisplayName:   "Quilt",
		Description:   "Fork of Fabric with additional features",
		Category:      "Fabric",
		ModsDirectory: "mods",
		Dialects:      []string{"quilt", "fabric"},
		Builtins:      []string{"quilt_loader", "quilt_base"},
		Facets:        []string{"quilt", "fabric"},
		Markers: []string{
			"libraries/org/quiltmc/quilt-loader",
			"quilt-server-launch.jar",
		},
	},
	{
		Loader:        models.ModLoaderBukkit,
		Proto:         v1.ModLoader_MOD_LOADER_BUKKIT,
		DisplayName:   "Bukkit",
		Description:   "The original plugin API for Minecraft servers",
		Category:      "Bukkit",
		ModsDirectory: "plugins",
	},
	{
		Loader:        models.ModLoaderSpigot,
		Proto:         v1.ModLoader_MOD_LOADER_SPIGOT,
		DisplayName:   "Spigot",
		Description:   "High-performance fork of Bukkit",
		Category:      "Bukkit",
		ModsDirectory: "plugins",
	},
	{
		Loader:        models.ModLoaderPaper,
		Proto:         v1.ModLoader_MOD_LOADER_PAPER,
		DisplayName:   "Paper",
		Description:   "Performance-optimized fork of Spigot",
		Category:      "Paper",
		ModsDirectory: "plugins",
	},
	{
		Loader:        models.ModLoaderPurpur,
		Proto:         v1.ModLoader_MOD_LOADER_PURPUR,
		DisplayName:   "Purpur",
		Description:   "Fork of Paper with additional gameplay features",
		Category:      "Paper",
		ModsDirectory: "plugins",
	},
	{
		Loader:        models.ModLoaderPufferfish,
		Proto:         v1.ModLoader_MOD_LOADER_PUFFERFISH,
		DisplayName:   "Pufferfish",
		Description:   "Performance-focused fork of Paper",
		Category:      "Paper",
		ModsDirectory: "plugins",
	},
	{
		Loader:        models.ModLoaderFolia,
		Proto:         v1.ModLoader_MOD_LOADER_FOLIA,
		DisplayName:   "Folia",
		Description:   "Regionized multithreaded fork of Paper",
		Category:      "Paper",
		ModsDirectory: "plugins",
	},
	{
		Loader:        models.ModLoaderMagma,
		Proto:         v1.ModLoader_MOD_LOADER_MAGMA,
		DisplayName:   "Magma",
		Description:   "Hybrid server supporting both Forge mods and Bukkit plugins",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        models.ModLoaderMagmaMaintained,
		Proto:         v1.ModLoader_MOD_LOADER_MAGMA_MAINTAINED,
		DisplayName:   "Magma Maintained",
		Description:   "Maintained fork of Magma hybrid server",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        models.ModLoaderKetting,
		Proto:         v1.ModLoader_MOD_LOADER_KETTING,
		DisplayName:   "Ketting",
		Description:   "Modern hybrid server for Forge and Bukkit",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        models.ModLoaderMohist,
		Proto:         v1.ModLoader_MOD_LOADER_MOHIST,
		DisplayName:   "Mohist",
		Description:   "Hybrid server combining Forge and Paper",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        models.ModLoaderYouer,
		Proto:         v1.ModLoader_MOD_LOADER_YOUER,
		DisplayName:   "Youer",
		Description:   "NeoForge hybrid server by the Mohist team",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"neoforge", "forge"},
	},
	{
		Loader:        models.ModLoaderBanner,
		Proto:         v1.ModLoader_MOD_LOADER_BANNER,
		DisplayName:   "Banner",
		Description:   "Fabric hybrid server by the Mohist team",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"fabric"},
	},
	{
		Loader:        models.ModLoaderCatserver,
		Proto:         v1.ModLoader_MOD_LOADER_CATSERVER,
		DisplayName:   "Catserver",
		Description:   "Hybrid server implementation",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        models.ModLoaderArclight,
		Proto:         v1.ModLoader_MOD_LOADER_ARCLIGHT,
		DisplayName:   "Arclight",
		Description:   "Modern hybrid implementation for Forge, NeoForge, and Fabric",
		Category:      "Hybrid",
		ModsDirectory: "mods",
	},
	{
		Loader:        models.ModLoaderSpongeVanilla,
		Proto:         v1.ModLoader_MOD_LOADER_SPONGE_VANILLA,
		DisplayName:   "SpongeVanilla",
		Description:   "Plugin platform with advanced API",
		Category:      "Sponge",
		ModsDirectory: "mods",
	},
	{
		Loader:        models.ModLoaderSpongeForge,
		Proto:         v1.ModLoader_MOD_LOADER_SPONGE_FORGE,
		DisplayName:   "SpongeForge",
		Description:   "Sponge plugin platform on top of Forge",
		Category:      "Sponge",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:      models.ModLoaderLimbo,
		Proto:       v1.ModLoader_MOD_LOADER_LIMBO,
		DisplayName: "Limbo",
		Description: "Lightweight server for holding players",
		Category:    "Lightweight",
	},
	{
		Loader:      models.ModLoaderNanoLimbo,
		Proto:       v1.ModLoader_MOD_LOADER_NANO_LIMBO,
		DisplayName: "NanoLimbo",
		Description: "Ultra-lightweight server for holding players",
		Category:    "Lightweight",
	},
	{
		Loader:        models.ModLoaderCrucible,
		Proto:         v1.ModLoader_MOD_LOADER_CRUCIBLE,
		DisplayName:   "Crucible",
		Description:   "Legacy hybrid server implementation",
		Category:      "Other",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        models.ModLoaderGlowstone,
		Proto:         v1.ModLoader_MOD_LOADER_GLOWSTONE,
		DisplayName:   "Glowstone",
		Description:   "Open-source Minecraft server implementation",
		Category:      "Other",
		ModsDirectory: "plugins",
	},
	{
		Loader:        models.ModLoaderCustom,
		Proto:         v1.ModLoader_MOD_LOADER_CUSTOM,
		DisplayName:   "Custom",
		Description:   "Custom server implementation",
		Category:      "Other",
		ModsDirectory: "mods",
	},
	{
		Loader:        models.ModLoaderAutoCurseForge,
		Proto:         v1.ModLoader_MOD_LOADER_AUTO_CURSEFORGE,
		DisplayName:   "Auto CurseForge",
		Description:   "Automatic CurseForge modpack installer",
		Category:      "Modpack",
		ModsDirectory: "mods",
	},
	{
		Loader:        models.ModLoaderCurseForge,
		Proto:         v1.ModLoader_MOD_LOADER_CURSEFORGE,
		DisplayName:   "CurseForge",
		Description:   "Popular modpack platform",
		Category:      "Modpack",
		ModsDirectory: "mods",
	},
	{
		Loader:        models.ModLoaderFTBA,
		Proto:         v1.ModLoader_MOD_LOADER_FTBA,
		DisplayName:   "Feed The Beast",
		Description:   "FTB modpack platform, upload the server files yourself",
		Category:      "Modpack",
		ModsDirectory: "mods",
	},
	{
		Loader:        models.ModLoaderModrinth,
		Proto:         v1.ModLoader_MOD_LOADER_MODRINTH,
		DisplayName:   "Modrinth",
		Description:   "Modern open-source modpack platform",
		Category:      "Modpack",
		ModsDirectory: "mods",
	},
}

var (
	loaderIndex  = map[models.ModLoader]*LoaderInfo{}
	protoIndex   = map[v1.ModLoader]*LoaderInfo{}
	dialectIndex = map[string]*LoaderInfo{}
)

func init() {
	for i := range registry {
		row := &registry[i]
		loaderIndex[row.Loader] = row
		protoIndex[row.Proto] = row
		if len(row.Dialects) > 0 && row.Dialects[0] == string(row.Loader) {
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

// Returns the row for a loader, unknown yields a bare row
func LoaderFor(loader models.ModLoader) LoaderInfo {
	if row, ok := loaderIndex[loader]; ok {
		return *row
	}
	return LoaderInfo{Loader: loader, DisplayName: string(loader), Description: "Unknown mod loader", Category: "Other"}
}

// Converts a db loader to its proto value
func ProtoFor(loader models.ModLoader) v1.ModLoader {
	if row, ok := loaderIndex[loader]; ok {
		return row.Proto
	}
	return v1.ModLoader_MOD_LOADER_UNSPECIFIED
}

// Converts a proto loader to its db value
func LoaderFromProto(p v1.ModLoader) (models.ModLoader, bool) {
	if row, ok := protoIndex[p]; ok {
		return row.Loader, true
	}
	return "", false
}

// Loaders that define a manifest format, modpacks build on these
func PackLoaderNames() []string {
	var out []string
	for i := range registry {
		if len(registry[i].Dialects) > 0 && registry[i].Dialects[0] == string(registry[i].Loader) {
			out = append(out, string(registry[i].Loader))
		}
	}
	return out
}

// Returns the mods storage path for a server
func GetModsPath(serverDataPath string, loader models.ModLoader) string {
	dir := LoaderFor(loader).ModsDirectory
	if dir == "" {
		return ""
	}
	return filepath.Join(serverDataPath, dir)
}

// Checks if a file is a valid mod for loader
func IsValidModFile(filename string, loader models.ModLoader) bool {
	if LoaderFor(loader).ModsDirectory == "" {
		return false
	}
	return strings.EqualFold(filepath.Ext(filename), ".jar")
}

// Fuzzy weights for mod loader matches
const (
	modLoaderMatchThreshold     = 0.5
	modpackLoaderMatchThreshold = 0.6
)

func MatchModLoader(input string) (models.ModLoader, bool) {
	row, score, ok := utils.BestFunc(input, registry, func(r LoaderInfo) string {
		return string(r.Loader)
	})
	if !ok || score < modLoaderMatchThreshold {
		return "", false
	}
	return row.Loader, true
}

// Inspects candidate strings for modloader identification
func DetectModpackLoader(candidates ...string) (models.ModLoader, bool) {
	best := ""
	bestScore := 0.0
	for _, c := range candidates {
		if m, ok := utils.Best(c, PackLoaderNames()); ok && m.Score > bestScore {
			best, bestScore = m.Value, m.Score
		}
	}
	if best == "" || bestScore < modpackLoaderMatchThreshold {
		return "", false
	}
	return models.ModLoader(best), true
}
