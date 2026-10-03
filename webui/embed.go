// Package webui embeds the compiled dashboard assets into the binary.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var dist embed.FS

// Dist returns the built dashboard files rooted at the asset directory.
func Dist() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
