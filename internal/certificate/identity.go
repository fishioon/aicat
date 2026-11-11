package certificate

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/lqqyt2423/go-mitmproxy/cert"
)

// LoadOrCreate keeps the upstream CA/leaf implementation and file layout. Only
// a new root's identity is aicat-specific; existing roots are never replaced.
func LoadOrCreate(directory string) (cert.CA, error) {
	privatePath := filepath.Join(directory, "mitmproxy-ca.pem")
	publicPath := filepath.Join(directory, "mitmproxy-ca-cert.pem")
	info, err := os.Lstat(privatePath)
	if errors.Is(err, os.ErrNotExist) {
		if _, e := os.Lstat(publicPath); !errors.Is(e, os.ErrNotExist) {
			return nil, errors.New("CA 私钥缺失，保留现有证书以避免意外更换身份")
		}
		key, e := rsa.GenerateKey(rand.Reader, 2048)
		if e != nil {
			return nil, e
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			return nil, e
		}
		now := time.Now()
		template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "aicat", Organization: []string{"aicat"}}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(3 * 365 * 24 * time.Hour), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true}
		der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if e != nil {
			return nil, e
		}
		keyDER, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return nil, e
		}
		content := append(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
		tmp, e := os.CreateTemp(directory, ".identity-*")
		if e != nil {
			return nil, e
		}
		defer os.Remove(tmp.Name())
		_, e = tmp.Write(content)
		if e == nil {
			e = tmp.Sync()
		}
		closeErr := tmp.Close()
		if e = errors.Join(e, closeErr); e != nil {
			return nil, e
		}
		// Link is atomic and refuses to overwrite a concurrent creator's identity.
		if e = os.Link(tmp.Name(), privatePath); e != nil && !errors.Is(e, os.ErrExist) {
			return nil, e
		}
	} else if err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, errors.New("CA 身份必须是普通文件")
	}
	if err = os.Chmod(privatePath, 0600); err != nil {
		return nil, err
	}
	ca, err := cert.NewSelfSignCA(directory)
	if err != nil {
		return nil, err
	}
	pair, err := os.ReadFile(privatePath)
	if err != nil {
		return nil, err
	}
	if _, err = tls.X509KeyPair(pair, pair); err != nil {
		return nil, fmt.Errorf("CA 证书与私钥不匹配: %w", err)
	}
	root := ca.GetRootCA()
	if !root.IsCA || root.CheckSignatureFrom(root) != nil {
		return nil, fmt.Errorf("CA 身份无效")
	}
	// Restore/export only the public certificate of the root actually loaded.
	tmp, err := os.CreateTemp(directory, ".public-ca-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	err = pem.Encode(tmp, &pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
	if err == nil {
		err = tmp.Sync()
	}
	err = errors.Join(err, tmp.Close())
	if err != nil {
		return nil, err
	}
	if err = os.Rename(tmp.Name(), publicPath); err != nil {
		return nil, err
	}
	return ca, nil
}
