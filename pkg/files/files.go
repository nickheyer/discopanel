package files

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func FindWorldDir(dataDir string) (string, error) {
	var worldDir string

	err := filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip problematic paths
			return nil // Continue walking
		}

		// Skip if not a directory
		if !d.IsDir() {
			return nil
		}

		folderName := d.Name()

		// Check if this directory name matches "world" (case-insensitive)
		if strings.ToLower(folderName) == "world" {
			// Validate it's actually a Minecraft world by checking for level.dat
			levelDatPath := filepath.Join(path, "level.dat")
			if _, err := os.Stat(levelDatPath); err == nil {
				worldDir = path
				// Stop walking once we find a valid world
				return fs.SkipAll
			}
		}

		return nil
	})

	if err != nil && err != fs.SkipAll {
		return "", fmt.Errorf("failed to find world dir in data %s: %w", dataDir, err)
	}

	if worldDir == "" {
		return "", fmt.Errorf("no valid world directory found in %s", dataDir)
	}

	return worldDir, nil
}

// calculateDirSize calculates the total size of a directory in bytes, including all nested files.
func CalculateDirSize(dirPath string) (int64, error) {
	var totalSize int64

	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip problematic paths
			return nil // Continue walking
		}

		// Skip if it's a directory or symbolic link
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			log.Printf("Error getting info for %s: %v", path, err)
			return nil // Continue walking
		}

		totalSize += info.Size()
		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to walk directory %s: %w", dirPath, err)
	}

	return totalSize, nil
}

func SanitizePathName(name string) string {
	// alphanum + _ + -
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	safe := re.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "_")

	// Empty
	if safe == "" {
		safe = "DISCO_GENERIC"
	}
	return safe
}
