//go:build e2e

package certificate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fishioon/aicat/internal/osfixture"
	"time"
)

const TrustSupported = true

// Remove replaces only the OS boundary, including a persisted partial result.
func Remove(ctx context.Context, path string) error {
	cert, err := readCertificate(path)
	if err != nil {
		return err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(cert.Raw))
	delay := 250 * time.Millisecond
	if err = osfixture.Update(func(s map[string]json.RawMessage) error {
		if osfixture.Bool(s, "holdRemove") {
			delay = 30 * time.Second
		}
		return nil
	}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
	}
	var result error
	err = osfixture.Update(func(state map[string]json.RawMessage) error {
		if osfixture.Bool(state, "cancelRemove") {
			return ErrCanceled
		}
		if osfixture.Bool(state, "failRemove") {
			return errors.New("test certificate removal failed")
		}
		var installed string
		_ = json.Unmarshal(state["installedCertificate"], &installed)
		if installed == fingerprint {
			state["trusted"] = json.RawMessage("false")
			if osfixture.Bool(state, "failRemoveAfterTrust") {
				result = errors.New("test keychain removal failed after trust revocation")
			} else {
				delete(state, "installedCertificate")
			}
		}
		return nil
	})
	return errors.Join(err, result)
}

func Install(ctx context.Context, path string) error {
	cert, err := readCertificate(path)
	if err != nil {
		return err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(cert.Raw))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(250 * time.Millisecond):
	}
	return osfixture.Update(func(state map[string]json.RawMessage) error {
		if osfixture.Bool(state, "cancelInstall") {
			return ErrCanceled
		}
		if osfixture.Bool(state, "failInstall") {
			return errors.New("test certificate install failed")
		}
		value, _ := json.Marshal(fingerprint)
		state["installedCertificate"] = value
		if !osfixture.Bool(state, "installWithoutTrust") {
			state["trusted"] = json.RawMessage("true")
			state["sslOnly"] = json.RawMessage("false")
		}
		return nil
	})
}

func Trusted(ctx context.Context, path string) (bool, error) {
	cert, err := readCertificate(path)
	if err != nil {
		return false, err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(cert.Raw))
	var trusted bool
	err = osfixture.Update(func(state map[string]json.RawMessage) error {
		var installed string
		_ = json.Unmarshal(state["installedCertificate"], &installed)
		if osfixture.Bool(state, "failTrustCheck") {
			return errors.New("test trust check failed")
		}
		trusted = osfixture.Bool(state, "trusted") && !osfixture.Bool(state, "sslOnly") && installed == fingerprint
		return nil
	})
	return trusted, err
}
