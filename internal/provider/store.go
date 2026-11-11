// Package provider keeps the model vendors whose API keys aicat holds, and
// forwards local agents' requests to them with the key attached.
//
// Providers live in <Home>/providers.json (mode 0600), apart from aicat.yaml so
// that sharing or backing up the behaviour config never carries a key. Keys
// are exactly what the user typed; they are never read from the environment
// and never returned to the UI.
package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Kind describes one supported vendor protocol: where agents reach it on the
// gateway, the vendor's default endpoint, and how its key is sent.
type Kind struct {
	ID      string
	Mount   string // path prefix on the gateway
	Default string // default upstream base URL; empty when the user must supply one
	header  string
	prefix  string
}

var kinds = []Kind{
	{ID: "anthropic", Mount: "/anthropic", Default: "https://api.anthropic.com", header: "X-Api-Key"},
	{ID: "openai", Mount: "/openai/v1", Default: "https://api.openai.com/v1", header: "Authorization", prefix: "Bearer "},
	{ID: "compat", Mount: "/compat/v1", header: "Authorization", prefix: "Bearer "},
}

func kind(id string) (Kind, bool) {
	for _, k := range kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

type entry struct {
	BaseURL string `json:"baseUrl"`
	Key     string `json:"key"`
}

// Provider is what the UI may know about a saved provider. The key stays behind;
// KeyHint is its last four characters, enough to tell two keys apart.
type Provider struct {
	ID      string `json:"id"`
	BaseURL string `json:"baseUrl"`
	Mount   string `json:"mount"`
	KeyHint string `json:"keyHint"`
}

type Store struct {
	mu      sync.RWMutex
	path    string
	entries map[string]entry
}

// Open reads the saved providers. A missing file is an empty store; a file
// that others can read is tightened to 0600 before use.
func Open(home string) (*Store, error) {
	s := &Store{path: filepath.Join(home, "providers.json"), entries: map[string]entry{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(s.path, 0600); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &s.entries); err != nil {
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}
	return s, nil
}

func (s *Store) List() []Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	views := make([]Provider, 0, len(s.entries))
	for id, e := range s.entries {
		k, _ := kind(id)
		hint := e.Key
		if len(hint) > 4 {
			hint = hint[len(hint)-4:]
		}
		views = append(views, Provider{ID: id, BaseURL: e.BaseURL, Mount: k.Mount, KeyHint: hint})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	return views
}

// Set saves a provider. An empty key keeps the one already saved, so the UI
// can change the endpoint without the key ever travelling back to it.
func (s *Store) Set(id, baseURL, key string) error {
	k, ok := kind(id)
	if !ok {
		return fmt.Errorf("未知的供应商：%s", id)
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = k.Default
	}
	if err := validateBaseURL(baseURL); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if strings.ContainsAny(key, " \t\r\n") {
		return errors.New("API Key 不能包含空白字符")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "" {
		key = s.entries[id].Key
	}
	if key == "" {
		return errors.New("接口地址和 API Key 都要填")
	}
	next := s.copyLocked()
	next[id] = entry{BaseURL: baseURL, Key: key}
	return s.saveLocked(next)
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return nil
	}
	next := s.copyLocked()
	delete(next, id)
	return s.saveLocked(next)
}

func (s *Store) copyLocked() map[string]entry {
	next := make(map[string]entry, len(s.entries)+1)
	for id, e := range s.entries {
		next[id] = e
	}
	return next
}

// saveLocked replaces the file atomically; memory changes only after the
// write succeeds, so a failed save leaves the running routes as they were.
func (s *Store) saveLocked(next map[string]entry) error {
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".providers-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(data, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), s.path); err != nil {
		return err
	}
	s.entries = next
	return nil
}

// validateBaseURL requires https, so a key is never sent in the clear; plain
// http is accepted only for a service on this machine.
func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("接口地址需要是完整的 URL，例如 https://api.example.com/v1")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if ip := net.ParseIP(host); host == "localhost" || ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return errors.New("接口地址需要使用 https；http 只允许本机地址")
}

// route finds the provider mounted at path and returns what follows the mount.
func (s *Store) route(path string) (Kind, entry, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range kinds {
		rest, ok := strings.CutPrefix(path, k.Mount)
		if !ok || rest != "" && !strings.HasPrefix(rest, "/") {
			continue
		}
		e, saved := s.entries[k.ID]
		return k, e, rest, saved
	}
	return Kind{}, entry{}, "", false
}

// Mounts lists the gateway path prefixes that belong to providers.
func Mounts() []string {
	mounts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		mounts = append(mounts, k.Mount)
	}
	return mounts
}
