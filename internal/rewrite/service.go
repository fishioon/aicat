package rewrite

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/plugins"
	"golang.org/x/net/http/httpguts"
	"golang.org/x/net/idna"
)

var ErrConflict = errors.New("rules changed; read the current revision before saving")

type Service struct {
	mu                                sync.Mutex
	home                              string
	cfg                               Config
	program                           *program
	save                              func(Config) error
	testSlots                         chan struct{}
	revision, activeRevision, problem string
}

func defaults(c Config) Config {
	if c.Timeout == 0 {
		c.Timeout = 200 * time.Millisecond
	}
	if c.MemoryBytes == 0 {
		c.MemoryBytes = 32 << 20
	}
	if c.MaxConcurrent == 0 {
		c.MaxConcurrent = 8
	}
	if c.MaxBodyBytes == 0 {
		c.MaxBodyBytes = 1 << 20
	}
	if c.Rules == nil {
		c.Rules = []Rule{}
	}
	return c
}
func Validate(c Config) error {
	c = defaults(c)
	if c.Timeout < 10*time.Millisecond || c.Timeout > 5*time.Second {
		return errors.New("rewrite timeout must be 10ms..5s")
	}
	if c.MemoryBytes < 4<<20 || c.MemoryBytes > 128<<20 {
		return errors.New("rewrite memoryBytes must be 4..128 MiB")
	}
	if c.MaxConcurrent < 1 || c.MaxConcurrent > 32 {
		return errors.New("rewrite maxConcurrent must be 1..32")
	}
	if c.MaxBodyBytes < 1 || c.MaxBodyBytes > 8<<20 {
		return errors.New("rewrite maxBodyBytes must be 1..8 MiB")
	}
	if len(c.Rules) > 64 {
		return errors.New("at most 64 rewrite rules are supported")
	}
	seen := map[string]bool{}
	for _, r := range c.Rules {
		if !plugins.ValidID(r.ID) || !plugins.ValidID(r.Plugin) || seen[r.ID] {
			return errors.New("rules require unique valid ids and a valid plugin id")
		}
		seen[r.ID] = true
		if len(r.Name) > 128 || len(r.Match.Hosts) > 32 || len(r.Match.Methods) > 32 || len(r.Match.PathPrefix) > 4096 {
			return errors.New("rule metadata exceeds limit")
		}
		if r.Match.PathPrefix != "" && !strings.HasPrefix(r.Match.PathPrefix, "/") {
			return errors.New("pathPrefix must start with /")
		}
		for _, method := range r.Match.Methods {
			if !httpguts.ValidHeaderFieldName(method) {
				return errors.New("invalid match method")
			}
		}
		for _, host := range r.Match.Hosts {
			h := strings.TrimPrefix(host, "*.")
			if net.ParseIP(h) != nil {
				continue
			}
			ascii, err := idna.Lookup.ToASCII(h)
			if err != nil || ascii != h || strings.ContainsAny(h, "/:?@*\\") {
				return errors.New("match hosts must be ASCII hosts without ports; *.example.com is supported")
			}
			for _, label := range strings.Split(strings.TrimSuffix(h, "."), ".") {
				if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
					return errors.New("invalid match hostname")
				}
			}
		}
		b, err := json.Marshal(r.Settings)
		if err != nil || len(b) > 16384 {
			return errors.New("rule settings must be JSON and at most 16 KiB")
		}
	}
	return nil
}
func revision() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func cloneConfig(c Config) Config {
	b, err := json.Marshal(c)
	if err != nil {
		return c
	}
	var out Config
	_ = json.Unmarshal(b, &out)
	return out
}
func New(ctx context.Context, home string, cfg Config, save func(Config) error) *Service {
	cfg = defaults(cfg)
	s := &Service{home: home, cfg: cfg, save: save, revision: revision(), testSlots: make(chan struct{}, 2)}
	s.activeRevision = s.revision
	s.program = &program{version: s.activeRevision, cfg: cfg, slots: make(chan struct{}, 8), rules: []compiledRule{}, plugins: []plugins.Info{}}
	if err := Validate(cfg); err != nil {
		s.problem = err.Error()
		s.program.cfg.Enabled = false
		return s
	}
	s.program.slots = make(chan struct{}, cfg.MaxConcurrent)
	loaded := map[string]*plugins.Plugin{}
	failures := map[string]string{}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, rule := range cfg.Rules {
		r := compiledRule{rule: rule}
		if cfg.Enabled && rule.Enabled {
			_, exists := loaded[rule.Plugin]
			if !exists {
				p, _, err := plugins.Load(ctx, home, rule.Plugin)
				loaded[rule.Plugin] = p
				if err != nil {
					failures[rule.Plugin] = err.Error()
					s.program.plugins = append(s.program.plugins, plugins.Info{Manifest: plugins.Manifest{ID: rule.Plugin}, Error: err.Error()})
				} else {
					s.program.plugins = append(s.program.plugins, p.Info)
				}
			}
			r.plugin = loaded[rule.Plugin]
			r.problem = failures[rule.Plugin]
		}
		s.program.rules = append(s.program.rules, r)
	}
	return s
}
func (s *Service) Begin() *Session { return s.program.session() }
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{ActiveEnabled: s.program.cfg.Enabled, Config: cloneConfig(s.cfg), Revision: s.revision, ActiveRevision: s.activeRevision, RestartRequired: s.revision != s.activeRevision, Error: s.problem, Rules: []RuleStatus{}, Plugins: slices.Clone(s.program.plugins)}
	active := cloneConfig(s.program.cfg)
	for i, r := range s.program.rules {
		out.Rules = append(out.Rules, RuleStatus{Rule: active.Rules[i], Error: r.problem})
	}
	for i := range out.Plugins {
		out.Plugins[i].Hooks = slices.Clone(out.Plugins[i].Hooks)
	}
	return out
}
func (s *Service) check(expected string) error {
	if expected != "" && expected != s.revision {
		return ErrConflict
	}
	return nil
}
func (s *Service) commit(c Config) error {
	c = defaults(c)
	if err := Validate(c); err != nil {
		return err
	}
	if s.save == nil {
		return errors.New("rule persistence is unavailable")
	}
	if err := s.save(c); err != nil {
		return err
	}
	s.cfg = cloneConfig(c)
	s.revision = revision()
	return nil
}
func (s *Service) SetEnabled(enabled bool, expected string) (Status, error) {
	s.mu.Lock()
	if err := s.check(expected); err != nil {
		s.mu.Unlock()
		return Status{}, err
	}
	c := cloneConfig(s.cfg)
	c.Enabled = enabled
	err := s.commit(c)
	s.mu.Unlock()
	return s.Status(), err
}
func (s *Service) PutRule(rule Rule, expected string) (Status, error) {
	s.mu.Lock()
	if err := s.check(expected); err != nil {
		s.mu.Unlock()
		return Status{}, err
	}
	c := cloneConfig(s.cfg)
	found := false
	for i := range c.Rules {
		if c.Rules[i].ID == rule.ID {
			c.Rules[i] = rule
			found = true
			break
		}
	}
	if !found {
		c.Rules = append(c.Rules, rule)
	}
	err := s.commit(c)
	s.mu.Unlock()
	return s.Status(), err
}
func (s *Service) DeleteRule(id, expected string) (Status, error) {
	s.mu.Lock()
	if err := s.check(expected); err != nil {
		s.mu.Unlock()
		return Status{}, err
	}
	c := cloneConfig(s.cfg)
	i := slices.IndexFunc(c.Rules, func(r Rule) bool { return r.ID == id })
	if i < 0 {
		s.mu.Unlock()
		return Status{}, os.ErrNotExist
	}
	c.Rules = slices.Delete(c.Rules, i, i+1)
	err := s.commit(c)
	s.mu.Unlock()
	return s.Status(), err
}
func (s *Service) EnableRule(id string, enabled bool, expected string) (Status, error) {
	s.mu.Lock()
	if err := s.check(expected); err != nil {
		s.mu.Unlock()
		return Status{}, err
	}
	c := cloneConfig(s.cfg)
	found := false
	for i := range c.Rules {
		if c.Rules[i].ID == id {
			c.Rules[i].Enabled = enabled
			found = true
			break
		}
	}
	if !found {
		s.mu.Unlock()
		return Status{}, os.ErrNotExist
	}
	err := s.commit(c)
	s.mu.Unlock()
	return s.Status(), err
}
func (s *Service) Reorder(ids []string, expected string) (Status, error) {
	s.mu.Lock()
	if err := s.check(expected); err != nil {
		s.mu.Unlock()
		return Status{}, err
	}
	c := cloneConfig(s.cfg)
	if len(ids) != len(c.Rules) {
		s.mu.Unlock()
		return Status{}, errors.New("order must contain every rule id exactly once")
	}
	original := map[string]Rule{}
	for _, r := range c.Rules {
		original[r.ID] = r
	}
	next := []Rule{}
	for _, id := range ids {
		r, ok := original[id]
		if !ok {
			s.mu.Unlock()
			return Status{}, errors.New("invalid or duplicate rule id in order")
		}
		next = append(next, r)
		delete(original, id)
	}
	c.Rules = next
	err := s.commit(c)
	s.mu.Unlock()
	return s.Status(), err
}
func (s *Service) ReadPlugin(ctx context.Context, id string) (plugins.Info, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, source, err := s.readPlugin(ctx, id)
	return info, source, s.revision, err
}
func (s *Service) readPlugin(ctx context.Context, id string) (plugins.Info, string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	m, code, err := plugins.Read(s.home, id)
	if err != nil {
		return plugins.Info{}, "", err
	}
	p, err := plugins.Compile(ctx, m, code)
	if err != nil {
		return plugins.Info{Manifest: m, Hooks: []string{}, Error: err.Error()}, code, nil
	}
	return p.Info, code, nil
}
func (s *Service) Plugins(ctx context.Context) []plugins.Info {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	root, err := os.OpenRoot(s.home)
	if err != nil {
		return []plugins.Info{}
	}
	defer root.Close()
	f, err := root.Open("plugins")
	if err != nil {
		return []plugins.Info{}
	}
	defer f.Close()
	entries, err := f.ReadDir(65)
	if err != nil && len(entries) == 0 {
		return []plugins.Info{}
	}
	infos := []plugins.Info{}
	for _, e := range entries {
		if ctx.Err() != nil {
			break
		}
		if !plugins.ValidID(e.Name()) || !e.IsDir() {
			continue
		}
		info, _, _, err := s.ReadPlugin(ctx, e.Name())
		if err != nil {
			info = plugins.Info{Manifest: plugins.Manifest{ID: e.Name()}, Error: err.Error()}
		}
		infos = append(infos, info)
		if len(infos) == 64 {
			break
		}
	}
	slices.SortFunc(infos, func(a, b plugins.Info) int { return strings.Compare(a.ID, b.ID) })
	return infos
}
func (s *Service) PutPlugin(ctx context.Context, m plugins.Manifest, source, expected string) (plugins.Info, string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	p, err := plugins.Compile(ctx, m, source)
	if err != nil {
		return plugins.Info{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.check(expected); err != nil {
		return plugins.Info{}, "", err
	}
	root, err := os.OpenRoot(s.home)
	if err != nil {
		return plugins.Info{}, "", err
	}
	defer root.Close()
	directory := filepath.Join("plugins", m.ID)
	if err = root.MkdirAll(directory, 0700); err != nil {
		return plugins.Info{}, "", err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err = atomicFile(root, filepath.Join(directory, m.Main), []byte(source)); err != nil {
		return plugins.Info{}, "", err
	}
	if err = atomicFile(root, filepath.Join(directory, "plugin.json"), append(b, '\n')); err != nil {
		return plugins.Info{}, "", err
	}
	s.revision = revision()
	return p.Info, s.revision, nil
}
func (s *Service) DeletePlugin(id, expected string) error {
	if !plugins.ValidID(id) {
		return errors.New("invalid plugin id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(expected); err != nil {
		return err
	}
	for _, r := range s.cfg.Rules {
		if r.Plugin == id {
			return fmt.Errorf("plugin is still referenced by rule %s", r.ID)
		}
	}
	root, err := os.OpenRoot(s.home)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = root.MkdirAll("trash/plugins", 0700); err != nil {
		return err
	}
	if err = root.Rename(filepath.Join("plugins", id), filepath.Join("trash/plugins", id+"-"+revision())); err != nil {
		return err
	}
	s.revision = revision()
	return nil
}
func atomicFile(root *os.Root, path string, data []byte) error {
	temp := filepath.Join(filepath.Dir(path), ".save-"+revision())
	f, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return root.Rename(temp, path)
}

// Test runs only one rule against supplied fixture data. It never sends a request.
func (s *Service) Test(ctx context.Context, rule Rule, stage string, input Exchange) (Exchange, []Execution, error) {
	if !slices.Contains(plugins.Stages, stage) {
		return input, nil, errors.New("invalid hook stage")
	}
	rule.Enabled = true
	cfg := defaults(Config{Enabled: true, Rules: []Rule{rule}})
	if err := Validate(cfg); err != nil {
		return input, nil, err
	}
	p, _, err := plugins.Load(ctx, s.home, rule.Plugin)
	if err != nil {
		return input, nil, err
	}
	session := (&program{cfg: cfg, rules: []compiledRule{{rule: rule, plugin: p}}, slots: s.testSlots}).session()
	output := session.Apply(ctx, stage, input)
	return output, session.Executions(), nil
}

func (s *Service) Executions() []Execution {
	s.program.logMu.Lock()
	defer s.program.logMu.Unlock()
	return slices.Clone(s.program.recent)
}
