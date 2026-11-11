package app

import (
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/fishioon/aicat/internal/config"
)

// Writes are serialized independently of the lookup lock. Saving config must
// not hold up the proxy's CONNECT decisions while waiting for filesystem I/O.
type interception struct {
	mu    sync.RWMutex
	save  sync.Mutex
	home  string
	hosts map[string]bool
	// suffixes holds wildcard entries keyed by their dotted suffix:
	// *.example.com is stored as ".example.com".
	suffixes map[string]bool
}

func newInterception(home string, hosts []string) *interception {
	p := &interception{home: home, hosts: make(map[string]bool), suffixes: make(map[string]bool)}
	for _, host := range hosts {
		p.put(config.Host(host), true)
	}
	return p
}

// put requires the write lock, or exclusive ownership during construction.
func (p *interception) put(entry string, enabled bool) {
	entries, key := p.hosts, entry
	if strings.HasPrefix(entry, "*.") {
		entries, key = p.suffixes, entry[1:]
	}
	if enabled {
		entries[key] = true
	} else {
		delete(entries, key)
	}
}

// enabled reports whether a CONNECT authority is covered by an exact entry or
// by a wildcard. A wildcard covers subdomains at any depth, never its own base.
func (p *interception) enabled(host string) bool {
	host = config.Host(host)
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.hosts[host] {
		return true
	}
	if len(p.suffixes) == 0 || net.ParseIP(host) != nil {
		return false
	}
	// Substrings share the host's memory, so each label costs one map probe.
	for i := strings.IndexByte(host, '.'); i > 0; {
		if p.suffixes[host[i:]] {
			return true
		}
		next := strings.IndexByte(host[i+1:], '.')
		if next < 0 {
			break
		}
		i += next + 1
	}
	return false
}

func (p *interception) shouldIntercept(r *http.Request) bool { return p.enabled(r.Host) }

func (p *interception) enable(host string) error {
	return p.set(host, true)
}

func (p *interception) list() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]string, 0, len(p.hosts)+len(p.suffixes))
	for h := range p.hosts {
		result = append(result, h)
	}
	for suffix := range p.suffixes {
		result = append(result, "*"+suffix)
	}
	sort.Strings(result)
	return result
}

// listed reports whether the entry itself is in the list, regardless of
// whether another entry already covers it.
func (p *interception) listed(entry string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if strings.HasPrefix(entry, "*.") {
		return p.suffixes[entry[1:]]
	}
	return p.hosts[entry]
}

func (p *interception) set(host string, enabled bool) error {
	p.save.Lock()
	defer p.save.Unlock()
	var err error
	host, err = config.InterceptHost(host)
	if err != nil {
		return err
	}
	if p.listed(host) == enabled {
		return nil
	}
	if err := config.SetInterceptEnabled(p.home, host, enabled); err != nil {
		return err
	}
	p.mu.Lock()
	p.put(host, enabled)
	p.mu.Unlock()
	return nil
}
