// Package gateway assembles go-mitmproxy without depending on UI or traffic business code.
package gateway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/fishioon/aicat/internal/listenaddr"
	"github.com/lqqyt2423/go-mitmproxy/cert"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Options struct {
	Addr              string
	CA                cert.CA
	ShouldIntercept   func(*http.Request) bool
	BufferedBodyLimit int64
	Addons            []proxy.Addon
	AddressAddons     func(string) []proxy.Addon
}
type Status struct {
	State string `json:"state"`
	Addr  string `json:"addr"`
	Error string `json:"error,omitempty"`
}
type listener struct {
	p             *proxy.Proxy
	mu            sync.Mutex
	status        Status
	ran, stopping bool
	ready         chan struct{}
	stopped       chan struct{}
	stopOnce      sync.Once
	stopErr       error
	tracker       *connections
	probe         string
}

func newListener(o Options) (*listener, error) {
	if err := listenaddr.Validate(o.Addr); err != nil {
		return nil, err
	}

	if o.CA == nil || o.BufferedBodyLimit <= 0 {
		return nil, fmt.Errorf("gateway: CA and positive body buffer limit required")
	}
	p, err := proxy.NewProxy(&proxy.Options{Addr: o.Addr, StreamLargeBodies: o.BufferedBodyLimit, NewCaFunc: func() (cert.CA, error) { return o.CA, nil }})
	if err != nil {
		return nil, err
	}
	p.SetShouldInterceptRule(func(r *http.Request) bool { return o.ShouldIntercept != nil && o.ShouldIntercept(r) })
	// Outbound traffic must never inherit an environment proxy pointing at us.
	p.SetUpstreamProxy(func(*http.Request) (*url.URL, error) { return nil, nil })
	tracker := &connections{conns: make(map[*proxy.ClientConn]struct{})}
	token := make([]byte, 24)
	if _, err = rand.Read(token); err != nil {
		return nil, err
	}
	probe := fmt.Sprintf("/__aicat_ready_%x", token)
	p.AddAddon(tracker)
	p.AddAddon(&readiness{path: probe})
	addons := append([]proxy.Addon(nil), o.Addons...)
	if o.AddressAddons != nil {
		addons = append(addons, o.AddressAddons(o.Addr)...)
	}
	for _, a := range addons {
		if a == nil {
			return nil, fmt.Errorf("gateway: nil addon")
		}
		p.AddAddon(a)
	}
	return &listener{p: p, status: Status{State: "new", Addr: o.Addr}, ready: make(chan struct{}), stopped: make(chan struct{}), tracker: tracker, probe: probe}, nil
}
func (g *listener) Ready() <-chan struct{} { return g.ready }

// ResetConnections terminates sessions that may retain certificates from a
// retired CA. The listener stays live and new clients use the current policy.
func (g *listener) ResetConnections() { g.tracker.closeAll() }
func (g *listener) Status() Status    { g.mu.Lock(); defer g.mu.Unlock(); return g.status }
func (g *listener) Run(ctx context.Context) error {
	g.mu.Lock()
	if g.ran || g.stopping {
		g.mu.Unlock()
		return fmt.Errorf("gateway: instance can run only once")
	}
	g.ran = true
	g.status.State = "starting"
	g.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- g.p.Start() }()
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 150 * time.Millisecond}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	ready := false
	for {
		select {
		case err := <-done:
			g.mu.Lock()
			stopping := g.stopping
			g.mu.Unlock()
			if stopping {
				<-g.stopped
				return g.stopErr
			}
			if errors.Is(err, http.ErrServerClosed) {
				err = fmt.Errorf("gateway: unexpected listener closure")
			}
			g.mu.Lock()
			g.status.State = "failed"
			if err != nil {
				g.status.Error = err.Error()
			}
			g.mu.Unlock()
			_ = g.p.Close()
			g.tracker.closeAll()
			return err
		case <-ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := g.Shutdown(stopCtx)
			cancel()
			<-done
			return err
		case <-ticker.C:
			if ready {
				continue
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, (&url.URL{Scheme: "http", Host: listenaddr.ClientAddress(g.status.Addr), Path: g.probe}).String(), nil)
			res, err := client.Do(req)
			if err != nil {
				continue
			}
			_, _ = io.Copy(io.Discard, res.Body)
			res.Body.Close()
			if res.StatusCode == 204 && res.Header.Get("X-Aicat-Instance") == g.probe {
				g.mu.Lock()
				if !g.stopping {
					g.status.State = "ready"
					close(g.ready)
					ready = true
				}
				g.mu.Unlock()
			}
		}
	}
}
func (g *listener) Shutdown(ctx context.Context) error {
	g.stopOnce.Do(func() {
		g.mu.Lock()
		g.stopping = true
		g.status.State = "stopping"
		g.mu.Unlock()
		err := g.p.Shutdown(ctx)
		// Shutdown has drained ordinary HTTP handlers. Hijacked tunnels have
		// no finite request boundary, so close them explicitly rather than
		// waiting until every normal desktop quit exhausts its deadline.
		g.tracker.closeAll()
		err = errors.Join(err, g.p.Close())
		g.mu.Lock()
		g.stopErr = err
		g.status.State = "stopped"
		if err != nil {
			g.status.Error = err.Error()
		}
		g.mu.Unlock()
		close(g.stopped)
	})
	select {
	case <-g.stopped:
		return g.stopErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

type readiness struct {
	proxy.BaseAddon
	path string
}

func (a *readiness) AccessProxyServer(r *http.Request, w http.ResponseWriter) {
	if r.URL.Path == a.path {
		w.Header().Set("X-Aicat-Instance", a.path)
		w.WriteHeader(204)
	}
}

type connections struct {
	proxy.BaseAddon
	mu    sync.Mutex
	conns map[*proxy.ClientConn]struct{}
}

func (c *connections) ClientConnected(conn *proxy.ClientConn) {
	c.mu.Lock()
	c.conns[conn] = struct{}{}
	c.mu.Unlock()
}
func (c *connections) ClientDisconnected(conn *proxy.ClientConn) {
	c.mu.Lock()
	delete(c.conns, conn)
	c.mu.Unlock()
}
func (c *connections) closeAll() {
	c.mu.Lock()
	conns := make([]*proxy.ClientConn, 0, len(c.conns))
	for conn := range c.conns {
		conns = append(conns, conn)
	}
	c.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Conn.Close()
	}
}
