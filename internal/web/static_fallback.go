//go:build !frontend_embed

package web

import (
	"embed"
	"io/fs"
)

//go:embed frontend_fallback/*
var fallbackFS embed.FS

// GetStaticFS returns a fallback page when the frontend bundle is not embedded.
func GetStaticFS() (fs.FS, error) {
	return fs.Sub(fallbackFS, "frontend_fallback")
}
