//go:build frontend_embed

package web

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var distFS embed.FS

// GetStaticFS returns the generated frontend asset bundle.
func GetStaticFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
