//go:build darwin && !e2e

package certificate

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Install passes only the public CA to Apple's current-user installer. macOS
// owns authentication; no shell, administrator elevation or private key is used.
func Install(ctx context.Context, path string) error {
	cert, err := readCertificate(path)
	if err != nil {
		return err
	}
	if err = cert.CheckSignatureFrom(cert); err != nil {
		return errors.New("只能安装自签名的 aicat 根证书")
	}
	output, err := exec.CommandContext(ctx, "/usr/bin/security", "default-keychain", "-d", "user").CombinedOutput()
	if err != nil {
		return commandError(ctx, "读取用户默认钥匙串", output, err)
	}
	keychain := strings.TrimSpace(string(output))
	if len(keychain) < 2 || keychain[0] != '"' || keychain[len(keychain)-1] != '"' {
		return errors.New("无法识别用户默认钥匙串路径")
	}
	keychain = keychain[1 : len(keychain)-1]
	if !filepath.IsAbs(keychain) {
		return errors.New("无法识别用户默认钥匙串路径")
	}
	// Omitting -d selects user trust; omitting -p leaves all purposes unrestricted.
	output, err = exec.CommandContext(ctx, "/usr/bin/security", "add-trusted-cert", "-r", "trustRoot", "-k", keychain, path).CombinedOutput()
	if err != nil {
		return commandError(ctx, "安装并信任当前用户证书", output, err)
	}
	return nil
}

func commandError(ctx context.Context, action string, output []byte, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	message := strings.TrimSpace(string(output))
	if strings.Contains(message, "User canceled") || strings.Contains(message, "user canceled") || strings.Contains(message, "canceled by the user") || strings.Contains(message, "(-128)") || strings.Contains(message, "(-60006)") {
		return ErrCanceled
	}
	return fmt.Errorf("%s：%s (%w)", action, message, err)
}
