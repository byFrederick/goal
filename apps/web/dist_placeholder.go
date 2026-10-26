//go:build !webui

package web

import (
	"embed"
	"io/fs"
)

//go:embed placeholder
var placeholder embed.FS

func dist() (fs.FS, error) { return fs.Sub(placeholder, "placeholder") }
