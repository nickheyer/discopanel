package web

import (
	"embed"
	"io/fs"
)

//go:embed build/*

// Embed the built SvelteKit application
// Note: build directory is not included in version control
// Run 'npm run build' in web/discopanel to generate it

var files embed.FS

// BuildFS returns the embedded filesystem containing the built frontend
func BuildFS() (fs.FS, error) {
	// Check if build directory exists in embedded FS
	_, err := fs.Stat(files, "build")
	if err != nil {
		// If build directory doesn't exist, return an empty filesystem
		return nil, nil
	}

	subFS, err := fs.Sub(files, "build")
	if err != nil {
		return nil, nil
	}
	return subFS, nil
}
