package channelplugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Descriptor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Package   string `json:"package"`
	Version   string `json:"version"`
	Publisher string `json:"publisher"`
}

var Catalog = []Descriptor{{ID: "wecom", Name: "企业微信 BOT", Summary: "官方 OpenClaw 企业微信长连接机器人插件", Package: "@wecom/wecom-openclaw-plugin", Version: "2026.10.30", Publisher: "WecomTeam"}}

type Installation struct {
	Package     string `json:"package"`
	Version     string `json:"version"`
	InstalledAt string `json:"installedAt"`
	LockSHA256  string `json:"lockSHA256"`
}

type Store struct {
	home string
	mu   sync.Mutex
}

func New(home string) *Store { return &Store{home: home} }

func RuntimeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	if filepath.Base(dir) == "MacOS" {
		return filepath.Join(filepath.Dir(dir), "Resources", "channel-host"), nil
	}
	return filepath.Join(dir, "channel-host"), nil
}

func Find(id string) (Descriptor, bool) {
	for _, d := range Catalog {
		if d.ID == id {
			return d, true
		}
	}
	return Descriptor{}, false
}

func (s *Store) Directory(id string) string {
	d, ok := Find(id)
	if !ok {
		return ""
	}
	return filepath.Join(s.home, "extensions", d.ID, d.Version)
}

func (s *Store) installationEntry(id string) (string, error) {
	d, ok := Find(id)
	if !ok {
		return "", errors.New("未知渠道插件")
	}
	dir := s.Directory(id)
	var installed Installation
	raw, err := os.ReadFile(filepath.Join(dir, "installation.json"))
	if err != nil || json.Unmarshal(raw, &installed) != nil || installed.Package != d.Package || installed.Version != d.Version {
		return "", errors.New("请先安装渠道插件")
	}
	lock, err := os.ReadFile(filepath.Join(dir, "bun.lock"))
	if err != nil {
		return "", errors.New("渠道插件依赖锁缺失，请重新安装")
	}
	sum := sha256.Sum256(lock)
	if installed.LockSHA256 != hex.EncodeToString(sum[:]) {
		return "", errors.New("渠道插件依赖锁已变更，请重新安装")
	}
	return inspect(dir, d)
}

func (s *Store) Entry(id string) (string, error) {
	entry, err := s.installationEntry(id)
	if err != nil {
		return "", err
	}
	runtime, err := RuntimeDir()
	if err != nil {
		return "", err
	}
	if !runtimeBound(s.Directory(id), runtime) {
		return "", errors.New("渠道兼容层需要更新，请重新安装")
	}
	return entry, nil
}

func (s *Store) RefreshRuntime(id string) error {
	if _, err := s.installationEntry(id); err != nil {
		return nil
	}
	runtime, err := RuntimeDir()
	if err != nil {
		return err
	}
	return bindRuntime(s.Directory(id), runtime)
}

func runtimeBound(dir, runtime string) bool {
	target := filepath.Join(runtime, "shim")
	link, err := os.Readlink(filepath.Join(dir, "node_modules", "openclaw"))
	if err != nil || link != target {
		return false
	}
	_, err = os.Stat(filepath.Join(target, "sdk.mjs"))
	return err == nil
}

func bindRuntime(dir, runtime string) error {
	if runtimeBound(dir, runtime) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(runtime, "shim", "package.json")); err != nil {
		return err
	}
	modules := filepath.Join(dir, "node_modules")
	link := filepath.Join(modules, ".openclaw-next")
	_ = os.Remove(link)
	if err := os.Symlink(filepath.Join(runtime, "shim"), link); err != nil {
		return err
	}
	defer os.Remove(link)
	return replacePath(filepath.Join(modules, "openclaw"), link)
}

func replacePath(target, next string) error {
	parent := filepath.Dir(target)
	backup := ""
	if _, err := os.Lstat(target); err == nil {
		backup, err = os.MkdirTemp(parent, ".replaced-")
		if err != nil {
			return err
		}
		if err = os.Remove(backup); err != nil {
			return err
		}
		if err = os.Rename(target, backup); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(next, target); err != nil {
		if backup != "" {
			return errors.Join(err, os.Rename(backup, target))
		}
		return err
	}
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func inspect(dir string, d Descriptor) (string, error) {
	packageDir := filepath.Join(dir, "node_modules", filepath.FromSlash(d.Package))
	var manifest struct {
		Name     string `json:"name"`
		Version  string `json:"version"`
		OpenClaw struct {
			Extensions []string `json:"extensions"`
		} `json:"openclaw"`
	}
	raw, err := os.ReadFile(filepath.Join(packageDir, "package.json"))
	if err != nil {
		return "", err
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return "", err
	}
	if manifest.Name != d.Package || manifest.Version != d.Version || len(manifest.OpenClaw.Extensions) != 1 {
		return "", errors.New("渠道插件声明与安装目录不一致")
	}
	var plugin struct {
		ID       string   `json:"id"`
		Channels []string `json:"channels"`
	}
	raw, err = os.ReadFile(filepath.Join(packageDir, "openclaw.plugin.json"))
	if err != nil || json.Unmarshal(raw, &plugin) != nil || plugin.ID == "" {
		return "", errors.New("渠道插件 manifest 无效")
	}
	matched := false
	for _, channel := range plugin.Channels {
		if channel == d.ID {
			matched = true
		}
	}
	if !matched {
		return "", errors.New("渠道插件未声明目标渠道")
	}
	entry := filepath.Join(packageDir, manifest.OpenClaw.Extensions[0])
	rel, err := filepath.Rel(packageDir, entry)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("插件入口越界")
	}
	resolved, err := filepath.EvalSymlinks(entry)
	if err != nil {
		return "", err
	}
	rel, err = filepath.Rel(packageDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("插件入口越界")
	}
	if info, err := os.Stat(resolved); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("插件入口不可用")
	}
	return resolved, nil
}

func (s *Store) Install(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := Find(id)
	if !ok {
		return errors.New("未知渠道插件")
	}
	runtime, err := RuntimeDir()
	if err != nil {
		return err
	}
	if _, err = s.installationEntry(id); err == nil {
		return bindRuntime(s.Directory(id), runtime)
	}
	parent := filepath.Dir(s.Directory(id))
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	pkg := map[string]any{"name": "aicat-channel-package", "private": true, "type": "module", "dependencies": map[string]string{d.Package: d.Version, "openclaw": "file:" + filepath.Join(runtime, "shim")}}
	raw, _ := json.Marshal(pkg)
	if err = os.WriteFile(filepath.Join(stage, "package.json"), raw, 0600); err != nil {
		return err
	}
	installCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(installCtx, filepath.Join(runtime, "bun"), "install", "--ignore-scripts", "--save-text-lockfile", "--no-progress", "--registry", registry())
	cmd.Dir = stage
	cmd.Env = []string{"HOME=" + stage, "PATH=" + runtime, "TMPDIR=" + os.TempDir(), "BUN_INSTALL_CACHE_DIR=" + filepath.Join(s.home, "cache", "channel-packages")}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if installCtx.Err() != nil {
			return fmt.Errorf("插件安装中断：%w", installCtx.Err())
		}
		detail := strings.TrimSpace(string(output))
		if len(detail) > 1500 {
			detail = detail[len(detail)-1500:]
		}
		return fmt.Errorf("插件安装失败（%v）：%s", err, detail)
	}
	if _, err = inspect(stage, d); err != nil {
		return err
	}
	if err = bindRuntime(stage, runtime); err != nil {
		return err
	}
	lock, err := os.ReadFile(filepath.Join(stage, "bun.lock"))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(lock)
	installed := Installation{Package: d.Package, Version: d.Version, InstalledAt: time.Now().UTC().Format(time.RFC3339Nano), LockSHA256: hex.EncodeToString(sum[:])}
	raw, _ = json.Marshal(installed)
	if err = os.WriteFile(filepath.Join(stage, "installation.json"), raw, 0600); err != nil {
		return err
	}
	return replacePath(s.Directory(id), stage)
}
