package app

import (
	"net/http"
	"sort"
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
}

func newInterception(home string, hosts []string) *interception {
	p := &interception{home: home, hosts: make(map[string]bool)}
	for _, host := range hosts {
		p.hosts[config.Host(host)] = true
	}
	return p
}

func (p *interception) enabled(host string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.hosts[config.Host(host)]
}

func (p *interception) shouldIntercept(r *http.Request) bool { return p.enabled(r.Host) }

func (p *interception) enable(host string) error {
	return p.set(host, true)
}

func (p *interception) list() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]string, 0, len(p.hosts))
	for h := range p.hosts {
		result = append(result, h)
	}
	sort.Strings(result)
	return result
}

func (p *interception) set(host string, enabled bool) error {
	p.save.Lock()
	defer p.save.Unlock()
	var err error
	host, err = config.InterceptHost(host)
	if err != nil {
		return err
	}
	if p.enabled(host) == enabled {
		return nil
	}
	if err := config.SetInterceptEnabled(p.home, host, enabled); err != nil {
		return err
	}
	p.mu.Lock()
	if enabled {
		p.hosts[host] = true
	} else {
		delete(p.hosts, host)
	}
	p.mu.Unlock()
	return nil
}
