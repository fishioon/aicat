//go:build !nogui && e2e

package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/fishioon/aicat/internal/osfixture"
)

func chooseTrafficExport(*mygo.Window) (string, error) {
	cancel := false
	err := osfixture.Update(func(s map[string]json.RawMessage) error { cancel = osfixture.Bool(s, "cancelExport"); return nil })
	if err != nil || cancel {
		return "", err
	}
	fixture := os.Getenv("AICAT_E2E_OS_STATE")
	if !filepath.IsAbs(fixture) {
		return "", errors.New("missing isolated OS fixture path")
	}
	return filepath.Join(filepath.Dir(fixture), "aicat-traffic.json"), nil
}
