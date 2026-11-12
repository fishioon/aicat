//go:build !e2e

package app

import "github.com/fishioon/aicat/internal/catalog"

// codexBackend is Codex's own backend; empty is chatgpt.com.
func codexBackend() string { return "" }

// modelsDevURL is where the models.dev catalog is downloaded from.
func modelsDevURL() string { return catalog.URL }
