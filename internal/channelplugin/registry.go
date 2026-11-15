//go:build !e2e

package channelplugin

func registry() string { return "https://registry.npmjs.org" }

func catalogVersion(version string) string { return version }

func sourceEndpoint(address string) string { return address }
