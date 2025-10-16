package minecraft

import (
	"fmt"
	"path/filepath"
	"strings"

	models "github.com/RandomTechrate/discopanel-fork/internal/db"
)

// ModLoaderInfo contains information about a specific mod loader
type ModLoaderInfo struct {
	Name              string
	DisplayName       string
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
			ModsDirectory:   "",
			ConfigDirectory: "",
			FileExtensions:  []string{},
		}

	// Forge-based
	case models.ModLoaderForge:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Minecraft Forge",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderNeoForge:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "NeoForge",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	// Fabric-based
	case models.ModLoaderFabric:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Fabric",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderQuilt:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Quilt",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	// Bukkit-based
	case models.ModLoaderBukkit:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Bukkit",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderSpigot:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Spigot",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderPaper:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Paper",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderPufferfish:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Pufferfish",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}

	// Hybrids (Forge + Bukkit)
	case models.ModLoaderMagma:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Magma",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderMagmaMaintained:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Magma Maintained",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderKetting:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Ketting",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderMohist:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Mohist",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderYouer:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Youer",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderBanner:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Banner",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderCatserver:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Catserver",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderArclight:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Arclight",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	// Sponge
	case models.ModLoaderSpongeVanilla:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "SpongeVanilla",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	// Others
	case models.ModLoaderLimbo:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Limbo",
			ModsDirectory:   "",
			ConfigDirectory: "",
			FileExtensions:  []string{},
		}
	case models.ModLoaderNanoLimbo:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "NanoLimbo",
			ModsDirectory:   "",
			ConfigDirectory: "",
			FileExtensions:  []string{},
		}
	case models.ModLoaderCrucible:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Crucible",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderGlowstone:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Glowstone",
			ModsDirectory:   "plugins",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderCustom:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Custom",
			ModsDirectory:   "",
			ConfigDirectory: "",
			FileExtensions:  []string{".jar"},
		}

	// Modpack Platforms
	case models.ModLoaderAutoCurseForge:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Auto CurseForge",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderCurseForge:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "CurseForge",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderFTBA:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Feed The Beast",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderModrinth:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     "Modrinth",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}

	default:
		return ModLoaderInfo{
			Name:            string(loader),
			DisplayName:     string(loader),
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

// GetConfigPath returns the path where configs should be stored for a given server
func GetConfigPath(serverDataPath string, loader models.ModLoader) string {
	info := GetModLoaderInfo(loader)
	if info.ConfigDirectory == "" {
		return serverDataPath
	}
	return filepath.Join(serverDataPath, info.ConfigDirectory)
}

// IsValidModFile checks if a file is a valid mod file for the given loader
func IsValidModFile(filename string, loader models.ModLoader) bool {
	info := GetModLoaderInfo(loader)
	if len(info.FileExtensions) == 0 {
		return false
	}

	ext := strings.ToLower(filepath.Ext(filename))
	for _, validExt := range info.FileExtensions {
		if ext == validExt {
			return true
		}
	}
	return false
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

// GetStartupFlags returns recommended JVM flags for the server
func GetStartupFlags(memory int) []string {
	return []string{
		fmt.Sprintf("-Xms%dM", memory),
		fmt.Sprintf("-Xmx%dM", memory),
		"-XX:+UseG1GC",
		"-XX:+ParallelRefProcEnabled",
		"-XX:MaxGCPauseMillis=200",
		"-XX:+UnlockExperimentalVMOptions",
		"-XX:+DisableExplicitGC",
		"-XX:+AlwaysPreTouch",
		"-XX:G1NewSizePercent=30",
		"-XX:G1MaxNewSizePercent=40",
		"-XX:G1HeapRegionSize=8M",
		"-XX:G1ReservePercent=20",
		"-XX:G1HeapWastePercent=5",
		"-XX:G1MixedGCCountTarget=4",
		"-XX:InitiatingHeapOccupancyPercent=15",
		"-XX:G1MixedGCLiveThresholdPercent=90",
		"-XX:G1RSetUpdatingPauseTimePercent=5",
		"-XX:SurvivorRatio=32",
		"-XX:+PerfDisableSharedMem",
		"-XX:MaxTenuringThreshold=1",
		"-Dusing.aikars.flags=https://mcflags.emc.gs",
		"-Daikars.new.flags=true",
	}
}
