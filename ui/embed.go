// Package ui embeds the framework-independent web frontend. This file only
// bridges Go's embed rule (paths are relative to the containing package).
package ui

import (
	"embed"
	"io/fs"
)

//go:embed *.html *.css *.js *.json
var files embed.FS

func Files() fs.FS { return files }
