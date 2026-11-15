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
	"regexp"
	"strings"
	"sync"
	"time"
)

type Descriptor struct {
	Format    string `json:"format,omitempty"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Package   string `json:"package"`
	Version   string `json:"version"`
	Publisher string `json:"publisher"`
	Registry  string `json:"registry"`
}

var Catalog = []Descriptor{
	{ID: "wecom", Name: "企业微信 BOT", Summary: "官方 OpenClaw 企业微信长连接机器人插件", Package: "@wecom/wecom-openclaw-plugin", Version: catalogVersion("2026.10.30"), Publisher: "WecomTeam", Registry: registry()},
	{ID: "wpk", Name: "微扑克", Summary: "将游戏桌的牌局消息接入 Room", Format: "aicat", Package: "@aicat/wpk", Version: "0.1.0", Publisher: "aicat", Registry: registry()},
	{ID: "wecom-local", Name: "企业微信客户端", Summary: "读取客户端会话消息到 Room，经授权可通过客户端发送文本", Format: "aicat", Package: "@aicat/wecom-local", Version: "0.1.0", Publisher: "aicat", Registry: registry()},
}

type Installation struct {
	Package     string `json:"package"`
	Version     string `json:"version"`
	InstalledAt string `json:"installedAt"`
	LockSHA256  string `json:"lockSHA256"`
}

type Store struct {
	home       string
	mu         sync.Mutex
	operations operationState
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

func (s *Store) root(id string) string {
	return filepath.Join(s.home, "extensions", id)
}

func (s *Store) Current(id string) (Installation, error) {
	d, ok := s.find(id)
	if !ok {
		return Installation{}, errors.New("未知渠道插件")
	}
	var installed Installation
	raw, err := os.ReadFile(filepath.Join(s.root(id), "current.json"))
	if err != nil {
		return installed, errors.New("请先安装渠道插件")
	}
	if err = json.Unmarshal(raw, &installed); err != nil || installed.Package != d.Package || !validVersion(installed.Version) {
		return Installation{}, errors.New("渠道安装记录无效，请重新安装")
	}
	return installed, nil
}

var fixedVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

func validVersion(version string) bool {
	return len(version) <= 128 && fixedVersion.MatchString(version)
}

func (s *Store) Directory(id string) string {
	installed, err := s.Current(id)
	if err != nil {
		return ""
	}
	return filepath.Join(s.root(id), installed.Version)
}

func (s *Store) Present(id string) bool {
	if _, ok := s.find(id); !ok {
		return false
	}
	_, err := os.Stat(s.root(id))
	return err == nil
}

func (s *Store) installationEntry(id string) (string, error) {
	d, ok := s.find(id)
	if !ok {
		return "", errors.New("未知渠道插件")
	}
	installed, err := s.Current(id)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.root(id), installed.Version)
	var stamp Installation
	raw, err := os.ReadFile(filepath.Join(dir, "installation.json"))
	if err != nil || json.Unmarshal(raw, &stamp) != nil || stamp != installed {
		return "", errors.New("渠道插件安装记录不一致，请重新安装")
	}
	lock, err := os.ReadFile(filepath.Join(dir, "bun.lock"))
	if err != nil {
		return "", errors.New("渠道插件依赖锁缺失，请重新安装")
	}
	sum := sha256.Sum256(lock)
	if installed.LockSHA256 != hex.EncodeToString(sum[:]) {
		return "", errors.New("渠道插件依赖锁已变更，请重新安装")
	}
	d.Version = installed.Version
	return inspect(dir, d)
}

func (s *Store) Entry(id string) (string, error) {
	entry, err := s.installationEntry(id)
	if err != nil {
		return "", err
	}
	if d, ok := s.find(id); ok && d.Format == "aicat" {
		return entry, nil
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
	if d, ok := s.find(id); ok && d.Format == "aicat" {
		return nil
	}
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
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	resolvedPackage, err := filepath.EvalSymlinks(packageDir)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolvedPackage)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("渠道包目录越界")
	}
	if d.Format == "aicat" {
		_, entry, err := inspectManifest(packageDir, d)
		return entry, err
	}
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
	return s.operate(ctx, id, "install", func(ctx context.Context) error {
		d, ok := s.find(id)
		if !ok {
			return errors.New("未知渠道插件")
		}
		if _, err := s.installationEntry(id); err == nil {
			runtime, err := RuntimeDir()
			if err != nil {
				return err
			}
			if d.Format == "aicat" {
				return nil
			}
			return bindRuntime(s.Directory(id), runtime)
		}
		return s.prepare(ctx, d)
	})
}

func (s *Store) Update(ctx context.Context, id string) error {
	return s.operate(ctx, id, "update", func(ctx context.Context) error {
		d, ok := s.find(id)
		if !ok {
			return errors.New("未知渠道插件")
		}
		if _, err := s.installationEntry(id); err != nil {
			return err
		}
		installed, err := s.Current(id)
		if err != nil {
			return err
		}
		if installed.Version == d.Version {
			return nil
		}
		return s.prepare(ctx, d)
	})
}

func (s *Store) Uninstall(ctx context.Context, id string) error {
	return s.operate(ctx, id, "uninstall", func(ctx context.Context) error {
		s.stage(id, "removing")
		if _, ok := s.find(id); !ok {
			return errors.New("未知渠道插件")
		}
		if err := os.Remove(filepath.Join(s.root(id), "current.json")); err != nil && !os.IsNotExist(err) {
			return err
		}
		return os.RemoveAll(s.root(id))
	})
}

func (s *Store) prepare(ctx context.Context, d Descriptor) error {
	if !validVersion(d.Version) {
		return errors.New("渠道版本无效")
	}
	runtime, err := RuntimeDir()
	if err != nil {
		return err
	}
	parent := s.root(d.ID)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	deps := map[string]string{d.Package: d.Version}
	if d.Format != "aicat" {
		deps["openclaw"] = "file:" + filepath.Join(runtime, "shim")
	}
	pkg := map[string]any{"name": "aicat-channel-package", "private": true, "type": "module", "dependencies": deps}
	raw, _ := json.Marshal(pkg)
	if err = os.WriteFile(filepath.Join(stage, "package.json"), raw, 0600); err != nil {
		return err
	}
	installCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	args := []string{"install", "--ignore-scripts", "--save-text-lockfile", "--no-progress", "--registry", d.Registry}
	cmd := exec.CommandContext(installCtx, filepath.Join(runtime, "bun"), args...)
	cmd.Dir = stage
	cmd.Env = []string{"HOME=" + stage, "PATH=" + runtime, "TMPDIR=" + os.TempDir(), "BUN_INSTALL_CACHE_DIR=" + filepath.Join(s.home, "cache", "channel-packages")}
	s.stage(d.ID, "downloading")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(installCtx.Err(), context.DeadlineExceeded) {
			return errors.New("下载插件超时，请检查网络后重试（最长等待 3 分钟）")
		}
		if installCtx.Err() != nil {
			return fmt.Errorf("插件安装中断：%w", installCtx.Err())
		}
		detail := strings.TrimSpace(string(output))
		if len(detail) > 1500 {
			detail = detail[len(detail)-1500:]
		}
		return fmt.Errorf("插件安装失败（%v）：%s", err, detail)
	}
	if err = installCtx.Err(); err != nil {
		return err
	}
	s.stage(d.ID, "verifying")
	if _, err = inspect(stage, d); err != nil {
		return err
	}
	if d.Format != "aicat" {
		if err = bindRuntime(stage, runtime); err != nil {
			return err
		}
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
	if err = installCtx.Err(); err != nil {
		return err
	}
	if err = s.activate(installCtx, d.ID); err != nil {
		return err
	}
	if err = replacePath(filepath.Join(parent, d.Version), stage); err != nil {
		return err
	}
	f, err := os.CreateTemp(parent, ".current-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(parent, "current.json")); err != nil {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
