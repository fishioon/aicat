//go:build !e2e

package codex

import "github.com/fishioon/aicat/internal/codex/catalog"

// backendOrigin is Codex's own backend; empty is chatgpt.com.
func backendOrigin() string { return "" }

// modelsDevURL is where the models.dev catalog is downloaded from.
func modelsDevURL() string { return catalog.URL }
