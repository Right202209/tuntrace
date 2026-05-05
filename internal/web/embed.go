package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

// distSub returns dist/ as the root, so request "/" maps to "dist/index.html".
func distSub() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
