//go:build !windows

package files

import (
	"fmt"
	"syscall"
)

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