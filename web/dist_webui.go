//go:build webui

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var built embed.FS

func dist() (fs.FS, error) { return fs.Sub(built, "dist") }
