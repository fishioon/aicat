//go:build e2e

package channelplugin

import "os"

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
