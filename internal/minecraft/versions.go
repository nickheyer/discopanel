package minecraft

import (
	"log"

	"github.com/RandomTechrate/discopanel-fork/internal/db"
)

// GetLatestVersion returns the latest Minecraft version
func GetLatestVersion() string {
	return "1.21.8"
}

// GetDefaultVersions returns a list of all Minecraft versions for the default modloader (paper)
func GetDefaultVersions() []string {
	return GetVersionsForModloader(db.ModLoaderPaper)
}

// GetVersionsForModloader returns a list of all Minecraft versions for a given modloader
func GetVersionsForModloader(modloader db.ModLoader) []string {
	versions, err := GetVersions(string(modloader))
	if err != nil {
		log.Println("failed to get versions for modloader", modloader, ":", err)
		return []string{}
	}
	return versions
}

// IsValidVersion checks if a given version string is a valid Minecraft version
func IsValidVersion(version string) bool {
	// This is not efficient, but it's the best we can do without a modloader parameter
	// We will check against all modloaders
	modloaders := []db.ModLoader{
		db.ModLoaderPaper,
		db.ModLoaderSpigot,
		db.ModLoaderVanilla,
		db.ModLoaderForge,
		db.ModLoaderFabric,
	}

	for _, modloader := range modloaders {
		versions := GetVersionsForModloader(modloader)
		for _, v := range versions {
			if v == version {
				return true
			}
		}
	}

	return false
}
