package web

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var build embed.FS

// Dist returns the embedded Vite build with index.html at the root.
func Dist() (fs.FS, error) {
	return fs.Sub(build, "dist")
}
