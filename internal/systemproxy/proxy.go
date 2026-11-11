// Package systemproxy owns macOS proxy changes and their durable undo record.
package systemproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fishioon/aicat/internal/listenaddr"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type service struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Proxies  map[string]any `json:"proxies"`
	Disabled bool           `json:"disabled,omitempty"`
	Unlisted bool           `json:"unlisted,omitempty"`
}

type change struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Before   map[string]any `json:"before"`
	After    map[string]any `json:"after"`
	Progress *writeProgress `json:"progress,omitempty"`
}

// A command can finish before the process records its result. Both dictionaries
// are durable before execution, so a crash on either side remains recoverable.
type writeProgress struct {
	Current map[string]any `json:"current"`
	Pending map[string]any `json:"pending,omitempty"`
}

type snapshot struct {
	Version  int      `json:"version"`
	OwnerPID int      `json:"ownerPid"`
	Changes  []change `json:"changes"`
}

// Status distinguishes an active session from a recovery record awaiting repair.
type Status struct {
	Supported      bool     `json:"supported"`
	Enabled        bool     `json:"enabled"`
	Recovery       bool     `json:"recovery"`
	Services       []string `json:"services"`
	Problem        string   `json:"problem,omitempty"`
	OwnedElsewhere bool     `json:"ownedElsewhere,omitempty"`
}

// Manager serializes changes from both windows and saves undo data before writing.
type Manager struct {
	mu   sync.Mutex
	path string
}

var ErrOwnedElsewhere = errors.New("系统代理由另一个正在运行的 aicat 进程管理")

func New(home string) *Manager {
	return &Manager{path: filepath.Join(home, "state", "system-proxy.json")}
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{Supported: supported, Services: []string{}}
	saved, err := m.load()
	if errors.Is(err, os.ErrNotExist) {
		return status
	}
	if err != nil {
		status.Recovery = true
		status.Problem = err.Error()
		return status
	}
	status.Recovery = true
	if saved.OwnerPID != 0 && saved.OwnerPID != os.Getpid() && ownerAlive(saved.OwnerPID) {
		status.OwnedElsewhere = true
		status.Problem = ErrOwnedElsewhere.Error()
		return status
	}
	current, err := readServices()
	if err != nil {
		status.Problem = err.Error()
		return status
	}
	byID := index(current)
	interrupted := false
	for _, c := range saved.Changes {
		if now, ok := byID[c.ID]; ok {
			if !now.Disabled && !now.Unlisted && equal(now.Proxies, c.After) {
				status.Services = append(status.Services, c.Name)
			} else if c.Progress != nil && ownedState(now.Proxies, c) && !equal(now.Proxies, c.Before) {
				interrupted = true
			}
		}
	}
	status.Enabled = len(status.Services) > 0
	if len(status.Services) != len(saved.Changes) {
		status.Problem = "部分网络设置已在 aicat 外修改；恢复时会保留这些修改。"
		if interrupted {
			status.Problem = "系统代理操作未完成，已保留逐命令恢复记录，请恢复原设置。"
		}
	}
	return status
}

func (m *Manager) Enable(addr string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !supported {
		return errors.New("当前平台不支持系统代理")
	}
	if _, err := m.load(); err == nil {
		return errors.New("已有代理恢复记录，请先恢复原设置")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	host, rawPort, err := net.SplitHostPort(addr)
	ip, _, parseErr := listenaddr.Parse(addr)
	if err != nil || parseErr != nil || !listenaddr.IsLocal(ip) {
		return errors.New("系统代理只能指向本机网关地址")
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("无效的网关端口")
	}
	services, err := readServices()
	if err != nil {
		return err
	}
	saved := snapshot{Version: 1, OwnerPID: os.Getpid()}
	for _, s := range services {
		if s.Disabled || s.Unlisted {
			continue
		}
		before := s.Proxies
		if before == nil {
			before = map[string]any{}
		}
		after := clone(before)
		for _, key := range []string{"ProxyAutoConfigEnable", "ProxyAutoDiscoveryEnable", "SOCKSEnable"} {
			after[key] = float64(0)
		}
		for _, prefix := range []string{"HTTP", "HTTPS"} {
			after[prefix+"Enable"] = float64(1)
			after[prefix+"Proxy"] = host
			after[prefix+"Port"] = float64(port)
		}
		// Authentication belongs to the previous proxy, never to aicat's listener.
		for _, key := range []string{"HTTPUser", "HTTPPassword", "HTTPSUser", "HTTPSPassword"} {
			delete(after, key)
		}
		if !equal(before, after) {
			saved.Changes = append(saved.Changes, change{ID: s.ID, Name: s.Name, Before: before, After: after})
		}
	}
	if len(saved.Changes) == 0 {
		return errors.New("没有可接管的网络服务，或代理已由其它实例指向 aicat")
	}
	if err := validateChanges(saved.Changes); err != nil {
		return err
	}
	// Original settings must be restorable before takeover. Recovery itself
	// may start from an enabled empty endpoint left by an interrupted CLI clear.
	for _, c := range saved.Changes {
		if _, err := proxyCommands(change{Name: c.Name, Before: c.After, After: c.Before}); err != nil {
			return fmt.Errorf("%s 原设置无法安全恢复: %w", c.Name, err)
		}
	}
	if err := m.save(saved); err != nil {
		return err
	}
	if err := m.applyLocked(&saved, saved.Changes); err != nil {
		// A commit can fail after reaching disk. Inspect and undo rather than
		// assuming a failed native call left every service unchanged.
		return errors.Join(err, m.restoreLocked())
	}
	return nil
}

// Restore changes only services still matching our write. A conflicting service
// keeps its undo record so a user can resolve it and retry without losing data.
func (m *Manager) Restore() error { m.mu.Lock(); defer m.mu.Unlock(); return m.restoreLocked() }

func (m *Manager) restoreLocked() error {
	saved, err := m.load()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if saved.OwnerPID != 0 && saved.OwnerPID != os.Getpid() && ownerAlive(saved.OwnerPID) {
		return ErrOwnedElsewhere
	}
	current, err := readServices()
	if err != nil {
		return err
	}
	byID := index(current)
	var undo []change
	var conflicts []string
	for _, c := range saved.Changes {
		now, ok := byID[c.ID]
		if !ok {
			continue
		}
		switch {
		case equal(now.Proxies, c.Before):
		case ownedState(now.Proxies, c):
			// Plan from the actual intermediate state, including any fields
			// networksetup normalized on a previous command.
			undo = append(undo, change{ID: c.ID, Name: c.Name, Before: now.Proxies, After: c.Before})
		default:
			conflicts = append(conflicts, c.Name)
		}
	}
	if len(undo) > 0 {
		if err := m.applyLocked(&saved, undo); err != nil {
			return err
		}
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("以下网络服务已被其它应用修改，保留现状与恢复记录：%s", strings.Join(conflicts, "、"))
	}
	if err := os.Remove(m.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func ownedState(current map[string]any, c change) bool {
	if c.Progress == nil {
		return equal(current, c.After)
	}
	return equal(current, c.Progress.Current) || c.Progress.Pending != nil && equal(current, c.Progress.Pending)
}

func (m *Manager) applyLocked(saved *snapshot, changes []change) error {
	return applyChanges(changes, func(id string, progress *writeProgress) error {
		for i := range saved.Changes {
			if saved.Changes[i].ID == id {
				saved.Changes[i].Progress = progress
				return m.save(*saved)
			}
		}
		return errors.New("代理进度没有对应的恢复记录")
	})
}

func (m *Manager) load() (snapshot, error) {
	var s snapshot
	info, err := os.Lstat(m.path)
	if err != nil {
		return s, err
	}
	if !info.Mode().IsRegular() {
		return s, errors.New("代理恢复记录必须是普通文件")
	}
	b, err := os.ReadFile(m.path)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("读取代理恢复记录：%w", err)
	}
	if s.Version != 1 || len(s.Changes) == 0 {
		return s, errors.New("代理恢复记录无效")
	}
	return s, nil
}

func (m *Manager) save(s snapshot) error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Dir(m.path))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("代理恢复目录不能是符号链接")
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".proxy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), m.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(m.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func clone(value map[string]any) map[string]any {
	b, _ := json.Marshal(value)
	var result map[string]any
	_ = json.Unmarshal(b, &result)
	return result
}
func equal(a, b map[string]any) bool {
	a, b = clone(a), clone(b)
	// An absent enable flag and an explicit zero both mean disabled. Compare
	// their meaning while keeping endpoints, bypass lists and other fields
	// exact, so actual external changes still prevent restoration.
	for _, key := range []string{"HTTPEnable", "HTTPSEnable", "SOCKSEnable", "ProxyAutoConfigEnable", "ProxyAutoDiscoveryEnable"} {
		if a[key] == false {
			delete(a, key)
		}
		if b[key] == false {
			delete(b, key)
		}
		if a[key] == true {
			a[key] = float64(1)
		}
		if b[key] == true {
			b[key] = float64(1)
		}
		if a[key] == float64(0) {
			delete(a, key)
		}
		if b[key] == float64(0) {
			delete(b, key)
		}
	}
	// networksetup clears an unset endpoint as empty host/zero port, not by
	// deleting its keys. Only those empty values on a disabled proxy are equal.
	for _, d := range []map[string]any{a, b} {
		for _, prefix := range []string{"HTTP", "HTTPS"} {
			if d[prefix+"Enable"] == nil {
				if d[prefix+"Proxy"] == "" {
					delete(d, prefix+"Proxy")
				}
				if d[prefix+"Port"] == float64(0) {
					delete(d, prefix+"Port")
				}
			}
		}
	}
	return (len(a) == 0 && len(b) == 0) || reflect.DeepEqual(a, b)
}
func index(services []service) map[string]service {
	result := map[string]service{}
	for _, s := range services {
		result[s.ID] = s
	}
	return result
}
func sortServices(services []service) {
	sort.Slice(services, func(i, j int) bool { return services[i].ID < services[j].ID })
}
