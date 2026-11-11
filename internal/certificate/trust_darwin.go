//go:build darwin && !e2e

package certificate

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const TrustSupported = true

// Trusted requires unrestricted current-user root trust and an effective SSL
// evaluation. It never passes -r, which would explicitly supply a trusted root.
func Trusted(ctx context.Context, path string) (bool, error) {
	certificate, err := readCertificate(path)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if trusted, err := alwaysTrusted(ctx, certificate.Raw); err != nil || !trusted {
		return false, err
	}
	command := exec.CommandContext(ctx, "/usr/bin/security", "verify-cert", "-c", path, "-p", "ssl", "-l", "-L", "-q")
	output, err := command.CombinedOutput()
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("检查证书信任：%s (%w)", strings.TrimSpace(string(output)), err)
}

func userTrustEntry(ctx context.Context, der []byte) (trustValue, bool, error) {
	directory, err := os.MkdirTemp("", "aicat-user-trust-")
	if err != nil {
		return trustValue{}, false, err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "trust.plist")
	output, err := exec.CommandContext(ctx, "/usr/bin/security", "trust-settings-export", path).CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if ctx.Err() == nil && errors.As(err, &exit) && exit.ExitCode() == 1 && strings.Contains(string(output), "No Trust Settings were found") {
			return trustValue{}, false, nil
		}
		return trustValue{}, false, commandError(ctx, "读取当前用户证书信任", output, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return trustValue{}, false, err
	}
	var root trustValue
	if err := xml.Unmarshal(data, &root); err != nil {
		return trustValue{}, false, fmt.Errorf("解析当前用户证书信任：%w", err)
	}
	if root.XMLName.Local != "plist" || len(root.Children) != 1 || !root.Children[0].dictionary() {
		return trustValue{}, false, errors.New("无效的当前用户证书信任 plist")
	}
	list, ok := root.Children[0].lookup("trustList")
	if !ok {
		return trustValue{}, false, nil
	}
	if !list.dictionary() {
		return trustValue{}, false, errors.New("无效的当前用户证书信任列表")
	}
	// Exact DER identity; no common-name matching.
	fingerprint := sha1.Sum(der)
	entry, ok := list.lookup(strings.ToUpper(hex.EncodeToString(fingerprint[:])))
	if ok && !entry.dictionary() {
		return trustValue{}, false, errors.New("无效的当前用户证书信任记录")
	}
	return entry, ok, nil
}

func alwaysTrusted(ctx context.Context, der []byte) (bool, error) {
	entry, exists, err := userTrustEntry(ctx, der)
	if err != nil || !exists {
		return false, err
	}

	rules, ok := entry.lookup("trustSettings")
	// An existing trust entry with no rules means Always Trust for all purposes.
	if !ok {
		return true, nil
	}
	if rules.XMLName.Local != "array" {
		return false, errors.New("无效的当前用户证书信任规则")
	}
	if len(rules.Children) == 0 {
		return true, nil
	}
	if len(rules.Children) != 1 || !rules.Children[0].dictionary() {
		return false, nil
	}
	rule := rules.Children[0]
	if len(rule.Children) == 0 {
		return true, nil
	}
	result, ok := rule.lookup("kSecTrustSettingsResult")
	return ok && len(rule.Children) == 2 && result.XMLName.Local == "integer" && strings.TrimSpace(result.Text) == "1", nil
}

// Keep plist types intact: a constrained/denied rule must not look like root trust.
type trustValue struct {
	XMLName  xml.Name
	Text     string       `xml:",chardata"`
	Children []trustValue `xml:",any"`
}

func (value trustValue) lookup(key string) (trustValue, bool) {
	for i := 0; i < len(value.Children); i += 2 {
		if value.Children[i].Text == key {
			return value.Children[i+1], true
		}
	}
	return trustValue{}, false
}

func (value trustValue) dictionary() bool {
	if value.XMLName.Local != "dict" || len(value.Children)%2 != 0 {
		return false
	}
	seen := make(map[string]bool)
	for i := 0; i < len(value.Children); i += 2 {
		key := value.Children[i]
		if key.XMLName.Local != "key" || seen[key.Text] {
			return false
		}
		seen[key.Text] = true
	}
	return true
}
