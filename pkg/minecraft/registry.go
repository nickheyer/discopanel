package minecraft

import (
	"path/filepath"
	"slices"
	"strings"

	v1 "github.com/nickheyer/discopanel/pkg/proto/discopanel/v1"
	utils "github.com/nickheyer/discopanel/pkg/utils"
)

type ModLoader string

const (
	// Vanilla
	ModLoaderVanilla ModLoader = "vanilla"

	// Forge-based
	ModLoaderForge    ModLoader = "forge"
	ModLoaderNeoForge ModLoader = "neoforge"

	// Fabric-based
	ModLoaderFabric ModLoader = "fabric"
	ModLoaderQuilt  ModLoader = "quilt"

	// Bukkit-based
	ModLoaderBukkit ModLoader = "bukkit"
	ModLoaderSpigot ModLoader = "spigot"

	// Paper-based
	ModLoaderPaper      ModLoader = "paper"
	ModLoaderPurpur     ModLoader = "purpur"
	ModLoaderPufferfish ModLoader = "pufferfish"
	ModLoaderFolia      ModLoader = "folia"

	// Hybrids (Forge + Bukkit)
	ModLoaderMagma           ModLoader = "magma"
	ModLoaderMagmaMaintained ModLoader = "magma_maintained"
	ModLoaderKetting         ModLoader = "ketting"
	ModLoaderMohist          ModLoader = "mohist"
	ModLoaderYouer           ModLoader = "youer"
	ModLoaderBanner          ModLoader = "banner"
	ModLoaderCatserver       ModLoader = "catserver"
	ModLoaderArclight        ModLoader = "arclight"

	// Sponge
	ModLoaderSpongeVanilla ModLoader = "spongevanilla"
	ModLoaderSpongeForge   ModLoader = "spongeforge"

	// Others
	ModLoaderLimbo     ModLoader = "limbo"
	ModLoaderNanoLimbo ModLoader = "nanolimbo"
	ModLoaderCrucible  ModLoader = "crucible"
	ModLoaderGlowstone ModLoader = "glowstone"
	ModLoaderCustom    ModLoader = "custom"

	// Modpack Platforms
	ModLoaderAutoCurseForge ModLoader = "auto_curseforge"
	ModLoaderCurseForge     ModLoader = "curseforge"
	ModLoaderFTBA           ModLoader = "ftba"
	ModLoaderModrinth       ModLoader = "modrinth"
)

// One row of loader facts keyed by proto enum
// Adding a loader is one enum value plus one row
// Dialects nil means the install on disk testifies instead
// Builtins, MavenRanges, Facets, Markers live on defining rows
type LoaderInfo struct {
	Loader        ModLoader
	Proto         v1.ModLoader
	DisplayName   string
	Description   string
	Category      string
	ModsDirectory string
	Dialects      []string      // Manifest formats read, native first
	Builtins      []string      // Dep ids the platform itself provides
	MavenRanges   bool          // Native manifest speaks maven ranges
	Facets        []string      // Indexer loader names that source jars
	Markers       []string      // Data-dir paths proving the platform installed
	Pack          *PackPlatform // Present on loaders that install packs
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
		Loader:      ModLoaderVanilla,
		Proto:       v1.ModLoader_MOD_LOADER_VANILLA,
		DisplayName: "Vanilla",
		Description: "Vanilla Minecraft server without mod support",
		Category:    "Vanilla",
	},
	{
		Loader:        ModLoaderForge,
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
		Loader:        ModLoaderNeoForge,
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
		Loader:        ModLoaderFabric,
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
		Loader:        ModLoaderQuilt,
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
		Loader:        ModLoaderBukkit,
		Proto:         v1.ModLoader_MOD_LOADER_BUKKIT,
		DisplayName:   "Bukkit",
		Description:   "The original plugin API for Minecraft servers",
		Category:      "Bukkit",
		ModsDirectory: "plugins",
	},
	{
		Loader:        ModLoaderSpigot,
		Proto:         v1.ModLoader_MOD_LOADER_SPIGOT,
		DisplayName:   "Spigot",
		Description:   "High-performance fork of Bukkit",
		Category:      "Bukkit",
		ModsDirectory: "plugins",
	},
	{
		Loader:        ModLoaderPaper,
		Proto:         v1.ModLoader_MOD_LOADER_PAPER,
		DisplayName:   "Paper",
		Description:   "Performance-optimized fork of Spigot",
		Category:      "Paper",
		ModsDirectory: "plugins",
	},
	{
		Loader:        ModLoaderPurpur,
		Proto:         v1.ModLoader_MOD_LOADER_PURPUR,
		DisplayName:   "Purpur",
		Description:   "Fork of Paper with additional gameplay features",
		Category:      "Paper",
		ModsDirectory: "plugins",
	},
	{
		Loader:        ModLoaderPufferfish,
		Proto:         v1.ModLoader_MOD_LOADER_PUFFERFISH,
		DisplayName:   "Pufferfish",
		Description:   "Performance-focused fork of Paper",
		Category:      "Paper",
		ModsDirectory: "plugins",
	},
	{
		Loader:        ModLoaderFolia,
		Proto:         v1.ModLoader_MOD_LOADER_FOLIA,
		DisplayName:   "Folia",
		Description:   "Regionized multithreaded fork of Paper",
		Category:      "Paper",
		ModsDirectory: "plugins",
	},
	{
		Loader:        ModLoaderMagma,
		Proto:         v1.ModLoader_MOD_LOADER_MAGMA,
		DisplayName:   "Magma",
		Description:   "Hybrid server supporting both Forge mods and Bukkit plugins",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        ModLoaderMagmaMaintained,
		Proto:         v1.ModLoader_MOD_LOADER_MAGMA_MAINTAINED,
		DisplayName:   "Magma Maintained",
		Description:   "Maintained fork of Magma hybrid server",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        ModLoaderKetting,
		Proto:         v1.ModLoader_MOD_LOADER_KETTING,
		DisplayName:   "Ketting",
		Description:   "Modern hybrid server for Forge and Bukkit",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        ModLoaderMohist,
		Proto:         v1.ModLoader_MOD_LOADER_MOHIST,
		DisplayName:   "Mohist",
		Description:   "Hybrid server combining Forge and Paper",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        ModLoaderYouer,
		Proto:         v1.ModLoader_MOD_LOADER_YOUER,
		DisplayName:   "Youer",
		Description:   "NeoForge hybrid server by the Mohist team",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"neoforge", "forge"},
	},
	{
		Loader:        ModLoaderBanner,
		Proto:         v1.ModLoader_MOD_LOADER_BANNER,
		DisplayName:   "Banner",
		Description:   "Fabric hybrid server by the Mohist team",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"fabric"},
	},
	{
		Loader:        ModLoaderCatserver,
		Proto:         v1.ModLoader_MOD_LOADER_CATSERVER,
		DisplayName:   "Catserver",
		Description:   "Hybrid server implementation",
		Category:      "Hybrid",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        ModLoaderArclight,
		Proto:         v1.ModLoader_MOD_LOADER_ARCLIGHT,
		DisplayName:   "Arclight",
		Description:   "Modern hybrid implementation for Forge, NeoForge, and Fabric",
		Category:      "Hybrid",
		ModsDirectory: "mods",
	},
	{
		Loader:        ModLoaderSpongeVanilla,
		Proto:         v1.ModLoader_MOD_LOADER_SPONGE_VANILLA,
		DisplayName:   "SpongeVanilla",
		Description:   "Plugin platform with advanced API",
		Category:      "Sponge",
		ModsDirectory: "mods",
	},
	{
		Loader:        ModLoaderSpongeForge,
		Proto:         v1.ModLoader_MOD_LOADER_SPONGE_FORGE,
		DisplayName:   "SpongeForge",
		Description:   "Sponge plugin platform on top of Forge",
		Category:      "Sponge",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:      ModLoaderLimbo,
		Proto:       v1.ModLoader_MOD_LOADER_LIMBO,
		DisplayName: "Limbo",
		Description: "Lightweight server for holding players",
		Category:    "Lightweight",
	},
	{
		Loader:      ModLoaderNanoLimbo,
		Proto:       v1.ModLoader_MOD_LOADER_NANO_LIMBO,
		DisplayName: "NanoLimbo",
		Description: "Ultra-lightweight server for holding players",
		Category:    "Lightweight",
	},
	{
		Loader:        ModLoaderCrucible,
		Proto:         v1.ModLoader_MOD_LOADER_CRUCIBLE,
		DisplayName:   "Crucible",
		Description:   "Legacy hybrid server implementation",
		Category:      "Other",
		ModsDirectory: "mods",
		Dialects:      []string{"forge"},
	},
	{
		Loader:        ModLoaderGlowstone,
		Proto:         v1.ModLoader_MOD_LOADER_GLOWSTONE,
		DisplayName:   "Glowstone",
		Description:   "Open-source Minecraft server implementation",
		Category:      "Other",
		ModsDirectory: "plugins",
	},
	{
		Loader:        ModLoaderCustom,
		Proto:         v1.ModLoader_MOD_LOADER_CUSTOM,
		DisplayName:   "Custom",
		Description:   "Custom server implementation",
		Category:      "Other",
		ModsDirectory: "mods",
	},
	{
		Loader:        ModLoaderAutoCurseForge,
		Proto:         v1.ModLoader_MOD_LOADER_AUTO_CURSEFORGE,
		DisplayName:   "Auto CurseForge",
		Description:   "Automatic CurseForge modpack installer",
		Category:      "Modpack",
		ModsDirectory: "mods",
		Pack:          curseforgePlatform,
	},
	{
		Loader:        ModLoaderCurseForge,
		Proto:         v1.ModLoader_MOD_LOADER_CURSEFORGE,
		DisplayName:   "CurseForge",
		Description:   "Popular modpack platform",
		Category:      "Modpack",
		ModsDirectory: "mods",
		Pack:          curseforgePlatform,
	},
	{
		Loader:        ModLoaderFTBA,
		Proto:         v1.ModLoader_MOD_LOADER_FTBA,
		DisplayName:   "Feed The Beast",
		Description:   "FTB modpack platform, upload the server files yourself",
		Category:      "Modpack",
		ModsDirectory: "mods",
	},
	{
		Loader:        ModLoaderModrinth,
		Proto:         v1.ModLoader_MOD_LOADER_MODRINTH,
		DisplayName:   "Modrinth",
		Description:   "Modern open-source modpack platform",
		Category:      "Modpack",
		ModsDirectory: "mods",
		Pack:          modrinthPlatform,
	},
}

// Pack platform for a loader, nil when packs never install
func PackPlatformFor(loader ModLoader) *PackPlatform {
	if row, ok := loaderIndex[loader]; ok {
		return row.Pack
	}
	return nil
}

var (
	loaderIndex  = map[ModLoader]*LoaderInfo{}
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

// Returns a loader's row, unknown yields bare
func LoaderFor(loader ModLoader) LoaderInfo {
	if row, ok := loaderIndex[loader]; ok {
		return *row
	}
	return LoaderInfo{Loader: loader, DisplayName: string(loader), Description: "Unknown mod loader", Category: "Other"}
}

// Converts a db loader to its proto value
func ProtoFor(loader ModLoader) v1.ModLoader {
	if row, ok := loaderIndex[loader]; ok {
		return row.Proto
	}
	return v1.ModLoader_MOD_LOADER_UNSPECIFIED
}

// Converts a proto loader to its db value
func LoaderFromProto(p v1.ModLoader) (ModLoader, bool) {
	if row, ok := protoIndex[p]; ok {
		return row.Loader, true
	}
	return "", false
}

// Maps an indexed modpack to the loader a server runs
func ServerLoaderForModpack(indexer string) (ModLoader, bool) {
	switch indexer {
	case "fuego", "manual":
		return ModLoaderAutoCurseForge, true
	case "modrinth":
		return ModLoaderModrinth, true
	}
	return "", false
}

// Splits manifest loader ids like forge-47.2.0
func CutPackLoaderID(loaderID string) (ModLoader, string, bool) {
	name, version, _ := strings.Cut(loaderID, "-")
	loader := ModLoader(strings.ToLower(name))
	if _, ok := loaderIndex[loader]; !ok {
		return "", "", false
	}
	return loader, version, true
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
func GetModsPath(serverDataPath string, loader ModLoader) string {
	dir := LoaderFor(loader).ModsDirectory
	if dir == "" {
		return ""
	}
	return filepath.Join(serverDataPath, dir)
}

// Checks if a file is a valid mod for loader
func IsValidModFile(filename string, loader ModLoader) bool {
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

func MatchModLoader(input string) (ModLoader, bool) {
	row, score, ok := utils.BestFunc(input, registry, func(r LoaderInfo) string {
		return string(r.Loader)
	})
	if !ok || score < modLoaderMatchThreshold {
		return "", false
	}
	return row.Loader, true
}

// Inspects candidate strings for modloader identification
func DetectModpackLoader(candidates ...string) (ModLoader, bool) {
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
	return ModLoader(best), true
}
