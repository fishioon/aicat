//go:build e2e

package app

import (
	"os"

	"github.com/fishioon/aicat/internal/catalog"
)

// Acceptance replaces only the remote origins with local fixtures: the same
// handler, interception and provider routes are exercised.
func codexBackend() string { return os.Getenv("AICAT_E2E_CODEX_BACKEND") }

func modelsDevURL() string {
	if u := os.Getenv("AICAT_E2E_MODELS_DEV"); u != "" {
		return u
	}
	return catalog.URL
}
