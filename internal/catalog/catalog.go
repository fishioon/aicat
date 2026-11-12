// Package catalog keeps a local copy of the models.dev catalog and answers
// what it says about a model: its name, context window, longest reply,
// reasoning levels and whether it takes images.
//
// A provider's own model list is the truth about what it serves; the catalog
// only fills in what that list leaves out. Models are matched by id without a
// vendor's prefix, and where the providers models.dev lists disagree, the
// answer most of them give wins. Matching and voting follow Magpie (MIT,
// github.com/yetone/magpie internal/catalog/catalog.go).
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// URL is where the catalog is downloaded from.
const URL = "https://models.dev/api.json"

// staleAfter is how old the local copy may be before it is downloaded again.
const staleAfter = 24 * time.Hour

// Info is what the catalog says about one model; zero values are unknown.
type Info struct {
	Name    string
	Context int
	Output  int
	Efforts []string
	// Images is whether most providers listing the model say it takes images.
	Images bool
}

type Catalog struct {
	path, url string
	client    *http.Client
	syncMu    sync.Mutex
	mu        sync.RWMutex
	loaded    bool
	byID      map[string]Info
	onChange  func()
}

// Open uses the copy kept at path, downloaded from url when stale.
func Open(path, url string) *Catalog {
	transport := &http.Transport{
		// never through an environment proxy that may be aicat itself
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	return &Catalog{path: path, url: url, client: &http.Client{Transport: transport, Timeout: time.Minute}}
}

// OnChange is told after a download replaced the catalog.
func (c *Catalog) OnChange(fn func()) {
	c.mu.Lock()
	c.onChange = fn
	c.mu.Unlock()
}

// Lookup is what the catalog says about a model of this id: by its id without
// a vendor's prefix and case, then without a "(variant)" or ":tag".
func (c *Catalog) Lookup(id string) (Info, bool) {
	byID := c.load()
	b := bareID(id)
	for _, want := range []string{b, cutAt(b, '('), cutAt(b, ':')} {
		if info, ok := byID[want]; ok {
			info.Efforts = slices.Clone(info.Efforts)
			return info, true
		}
	}
	return Info{}, false
}

// Stale reports whether there is no local copy or it is older than a day.
func (c *Catalog) Stale() bool {
	st, err := os.Stat(c.path)
	return err != nil || time.Since(st.ModTime()) > staleAfter
}

// Refresh downloads the catalog in the background when it is stale.
func (c *Catalog) Refresh() {
	if !c.Stale() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := c.Sync(ctx); err != nil {
			log.Printf("models.dev: %v", err)
		}
	}()
}

// KeepFresh looks every hour whether the catalog is wanted and stale, and
// downloads it then, until ctx ends.
func (c *Catalog) KeepFresh(ctx context.Context, wanted func() bool) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if wanted() {
				c.Refresh()
			}
		}
	}
}

// Sync downloads the catalog and replaces the local copy. A download that is
// not a catalog leaves the copy as it was.
func (c *Catalog) Sync(ctx context.Context) error {
	if !c.syncMu.TryLock() {
		return nil // one is under way
	}
	defer c.syncMu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "aicat")
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return err
	}
	byID, err := parse(b)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.path), ".models-dev-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), c.path); err != nil {
		return err
	}
	c.mu.Lock()
	c.byID, c.loaded = byID, true
	fn := c.onChange
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
	return nil
}

func (c *Catalog) load() map[string]Info {
	c.mu.RLock()
	if c.loaded {
		defer c.mu.RUnlock()
		return c.byID
	}
	c.mu.RUnlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		c.loaded = true
		if b, err := os.ReadFile(c.path); err == nil {
			if byID, err := parse(b); err == nil {
				c.byID = byID
			} else {
				log.Printf("models.dev: %s: %v", c.path, err)
			}
		}
	}
	return c.byID
}

type mdProvider struct {
	Models map[string]mdModel `json:"models"`
}

type mdModel struct {
	Name      string `json:"name"`
	Reasoning []struct {
		Type   string   `json:"type"`
		Values []string `json:"values"`
	} `json:"reasoning_options"`
	Modalities struct {
		Input []string `json:"input"`
	} `json:"modalities"`
	Limit struct {
		Context int `json:"context"`
		Input   int `json:"input"`
		Output  int `json:"output"`
	} `json:"limit"`
}

// parse reduces the catalog to one answer per bare model id: for each field,
// the value most providers listing the model give.
func parse(b []byte) (map[string]Info, error) {
	var all map[string]mdProvider
	if err := json.Unmarshal(b, &all); err != nil || len(all) == 0 {
		return nil, errors.New("返回的不是 models.dev 目录")
	}
	names := map[string]map[string]int{}
	windows := map[string]map[int]int{}
	outputs := map[string]map[int]int{}
	levels := map[string]map[string]int{}
	images := map[string]int{}
	seen := map[string]bool{}
	vote := func(m map[string]map[string]int, id, v string) {
		if m[id] == nil {
			m[id] = map[string]int{}
		}
		m[id][v]++
	}
	voteN := func(m map[string]map[int]int, id string, v int) {
		if m[id] == nil {
			m[id] = map[int]int{}
		}
		m[id][v]++
	}
	// one vote a provider: a gateway listing a model under each host it
	// routes to votes once
	once := map[string]bool{}
	for pid, p := range all {
		for key, m := range p.Models {
			id := bareID(key)
			if once[pid+"\x00"+id] {
				continue
			}
			once[pid+"\x00"+id] = true
			seen[id] = true
			if m.Name != "" {
				vote(names, id, m.Name)
			}
			for _, r := range m.Reasoning {
				if r.Type == "effort" && len(r.Values) > 0 {
					vote(levels, id, strings.Join(r.Values, ","))
				}
			}
			window := m.Limit.Input
			if window == 0 {
				window = m.Limit.Context
			}
			if window > 0 {
				voteN(windows, id, window)
			}
			if m.Limit.Output > 0 {
				voteN(outputs, id, m.Limit.Output)
			}
			if slices.Contains(m.Modalities.Input, "image") {
				images[id]++
			} else {
				images[id]--
			}
		}
	}
	byID := make(map[string]Info, len(seen))
	for id := range seen {
		info := Info{Name: mostListed(names[id]), Context: mostGiven(windows[id]), Output: mostGiven(outputs[id]), Images: images[id] > 0}
		if l := mostListed(levels[id]); l != "" {
			info.Efforts = strings.Split(l, ",")
		}
		byID[id] = info
	}
	return byID, nil
}

// mostListed is the value most providers give; a tie goes to the shorter
// list, then the first in order, so the answer doesn't change run to run.
func mostListed(by map[string]int) string {
	best, n := "", 0
	for v, c := range by {
		if c > n || c == n && (strings.Count(v, ",") < strings.Count(best, ",") || strings.Count(v, ",") == strings.Count(best, ",") && v < best) {
			best, n = v, c
		}
	}
	return best
}

// mostGiven is the size most providers give; a tie goes to the smaller,
// which a prompt fits either way.
func mostGiven(by map[int]int) int {
	best, n := 0, 0
	for v, c := range by {
		if c > n || c == n && v < best {
			best, n = v, c
		}
	}
	return best
}

// bareID is a model id without a vendor's prefix, in lower case.
func bareID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	return id
}

// cutAt is s before sep: "gpt-5(high)" is gpt-5, "glm-5:free" is glm-5.
func cutAt(s string, sep byte) string {
	if i := strings.IndexByte(s, sep); i > 0 {
		return s[:i]
	}
	return s
}
