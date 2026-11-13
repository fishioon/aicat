//go:build e2e

package main

import (
	"crypto/x509"
	"log/slog"
	"os"
)

// Shared by native and headless E2E: trust only the isolated fixture root.
// This never writes to Keychain or disables TLS verification.
func init() {
	if path := os.Getenv("AICAT_E2E_ROOT"); path != "" {
		pem, err := os.ReadFile(path)
		if err != nil {
			slog.Error("read fixture CA failed", "err", err)
			os.Exit(1)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			slog.Error("invalid fixture CA")
			os.Exit(1)
		}
		x509.SetFallbackRoots(roots)
	}
}
