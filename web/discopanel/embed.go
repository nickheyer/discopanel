package web

import (
	"embed"
	"io/fs"
)

// Embed the built SvelteKit application
//
//go:embed all:build
var files embed.FS

// BuildFS returns the embedded filesystem containing the built frontend
func BuildFS() (fs.FS, error) {
	return fs.Sub(files, "build")
}