package files

import (
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"syscall"
)

// calculateDirSize calculates the total size of a directory in bytes, including all nested files.
func CalculateDirSize(dirPath string) (int64, error) {
	var totalSize int64

	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip problematic paths but log them
			log.Printf("Error accessing path %s: %v", path, err)
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

// GetDiskSpace returns the total disk space available in bytes for the given path
func GetDiskSpace(path string) (int64, error) {
	var stat syscall.Statfs_t
	
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("failed to get disk stats for %s: %w", path, err)
	}
	
	// Total space = block size * total blocks
	totalSpace := int64(stat.Blocks) * int64(stat.Bsize)
	
	return totalSpace, nil
}
