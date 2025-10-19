package files

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mholt/archives"
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

// Extracts archive file to destination
func ExtractArchive(ctx context.Context, archivePath string, destPath string) error {
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("failed to open archive file: %w", err)
	}
	defer archiveFile.Close()

	// Identify archive format
	format, stream, err := archives.Identify(ctx, archivePath, archiveFile)
	if err != nil {
		return fmt.Errorf("failed to identify archive format: %w", err)
	}

	// Is format extractable?
	extractor, ok := format.(archives.Extractor)
	if !ok {
		return fmt.Errorf("format does not support extraction")
	}

	// Make dest
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Extract and walk while recursively extracting
	err = extractor.Extract(ctx, stream, func(ctx context.Context, f archives.FileInfo) error {
		// Build path
		targetPath := filepath.Join(destPath, f.NameInArchive)

		// No sneaky traversals
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destPath)) {
			return fmt.Errorf("illegal file path in archive: %s", f.NameInArchive)
		}

		if f.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}

		// Make parent(s)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Create file
		outFile, err := os.Create(targetPath)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", targetPath, err)
		}
		defer outFile.Close()

		// Open the file from the archive
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open file in archive: %w", err)
		}
		defer rc.Close()

		// Copy contents
		if _, err := io.Copy(outFile, rc); err != nil {
			return fmt.Errorf("failed to extract file %s: %w", targetPath, err)
		}

		// Set file perms
		if f.Mode() != 0 {
			os.Chmod(targetPath, f.Mode())
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to extract archive: %w", err)
	}

	return nil
}
