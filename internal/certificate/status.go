// Package certificate manages the public certificate's OS trust, independently of the proxy and UI.
package certificate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"
)

var ErrCanceled = errors.New("已取消证书授权")

type Status struct {
	Name            string    `json:"name"`
	Fingerprint     string    `json:"fingerprint"`
	NotBefore       time.Time `json:"notBefore"`
	NotAfter        time.Time `json:"notAfter"`
	Trusted         bool      `json:"trusted"`
	Expired         bool      `json:"expired"`
	Supported       bool      `json:"supported"`
	Exists          bool      `json:"exists"`
	PendingDeletion bool      `json:"pendingDeletion"`
}

func Inspect(ctx context.Context, path string) (Status, error) {
	s := Status{Supported: TrustSupported}
	c, err := readCertificate(path)
	if err != nil {
		return s, err
	}
	s = statusOf(c)
	if s.Expired {
		return s, nil
	}
	s.Trusted, err = Trusted(ctx, path)
	return s, err
}

func statusOf(c *x509.Certificate) Status {
	return Status{Name: c.Subject.CommonName, Fingerprint: fmt.Sprintf("%X", sha256.Sum256(c.Raw)), NotBefore: c.NotBefore, NotAfter: c.NotAfter, Expired: time.Now().Before(c.NotBefore) || !time.Now().Before(c.NotAfter), Supported: TrustSupported, Exists: true}
}

func readCertificate(path string) (*x509.Certificate, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(b)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("无效的公开 CA 证书")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	if !c.IsCA || !c.BasicConstraintsValid {
		return nil, errors.New("证书不是有效的 CA")
	}
	return c, c.CheckSignatureFrom(c)
}
