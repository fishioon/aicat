//go:build e2e

package main

import (
	"crypto/x509"
	"log"
	"os"
)

// Shared by native and headless E2E: trust only the isolated fixture root.
// This never writes to Keychain or disables TLS verification.
func init() {
	if path := os.Getenv("AICAT_E2E_ROOT"); path != "" {
		pem, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			log.Fatal("invalid fixture CA")
		}
		x509.SetFallbackRoots(roots)
	}
}
