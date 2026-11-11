//go:build darwin && !e2e

package certificate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/pem"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func defaultKeychain(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "/usr/bin/security", "default-keychain", "-d", "user").CombinedOutput()
	if err != nil {
		return "", commandError(ctx, "读取用户默认钥匙串", output, err)
	}
	p := strings.TrimSpace(string(output))
	if len(p) < 2 || p[0] != '"' || p[len(p)-1] != '"' || !filepath.IsAbs(p[1:len(p)-1]) {
		return "", errors.New("无法识别用户默认钥匙串路径")
	}
	return p[1 : len(p)-1], nil
}

// Remove revokes this root's user-domain trust and deletes its exact public
// identity from the current user keychain search list. System/admin stores are untouched.
func Remove(ctx context.Context, path string) error {
	root, err := readCertificate(path)
	if err != nil {
		return err
	}
	keychains, err := userKeychains(ctx)
	if err != nil {
		return err
	}
	if _, exists, e := userTrustEntry(ctx, root.Raw); e != nil {
		return e
	} else if exists {
		out, e := exec.CommandContext(ctx, "/usr/bin/security", "remove-trusted-cert", path).CombinedOutput()
		if e != nil {
			return commandError(ctx, "移除当前用户证书信任", out, e)
		}
	}
	hash := fmt.Sprintf("%X", sha256.Sum256(root.Raw))
	for _, keychain := range keychains {
		present, e := keychainContains(ctx, keychain, root.Raw)
		if e != nil {
			return e
		}
		if present {
			out, e := exec.CommandContext(ctx, "/usr/bin/security", "delete-certificate", "-Z", hash, keychain).CombinedOutput()
			if e != nil {
				return commandError(ctx, "删除钥匙串中的根证书", out, e)
			}
		}
		present, e = keychainContains(ctx, keychain, root.Raw)
		if e != nil {
			return e
		}
		if present {
			return errors.New("钥匙串仍保留根证书，请重试删除")
		}
	}
	if _, exists, e := userTrustEntry(ctx, root.Raw); e != nil {
		return e
	} else if exists {
		return errors.New("系统仍保留根证书的用户信任，请重试删除")
	}
	return nil
}

func userKeychains(ctx context.Context) ([]string, error) {
	primary, err := defaultKeychain(ctx)
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "/usr/bin/security", "list-keychains", "-d", "user").CombinedOutput()
	if err != nil {
		return nil, commandError(ctx, "读取当前用户钥匙串", out, err)
	}
	result := []string{}
	seen := map[string]bool{}
	for _, line := range append([]string{strconv.Quote(primary)}, strings.Split(string(out), "\n")...) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p, e := strconv.Unquote(strings.TrimSpace(line))
		if e != nil || !filepath.IsAbs(p) {
			return nil, errors.New("无法识别当前用户钥匙串路径")
		}
		p = filepath.Clean(p)
		// User search lists can include system stores; never mutate them.
		if strings.HasPrefix(p, "/System/") || strings.HasPrefix(p, "/Library/Keychains/") || seen[p] {
			continue
		}
		seen[p] = true
		result = append(result, p)
	}
	return result, nil
}

func keychainContains(ctx context.Context, keychain string, der []byte) (bool, error) {
	out, err := exec.CommandContext(ctx, "/usr/bin/security", "find-certificate", "-a", "-p", keychain).CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if ctx.Err() == nil && errors.As(err, &exit) && exit.ExitCode() == 44 {
			return false, nil
		} // errSecItemNotFound
		return false, commandError(ctx, "读取钥匙串中的证书", out, err)
	}
	for len(out) > 0 {
		block, rest := pem.Decode(out)
		if block == nil {
			break
		}
		out = rest
		if block.Type == "CERTIFICATE" && bytes.Equal(block.Bytes, der) {
			return true, nil
		}
	}
	return false, nil
}
