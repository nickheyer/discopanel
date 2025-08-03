package minecraft

import (
	"fmt"
	"path/filepath"
	"strings"

	models "github.com/nickheyer/discopanel/internal/db"
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
	case models.ModLoaderForge:
		return ModLoaderInfo{
			Name:            "forge",
			DisplayName:     "Minecraft Forge",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderFabric:
		return ModLoaderInfo{
			Name:            "fabric",
			DisplayName:     "Fabric",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderNeoForge:
		return ModLoaderInfo{
			Name:            "neoforge",
			DisplayName:     "NeoForge",
			ModsDirectory:   "mods",
			ConfigDirectory: "config",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderPaper:
		return ModLoaderInfo{
			Name:            "paper",
			DisplayName:     "Paper",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}
	case models.ModLoaderSpigot:
		return ModLoaderInfo{
			Name:            "spigot",
			DisplayName:     "Spigot",
			ModsDirectory:   "plugins",
			ConfigDirectory: "plugins",
			FileExtensions:  []string{".jar"},
		}
	default:
		return ModLoaderInfo{
			Name:            "vanilla",
			DisplayName:     "Vanilla",
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
		models.ModLoaderVanilla,
		models.ModLoaderForge,
		models.ModLoaderFabric,
		models.ModLoaderNeoForge,
		models.ModLoaderPaper,
		models.ModLoaderSpigot,
	}

	infos := make([]ModLoaderInfo, len(loaders))
	for i, loader := range loaders {
		infos[i] = GetModLoaderInfo(loader)
	}

	return infos
}

// GetJavaVersionForMinecraft returns the recommended Java version for a Minecraft version
func GetJavaVersionForMinecraft(mcVersion string) string {
	// Parse major.minor version
	parts := strings.Split(mcVersion, ".")
	if len(parts) < 2 {
		return "17" // Default to Java 17
	}

	major := parts[1]
	if len(major) > 0 {
		switch major {
		case "7", "8", "9", "10", "11", "12", "13", "14", "15", "16":
			return "8"
		case "17":
			return "16"
		case "18", "19":
			return "17"
		case "20":
			return "17"
		case "21":
			return "21"
		default:
			// For versions 1.21+ use Java 21
			if majorNum := strings.TrimPrefix(major, "1."); len(majorNum) > 0 {
				if majorNum >= "21" {
					return "21"
				}
			}
			return "21" // Latest versions use Java 21
		}
	}

	return "17"
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
