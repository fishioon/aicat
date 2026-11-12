// Package provider keeps the model vendors whose API keys aicat holds, and
// forwards local agents' requests to them with the key attached.
//
// Providers live in <Home>/providers.json (mode 0600), apart from aicat.yaml so
// that sharing or backing up the behaviour config never carries a key. Keys
// are exactly what the user typed; they are never read from the environment
// and never returned to the UI.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind describes one supported vendor protocol: where agents reach it on the
// gateway, the vendor's default endpoint, and how its key is sent.
type Kind struct {
	ID      string
	Mount   string // path prefix on the gateway
	Default string // default upstream base URL; empty when the user must supply one
	header  string
	prefix  string
	// responses: the vendor speaks the OpenAI Responses API at its base URL,
	// so its models can be offered to Codex as they are.
	responses bool
}

var kinds = []Kind{
	{ID: "anthropic", Mount: "/anthropic", Default: "https://api.anthropic.com", header: "X-Api-Key"},
	{ID: "openai", Mount: "/openai/v1", Default: "https://api.openai.com/v1", header: "Authorization", prefix: "Bearer ", responses: true},
	{ID: "compat", Mount: "/compat/v1", header: "Authorization", prefix: "Bearer ", responses: true},
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
	// Responses: the provider's models can be offered to Codex.
	Responses bool      `json:"responses"`
	Models    ModelList `json:"models"`
	Syncing   bool      `json:"syncing"`
}

type Store struct {
	mu         sync.RWMutex
	path       string
	modelsPath string
	entries    map[string]entry
	models     map[string]ModelList
	// generation counts the syncs started for each provider; a sync whose
	// generation is no longer current lost to a newer one and is dropped.
	generation map[string]uint64
	syncing    map[string]bool
	client     *http.Client
	onChange   func()
}

// Open reads the saved providers and their last synced model lists. A
// missing file is an empty store; a key file that others can read is
// tightened to 0600 before use.
func Open(home string) (*Store, error) {
	s := &Store{path: filepath.Join(home, "providers.json"), modelsPath: filepath.Join(home, "provider-models.json"),
		entries: map[string]entry{}, models: map[string]ModelList{}, generation: map[string]uint64{}, syncing: map[string]bool{},
		client: &http.Client{Transport: direct, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	data, err := os.ReadFile(s.path)
	if err == nil {
		if err = os.Chmod(s.path, 0600); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &s.entries); err != nil {
			return nil, fmt.Errorf("read %s: %w", s.path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// The model lists are a cache of what the vendors said: an unreadable
	// file is synced again rather than stopping aicat.
	if data, err = os.ReadFile(s.modelsPath); err == nil {
		if json.Unmarshal(data, &s.models) != nil {
			s.models = map[string]ModelList{}
		}
	}
	for id := range s.models {
		if _, ok := s.entries[id]; !ok {
			delete(s.models, id)
		}
	}
	return s, nil
}

// OnChange is told when a model list sync starts or ends, outside any save
// the caller made itself.
func (s *Store) OnChange(fn func()) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
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
		list := s.models[id]
		list.Models = slices.Clone(list.Models)
		views = append(views, Provider{ID: id, BaseURL: e.BaseURL, Mount: k.Mount, KeyHint: hint, Responses: k.responses, Models: list, Syncing: s.syncing[id]})
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
	old, existed := s.entries[id]
	if key == "" {
		key = old.Key
	}
	if key == "" {
		return errors.New("接口地址和 API Key 都要填")
	}
	next := s.copyLocked()
	next[id] = entry{BaseURL: baseURL, Key: key}
	if err := s.saveLocked(next); err != nil {
		return err
	}
	// Another endpoint or key may serve other models: the old list is
	// forgotten and the new one asked for.
	if !existed || old != next[id] {
		s.forgetModelsLocked(id)
		s.syncLocked(id)
	}
	return nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return nil
	}
	next := s.copyLocked()
	delete(next, id)
	if err := s.saveLocked(next); err != nil {
		return err
	}
	s.forgetModelsLocked(id)
	// a sync still running for it is dropped when it ends
	s.generation[id]++
	delete(s.syncing, id)
	return nil
}

// SyncMissing asks each saved provider without a model list for one, in the
// background: providers saved before lists were kept, or whose list was lost.
func (s *Store) SyncMissing() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.entries {
		if _, ok := s.models[id]; !ok && !s.syncing[id] {
			s.syncLocked(id)
		}
	}
}

// Sync asks the provider for its model list now and waits for the answer. A
// failure keeps the list an earlier sync found, with the error beside it.
func (s *Store) Sync(ctx context.Context, id string) error {
	s.mu.Lock()
	if _, ok := s.entries[id]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("供应商 %s 尚未保存", id)
	}
	gen := s.startLocked(id)
	s.mu.Unlock()
	s.changed()
	err := s.sync(ctx, id, gen)
	s.changed()
	return err
}

// syncLocked starts a sync in the background.
func (s *Store) syncLocked(id string) {
	gen := s.startLocked(id)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*fetchTimeout)
		defer cancel()
		if err := s.sync(ctx, id, gen); err != nil {
			log.Printf("provider %s models: %v", id, err)
		}
		s.changed()
	}()
}

func (s *Store) startLocked(id string) uint64 {
	s.generation[id]++
	s.syncing[id] = true
	return s.generation[id]
}

func (s *Store) sync(ctx context.Context, id string, gen uint64) error {
	s.mu.RLock()
	e := s.entries[id]
	s.mu.RUnlock()
	k, _ := kind(id)
	models, source, err := fetchModels(ctx, s.client, k, e.BaseURL, e.Key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation[id] != gen {
		return err
	}
	delete(s.syncing, id)
	list := s.models[id]
	if err != nil {
		list.Error = err.Error()
	} else {
		list = ModelList{Models: models, SyncedAt: time.Now(), Source: source}
	}
	next := make(map[string]ModelList, len(s.models)+1)
	for k, v := range s.models {
		next[k] = v
	}
	next[id] = list
	if saveErr := s.saveModelsLocked(next); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	return err
}

func (s *Store) forgetModelsLocked(id string) {
	if _, ok := s.models[id]; !ok {
		return
	}
	next := make(map[string]ModelList, len(s.models))
	for k, v := range s.models {
		if k != id {
			next[k] = v
		}
	}
	if err := s.saveModelsLocked(next); err != nil {
		log.Printf("provider models: %v", err)
	}
}

func (s *Store) changed() {
	s.mu.RLock()
	fn := s.onChange
	s.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// ResponsesModel is a model of a saved provider that speaks the Responses
// API, as its list has it.
type ResponsesModel struct {
	Provider string
	Mount    string
	Model
}

// ResponsesModels lists what Codex may be offered: the models of each saved
// provider that speaks the Responses API, leaving out any its vendor says is
// served on other APIs only.
func (s *Store) ResponsesModels() []ResponsesModel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ResponsesModel
	for _, k := range kinds {
		if _, saved := s.entries[k.ID]; !saved || !k.responses {
			continue
		}
		for _, m := range s.models[k.ID].Models {
			if len(m.APIs) > 0 && !slices.Contains(m.APIs, "responses") {
				continue
			}
			out = append(out, ResponsesModel{Provider: k.ID, Mount: k.Mount, Model: m})
		}
	}
	return out
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
	if err := writeJSON(s.path, next); err != nil {
		return err
	}
	s.entries = next
	return nil
}

func (s *Store) saveModelsLocked(next map[string]ModelList) error {
	if err := writeJSON(s.modelsPath, next); err != nil {
		return err
	}
	s.models = next
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".providers-*.json")
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
	return os.Rename(f.Name(), path)
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
