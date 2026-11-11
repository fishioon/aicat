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

// Only native OS panels/launching are replaced. The same menu, controller,
// public export, filesystem lifecycle and signing implementation are exercised.
func chooseCertificateExport(*mygo.Window) (string, error) {
	cancel := false
	err := osfixture.Update(func(s map[string]json.RawMessage) error { cancel = osfixture.Bool(s, "cancelExport"); return nil })
	if err != nil || cancel {
		return "", err
	}
	p := os.Getenv("AICAT_E2E_OS_STATE")
	if !filepath.IsAbs(p) {
		return "", errors.New("missing isolated OS fixture path")
	}
	return filepath.Join(filepath.Dir(p), "aicat-root-ca.pem"), nil
}
func openCertificateKeychain() error {
	return osfixture.Update(func(s map[string]json.RawMessage) error { s["keychainOpened"] = json.RawMessage("true"); return nil })
}
