//go:build e2e

// Package ui embeds the framework-independent web frontend. This file only
// bridges Go's embed rule (paths are relative to the containing package).
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist-e2e
var files embed.FS

func Files() fs.FS {
	web, err := fs.Sub(files, "dist-e2e")
	if err != nil {
		panic(err)
	}
	return web
}
