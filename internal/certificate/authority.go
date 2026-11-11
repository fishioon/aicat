package certificate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/lqqyt2423/go-mitmproxy/cert"
)

// Authority owns one root identity. Signing and its leaf cache stay upstream;
// dropping the upstream CA disables signing and releases that cache together.
// A durable tombstone prevents a deleted identity from being recreated at boot.
type Authority struct {
	mu        sync.RWMutex
	directory string
	ca        cert.CA
	root      *x509.Certificate
	pending   bool
}

func Open(directory string) (*Authority, error) {
	a := &Authority{directory: directory}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, err
	}
	_, err := os.Lstat(a.tombstone())
	if err == nil {
		// A crash or cancellation can leave files awaiting removal. Never load
		// their private key into a signing CA while deletion is pending.
		for _, name := range []string{"mitmproxy-ca-cert.cer", "mitmproxy-ca-cert.pem", "mitmproxy-ca.pem"} {
			b, e := os.ReadFile(filepath.Join(directory, name))
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil {
				return nil, e
			}
			for len(b) > 0 {
				block, rest := pem.Decode(b)
				b = rest
				if block == nil {
					return nil, errors.New("无法读取待删除的根证书")
				}
				if block.Type == "CERTIFICATE" {
					a.root, e = x509.ParseCertificate(block.Bytes)
					if e != nil {
						return nil, e
					}
					if !a.root.IsCA || a.root.CheckSignatureFrom(a.root) != nil {
						return nil, errors.New("待删除的 CA 身份无效")
					}
					a.pending = true
					return a, nil
				}
			}
		}
		return a, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	a.ca, err = LoadOrCreate(directory)
	if err != nil {
		return nil, err
	}
	a.root = a.ca.GetRootCA()
	return a, nil
}

func (a *Authority) tombstone() string  { return filepath.Join(a.directory, "identity-deleted") }
func (a *Authority) PublicPath() string { return filepath.Join(a.directory, "mitmproxy-ca-cert.pem") }

func (a *Authority) Available() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ca != nil
}
func (a *Authority) GetRootCA() *x509.Certificate {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.root
}
func (a *Authority) GetCert(host string) (*tls.Certificate, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ca == nil {
		return nil, errors.New("本机根证书不可用，HTTPS 解密已停用")
	}
	return a.ca.GetCert(host)
}

// App's connection coordinator serializes management operations. The signing
// lock is held only when swapping identities, never during OS authorization.
func (a *Authority) Inspect(ctx context.Context) (Status, error) {
	a.mu.RLock()
	root, pending := a.root, a.pending
	a.mu.RUnlock()
	if root == nil {
		return Status{Supported: TrustSupported}, nil
	}
	s := statusOf(root)
	s.PendingDeletion = pending
	if _, err := a.public(); err != nil {
		return s, err
	}
	if s.Expired || pending {
		return s, nil
	}
	var err error
	s.Trusted, err = Trusted(ctx, a.PublicPath())
	return s, err
}

func (a *Authority) public() ([]byte, error) {
	a.mu.RLock()
	root := a.root
	a.mu.RUnlock()
	if root == nil {
		return nil, errors.New("尚未创建根证书")
	}
	b := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: root.Raw})
	// Detect external replacement; exporting/installing/deleting must refer to
	// the active identity, never another certificate that happens to share a name.
	if disk, err := os.ReadFile(a.PublicPath()); err == nil {
		if !bytes.Equal(bytes.TrimSpace(disk), bytes.TrimSpace(b)) {
			return nil, errors.New("公开证书与当前 CA 不一致")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return b, nil
}

func (a *Authority) Export(path string) error {
	b, err := a.public()
	if err != nil {
		return err
	}
	// Never let an export overwrite the private identity or its lifecycle record.
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	dir, err := filepath.EvalSymlinks(a.directory)
	if err != nil {
		return err
	}
	if rel, e := filepath.Rel(dir, filepath.Join(parent, filepath.Base(path))); e != nil || rel == "." || (rel != ".." && !bytes.HasPrefix([]byte(rel), []byte(".."+string(os.PathSeparator)))) {
		return errors.New("请选择证书目录以外的导出位置")
	}
	return writePublic(path, b)
}

func (a *Authority) Create() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.root != nil {
		return errors.New("请先完成现有根证书的删除")
	}
	ca, err := LoadOrCreate(a.directory)
	if err != nil {
		return err
	}
	// Publish only after persistence succeeds. A crash before this point stays
	// blocked at the next boot and offers deletion retry instead of signing.
	if err = os.Remove(a.tombstone()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	a.root, a.ca, a.pending = ca.GetRootCA(), ca, false
	return syncDirectory(a.directory)
}

func (a *Authority) Delete(ctx context.Context, disconnect func()) error {
	a.mu.RLock()
	root := a.root
	a.mu.RUnlock()
	if root == nil {
		return nil
	}
	b, err := a.public()
	if err != nil {
		return err
	}
	if err = writePublic(a.tombstone(), []byte(fmt.Sprintf("%X\n", sha256.Sum256(root.Raw)))); err != nil {
		return err
	}
	a.mu.Lock()
	a.ca, a.pending = nil, true
	a.mu.Unlock()
	disconnect()
	// Keep a public copy available for an OS removal retry after a partial file
	// cleanup. This never recreates or reloads the private identity.
	if err = writePublic(a.PublicPath(), b); err != nil {
		return err
	}
	if err = Remove(ctx, a.PublicPath()); err != nil {
		return err
	}
	for _, name := range []string{"mitmproxy-ca-cert.cer", "mitmproxy-ca-cert.pem", "mitmproxy-ca.pem"} {
		path := filepath.Join(a.directory, name)
		if disk, e := os.ReadFile(path); e == nil {
			matched := false
			for len(disk) > 0 {
				block, rest := pem.Decode(disk)
				if block == nil {
					break
				}
				disk = rest
				if block.Type == "CERTIFICATE" {
					matched = bytes.Equal(block.Bytes, root.Raw)
					break
				}
			}
			if !matched {
				return errors.New("证书文件已被替换，已保留文件，请检查后重试")
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err = syncDirectory(a.directory); err != nil {
		return err
	}
	a.mu.Lock()
	a.root, a.pending = nil, false
	a.mu.Unlock()
	return nil
}

func writePublic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".certificate-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
