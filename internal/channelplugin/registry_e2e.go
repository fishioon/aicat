//go:build e2e

package channelplugin

import (
	"os"
	"strings"
)

func registry() string {
	if value := os.Getenv("AICAT_E2E_PLUGIN_REGISTRY"); value != "" {
		return value
	}
	return "https://registry.npmjs.org"
}

func catalogVersion(version string) string {
	if value := os.Getenv("AICAT_E2E_PLUGIN_VERSION"); value != "" {
		return value
	}
	return version
}

func sourceEndpoint(address string) string {
	if origin := os.Getenv("AICAT_E2E_GITHUB_URL"); origin != "" && strings.HasPrefix(address, "https://api.github.com/") {
		return origin + strings.TrimPrefix(address, "https://api.github.com")
	}
	return address
}
