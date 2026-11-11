//go:build !nogui && e2e

package app

import (
	"errors"
	"github.com/egoist/mygo"
	"os"
	"path/filepath"
)

// Replace only the native save panel. Serialization, the real button and
// filesystem writes stay on the production path in the isolated E2E directory.
func chooseTrafficExport(*mygo.Window) (string, error) {
	fixture := os.Getenv("AICAT_E2E_OS_STATE")
	if !filepath.IsAbs(fixture) {
		return "", errors.New("missing isolated OS fixture path")
	}
	return filepath.Join(filepath.Dir(fixture), "aicat-traffic.json"), nil
}
