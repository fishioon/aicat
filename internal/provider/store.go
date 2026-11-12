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
	"strings"
	"sync"
	"time"
)

// protocol is how a vendor is spoken to: how its key is sent, and whether
// it serves the OpenAI Responses API, so its models can be offered to Codex.
type protocol struct {
	header, prefix string
	responses      bool
}

var protocols = map[string]protocol{
	"anthropic": {header: "X-Api-Key"},
	"openai":    {header: "Authorization", prefix: "Bearer ", responses: true},
	// chat: OpenAI Chat Completions only
	"chat": {header: "Authorization", prefix: "Bearer "},
}

// Mount is the gateway path prefix of the provider routes: /p/<id> stands
// for the provider's base URL.
const Mount = "/p/"

type entry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Preset is the catalog entry the page made it from; empty for a custom
	// one. The store only keeps it.
	Preset   string `json:"preset,omitempty"`
	Protocol string `json:"protocol"`
	BaseURL  string `json:"baseUrl"`
	Key      string `json:"key"`
}

type file struct {
	Providers []entry `json:"providers"`
}

// Input is a provider as the page saves it. An empty ID adds one; an empty
// Key keeps the saved key.
type Input struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Preset   string `json:"preset"`
	Protocol string `json:"protocol"`
	BaseURL  string `json:"baseUrl"`
	Key      string `json:"key"`
}

// Provider is what the UI may know about a saved provider. The key stays behind;
// KeyHint is its last four characters, enough to tell two keys apart.
type Provider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Preset   string `json:"preset"`
	Protocol string `json:"protocol"`
	BaseURL  string `json:"baseUrl"`
	Mount    string `json:"mount"`
	KeyHint  string `json:"keyHint"`
	// Responses: the provider's models can be offered to Codex.
	Responses bool      `json:"responses"`
	Models    ModelList `json:"models"`
	Syncing   bool      `json:"syncing"`
}

type Store struct {
	mu         sync.RWMutex
	path       string
	modelsPath string
	entries    []entry // in the order they were added
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
		models: map[string]ModelList{}, generation: map[string]uint64{}, syncing: map[string]bool{},
		client: &http.Client{Transport: direct, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	data, err := os.ReadFile(s.path)
	if err == nil {
		if err = os.Chmod(s.path, 0600); err != nil {
			return nil, err
		}
		if s.entries, err = parse(data); err != nil {
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
		if s.find(id) < 0 {
			delete(s.models, id)
		}
	}
	return s, nil
}

// parse reads providers.json. A file from before providers were named, one
// entry per vendor kind, is read as three providers named after the kinds;
// it is rewritten on the next save.
func parse(data []byte) ([]entry, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	if _, ok := top["providers"]; ok || len(top) == 0 {
		var f file
		err := json.Unmarshal(data, &f)
		return f.Providers, err
	}
	var entries []entry
	for _, old := range []entry{
		{ID: "anthropic", Name: "Anthropic", Preset: "anthropic", Protocol: "anthropic"},
		{ID: "openai", Name: "OpenAI", Preset: "openai", Protocol: "openai"},
		{ID: "compat", Name: "OpenAI-compatible", Protocol: "openai"},
	} {
		raw, ok := top[old.ID]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, &old); err != nil {
			return nil, err
		}
		entries = append(entries, old)
	}
	return entries, nil
}

// OnChange is told when a model list sync starts or ends, outside any save
// the caller made itself.
func (s *Store) OnChange(fn func()) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

func (s *Store) find(id string) int {
	return slices.IndexFunc(s.entries, func(e entry) bool { return e.ID == id })
}

func (s *Store) List() []Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	views := make([]Provider, 0, len(s.entries))
	for _, e := range s.entries {
		hint := e.Key
		if len(hint) > 4 {
			hint = hint[len(hint)-4:]
		}
		list := s.models[e.ID]
		list.Models = slices.Clone(list.Models)
		views = append(views, Provider{ID: e.ID, Name: e.Name, Preset: e.Preset, Protocol: e.Protocol, BaseURL: e.BaseURL, Mount: Mount + e.ID,
			KeyHint: hint, Responses: protocols[e.Protocol].responses, Models: list, Syncing: s.syncing[e.ID]})
	}
	return views
}

// Save adds a provider or changes a saved one, and returns its ID. An empty
// key keeps the one already saved, so the UI can change the endpoint without
// the key ever travelling back to it.
func (s *Store) Save(in Input) (string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len([]rune(name)) > 40 {
		return "", errors.New("供应商名称需要填写，且不超过 40 个字符")
	}
	if _, ok := protocols[in.Protocol]; !ok {
		return "", fmt.Errorf("未知的接口协议：%s", in.Protocol)
	}
	baseURL := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if baseURL != "" && !strings.Contains(baseURL, "://") {
		baseURL = "https://" + baseURL
	}
	if in.Protocol == "anthropic" {
		// agents add /v1/messages themselves
		baseURL = strings.TrimSuffix(strings.TrimSuffix(baseURL, "/messages"), "/v1")
	}
	if err := validateBaseURL(baseURL); err != nil {
		return "", err
	}
	key := strings.TrimSpace(in.Key)
	if strings.ContainsAny(key, " \t\r\n") {
		return "", errors.New("API Key 不能包含空白字符")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := slices.Clone(s.entries)
	e := entry{ID: in.ID, Name: name, Preset: in.Preset, Protocol: in.Protocol, BaseURL: baseURL, Key: key}
	var old entry
	if in.ID == "" {
		e.ID = s.newIDLocked(firstOf(in.Preset, name))
		next = append(next, e)
	} else {
		i := s.find(in.ID)
		if i < 0 {
			return "", fmt.Errorf("供应商 %s 尚未保存", in.ID)
		}
		old = next[i]
		if e.Key == "" {
			e.Key = old.Key
		}
		next[i] = e
	}
	if e.Key == "" {
		return "", errors.New("接口地址和 API Key 都要填")
	}
	if err := s.saveLocked(next); err != nil {
		return "", err
	}
	// Another endpoint, protocol or key may serve other models: the old list
	// is forgotten and the new one asked for.
	if old.BaseURL != e.BaseURL || old.Key != e.Key || old.Protocol != e.Protocol {
		s.forgetModelsLocked(e.ID)
		s.syncLocked(e.ID)
	}
	return e.ID, nil
}

// newIDLocked makes an unused ID from a preset or a name: the ID is the
// provider's path on the gateway and the prefix of its models in Codex, so
// it is plain ASCII and never changes afterwards.
func (s *Store) newIDLocked(from string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(from) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		id = "provider"
	}
	base := id
	for n := 2; s.find(id) >= 0; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.find(id)
	if i < 0 {
		return nil
	}
	if err := s.saveLocked(slices.Delete(slices.Clone(s.entries), i, i+1)); err != nil {
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
	for _, e := range s.entries {
		if _, ok := s.models[e.ID]; !ok && !s.syncing[e.ID] {
			s.syncLocked(e.ID)
		}
	}
}

// Sync asks the provider for its model list now and waits for the answer. A
// failure keeps the list an earlier sync found, with the error beside it.
func (s *Store) Sync(ctx context.Context, id string) error {
	s.mu.Lock()
	if s.find(id) < 0 {
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
	var e entry
	if i := s.find(id); i >= 0 {
		e = s.entries[i]
	}
	s.mu.RUnlock()
	models, source, err := fetchModels(ctx, s.client, e.Protocol, e.BaseURL, e.Key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation[id] != gen {
		return err
	}
	delete(s.syncing, id)
	list := s.models[id]
	if err != nil {
		list.Error, list.ErrorKind = err.Error(), errorKind(err)
	} else {
		list = ModelList{Models: models, SyncedAt: time.Now(), Source: source}
		// The list answered only under /v1: the address was typed without
		// it, and requests need it as much as the list does.
		if i := s.find(id); i >= 0 && e.Protocol != "anthropic" && source == e.BaseURL+"/v1/models" {
			fixed := slices.Clone(s.entries)
			fixed[i].BaseURL = e.BaseURL + "/v1"
			if saveErr := s.saveLocked(fixed); saveErr != nil {
				log.Printf("provider %s: %v", id, saveErr)
			}
		}
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
	Provider     string // the provider's ID
	ProviderName string
	Mount        string
	// OpenAI: the provider is OpenAI itself, which takes a Codex request
	// with its OpenAI-only fields.
	OpenAI bool
	Model
}

// ResponsesModels lists what Codex may be offered: the models of each saved
// provider that speaks the Responses API, leaving out any its vendor says is
// served on other APIs only.
func (s *Store) ResponsesModels() []ResponsesModel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ResponsesModel
	for _, e := range s.entries {
		if !protocols[e.Protocol].responses {
			continue
		}
		u, _ := url.Parse(e.BaseURL)
		for _, m := range s.models[e.ID].Models {
			if len(m.APIs) > 0 && !slices.Contains(m.APIs, "responses") {
				continue
			}
			out = append(out, ResponsesModel{Provider: e.ID, ProviderName: e.Name, Mount: Mount + e.ID, OpenAI: u != nil && u.Hostname() == "api.openai.com", Model: m})
		}
	}
	return out
}

// saveLocked replaces the file atomically; memory changes only after the
// write succeeds, so a failed save leaves the running routes as they were.
func (s *Store) saveLocked(next []entry) error {
	if next == nil {
		next = []entry{}
	}
	if err := writeJSON(s.path, file{Providers: next}); err != nil {
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

// route finds the provider mounted at path and returns what follows the
// mount; ok is false when no saved provider is mounted there.
func (s *Store) route(path string) (e entry, rest string, ok bool) {
	after, found := strings.CutPrefix(path, Mount)
	if !found {
		return entry{}, "", false
	}
	id, rest, slash := strings.Cut(after, "/")
	if slash {
		rest = "/" + rest
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := s.find(id)
	if i < 0 {
		return entry{}, "", false
	}
	return s.entries[i], rest, true
}
