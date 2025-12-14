package minecraft

import (
	"path/filepath"
	"slices"
	"strings"

	models "github.com/nickheyer/discopanel/internal/db"
)

// ModLoaderInfo contains information about a specific mod loader
type ModLoaderInfo struct {
	Name              string
	DisplayName       string
	Description       string
	Category          string
	ModsDirectory     string
	ConfigDirectory   string
	SupportedVersions []string
	FileExtensions    []string
}

// GetModLoaderInfo returns information about a specific mod loader
func GetModLoaderInfo(loader models.ModLoader) ModLoaderInfo {
	switch loader {
	// Vanilla
	case models.ModLoaderVanilla:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Vanilla",
			Description:     "Vanilla Minecraft server without mod support",
			Category:        "Vanilla",
			ModsDirectory:   "",
			ConfigDirectory: "",
			FileExtensions:  []string{},
		}

	// Forge-based
	case models.ModLoaderForge:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Minecraft Forge",
			Description:     "The original and most widely used modding platform",
			Category:        "Forge",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderNeoForge:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "NeoForge",
			Description:     "Modern fork of Forge with improved features",
			Category:        "Forge",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	// Fabric-based
	case models.ModLoaderFabric:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Fabric",
			Description:     "Lightweight and fast modding platform",
			Category:        "Fabric",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderQuilt:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Quilt",
			Description:     "Fork of Fabric with additional features",
			Category:        "Fabric",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	// Bukkit-based
	case models.ModLoaderBukkit:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Bukkit",
			Description:     "The original plugin API for Minecraft servers",
			Category:        "Bukkit",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderSpigot:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Spigot",
			Description:     "High-performance fork of Bukkit",
			Category:        "Bukkit",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderPaper:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Paper",
			Description:     "Performance-optimized fork of Spigot",
			Category:        "Bukkit",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderPurpur:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Purpur",
			Description:     "Fork of Paper with additional gameplay features",
			Category:        "Bukkit",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderPufferfish:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Pufferfish",
			Description:     "Performance-focused fork of Paper",
			Category:        "Bukkit",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}

	// Hybrids (Forge + Bukkit)
	case models.ModLoaderMagma:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Magma",
			Description:     "Hybrid server supporting both Forge mods and Bukkit plugins",
			Category:        "Hybrid",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderMagmaMaintained:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Magma Maintained",
			Description:     "Maintained fork of Magma hybrid server",
			Category:        "Hybrid",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderKetting:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Ketting",
			Description:     "Modern hybrid server for Forge and Bukkit",
			Category:        "Hybrid",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderMohist:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Mohist",
			Description:     "Hybrid server combining Forge and Paper",
			Category:        "Hybrid",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderYouer:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Youer",
			Description:     "Hybrid server implementation",
			Category:        "Hybrid",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderBanner:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Banner",
			Description:     "Hybrid server implementation",
			Category:        "Hybrid",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderCatserver:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Catserver",
			Description:     "Hybrid server implementation",
			Category:        "Hybrid",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderArclight:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Arclight",
			Description:     "Modern hybrid implementation for Forge and Bukkit",
			Category:        "Hybrid",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	// Sponge
	case models.ModLoaderSpongeVanilla:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "SpongeVanilla",
			Description:     "Plugin platform with advanced API",
			Category:        "Sponge",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	// Others
	case models.ModLoaderLimbo:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Limbo",
			Description:     "Lightweight server for holding players",
			Category:        "Lightweight",
			ModsDirectory:   "",
			ConfigDirectory: "",
			FileExtensions:  []string{},
		}
	case models.ModLoaderNanoLimbo:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "NanoLimbo",
			Description:     "Ultra-lightweight server for holding players",
			Category:        "Lightweight",
			ModsDirectory:   "",
			ConfigDirectory: "",
			FileExtensions:  []string{},
		}
	case models.ModLoaderCrucible:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Crucible",
			Description:     "Legacy hybrid server implementation",
			Category:        "Other",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderGlowstone:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Glowstone",
			Description:     "Open-source Minecraft server implementation",
			Category:        "Other",
			ModsDirectory:   "plugins",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderCustom:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Custom",
			Description:     "Custom server implementation",
			Category:        "Other",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	// Modpack Platforms
	case models.ModLoaderAutoCurseForge:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Auto CurseForge",
			Description:     "Automatic CurseForge modpack installer",
			Category:        "Modpack",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderCurseForge:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "CurseForge",
			Description:     "Popular modpack platform",
			Category:        "Modpack",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderFTBA:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Feed The Beast",
			Description:     "Feed The Beast modpack platform",
			Category:        "Modpack",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderModrinth:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Modrinth",
			Description:     "Modern open-source modpack platform",
			Category:        "Modpack",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	default:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     string(loader),
			Description:     "Unknown mod loader",
			Category:        "Other",
			ModsDirectory:   "",
			ConfigDirectory: "",
			FileExtensions:  []string{},
		}
	}
}

// GetModsPath returns the path where mods should be stored for a given server
func GetModsPath(serverDataPath string, loader models.ModLoader) string {
	info := GetModLoaderInfo(loader)
	if info.ModsDirectory == "" {
		return ""
	}
	return filepath.Join(serverDataPath, info.ModsDirectory)
}

// IsValidModFile checks if a file is a valid mod file for the given loader
func IsValidModFile(filename string, loader models.ModLoader) bool {
	info := GetModLoaderInfo(loader)
	if len(info.FileExtensions) == 0 {
		return false
	}

	ext := strings.ToLower(filepath.Ext(filename))
	return slices.Contains(info.FileExtensions, ext)
}

// GetAllModLoaders returns information about all available mod loaders
func GetAllModLoaders() []ModLoaderInfo {
	loaders := []models.ModLoader{
		// Vanilla
		models.ModLoaderVanilla,

		// Forge-based
		models.ModLoaderForge,
		models.ModLoaderNeoForge,

		// Fabric-based
		models.ModLoaderFabric,
		models.ModLoaderQuilt,

		// Bukkit-based
		models.ModLoaderBukkit,
		models.ModLoaderSpigot,
		models.ModLoaderPaper,
		models.ModLoaderPurpur,
		models.ModLoaderPufferfish,

		// Hybrids (Forge + Bukkit)
		models.ModLoaderMagma,
		models.ModLoaderMagmaMaintained,
		models.ModLoaderKetting,
		models.ModLoaderMohist,
		models.ModLoaderYouer,
		models.ModLoaderBanner,
		models.ModLoaderCatserver,
		models.ModLoaderArclight,

		// Sponge
		models.ModLoaderSpongeVanilla,

		// Others
		models.ModLoaderLimbo,
		models.ModLoaderNanoLimbo,
		models.ModLoaderCrucible,
		models.ModLoaderGlowstone,
		models.ModLoaderCustom,

		// Modpack Platforms
		models.ModLoaderAutoCurseForge,
		models.ModLoaderCurseForge,
		models.ModLoaderFTBA,
		models.ModLoaderModrinth,
	}

	infos := make([]ModLoaderInfo, len(loaders))
	for i, loader := range loaders {
		infos[i] = GetModLoaderInfo(loader)
	}

	return infos
}
