package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

// Host holds the registered extensions and turns them on and off.
type Host struct {
	ctx   context.Context
	items []*item
}

type item struct {
	Extension
	parts  Parts
	on     atomic.Bool
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// New builds every extension; none is on yet. ctx bounds every extension's
// background work.
func New(ctx context.Context, env Env, extensions []Extension) (*Host, error) {
	h := &Host{ctx: ctx}
	for _, e := range extensions {
		parts, err := e.Build(env)
		if err != nil {
			return nil, fmt.Errorf("extension %s: %w", e.ID, err)
		}
		h.items = append(h.items, &item{Extension: e, parts: parts})
	}
	return h, nil
}

func (h *Host) find(id string) *item {
	for _, it := range h.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

// Known reports whether an extension of this ID is registered, and whether it
// needs the root certificate trusted.
func (h *Host) Known(id string) (known, needsTrust bool) {
	it := h.find(id)
	return it != nil, it != nil && it.NeedsTrust
}

// State is a registered extension and whether it is on.
type State struct {
	Extension
	Enabled bool `json:"enabled"`
}

// List is every registered extension, in the order they were registered.
func (h *Host) List() []State {
	out := make([]State, 0, len(h.items))
	for _, it := range h.items {
		out = append(out, State{Extension: it.Extension, Enabled: it.on.Load()})
	}
	return out
}

// Set turns an extension on or off. Turning one off stops its background work
// and waits for it.
func (h *Host) Set(id string, on bool) error {
	it := h.find(id)
	if it == nil {
		return fmt.Errorf("没有这个扩展：%s", id)
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.on.Load() == on {
		return nil
	}
	it.on.Store(on)
	if on {
		if it.parts.Run != nil {
			ctx, cancel := context.WithCancel(h.ctx)
			done := make(chan struct{})
			it.cancel, it.done = cancel, done
			go func() { defer close(done); it.parts.Run(ctx) }()
		}
		return nil
	}
	if it.cancel != nil {
		it.cancel()
		<-it.done
		it.cancel, it.done = nil, nil
	}
	return nil
}

// Close turns every extension off.
func (h *Host) Close() {
	for _, it := range h.items {
		_ = h.Set(it.ID, false)
	}
}

// Intercepts reports whether an extension that is on wants a CONNECT target
// decrypted.
func (h *Host) Intercepts(r *http.Request) bool {
	for _, it := range h.items {
		if it.parts.Intercepts != nil && it.on.Load() && it.parts.Intercepts(r) {
			return true
		}
	}
	return false
}

// Mount serves every extension's routes on mux. While an extension is off its
// routes answer 503 and say so; its handler is not called.
func (h *Host) Mount(mux *http.ServeMux) {
	for _, it := range h.items {
		for pattern, handler := range it.parts.Routes {
			mux.Handle(pattern, gatedHandler{it, handler})
		}
	}
}

type gatedHandler struct {
	it   *item
	next http.Handler
}

func (g gatedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !g.it.on.Load() {
		b, _ := json.Marshal(map[string]any{"error": map[string]string{"type": "aicat_error",
			"message": "aicat 的 " + g.it.ID + " 扩展未启用；在 aicat 的扩展里启用后再试"}})
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write(b)
		return
	}
	g.next.ServeHTTP(w, r)
}

// Addons are the extensions' gateway addons, each called only while its
// extension is on.
func (h *Host) Addons() []proxy.Addon {
	var out []proxy.Addon
	for _, it := range h.items {
		if it.parts.Addon != nil {
			out = append(out, gatedAddon{it.parts.Addon, &it.on})
		}
	}
	return out
}

type gatedAddon struct {
	a  proxy.Addon
	on *atomic.Bool
}

func (g gatedAddon) ClientConnected(c *proxy.ClientConn) {
	if g.on.Load() {
		g.a.ClientConnected(c)
	}
}
func (g gatedAddon) ClientDisconnected(c *proxy.ClientConn) {
	if g.on.Load() {
		g.a.ClientDisconnected(c)
	}
}
func (g gatedAddon) ServerConnected(c *proxy.ConnContext) {
	if g.on.Load() {
		g.a.ServerConnected(c)
	}
}
func (g gatedAddon) ServerDisconnected(c *proxy.ConnContext) {
	if g.on.Load() {
		g.a.ServerDisconnected(c)
	}
}
func (g gatedAddon) TlsEstablishedServer(c *proxy.ConnContext) {
	if g.on.Load() {
		g.a.TlsEstablishedServer(c)
	}
}
func (g gatedAddon) Requestheaders(f *proxy.Flow) {
	if g.on.Load() {
		g.a.Requestheaders(f)
	}
}
func (g gatedAddon) Request(f *proxy.Flow) {
	if g.on.Load() {
		g.a.Request(f)
	}
}
func (g gatedAddon) Responseheaders(f *proxy.Flow) {
	if g.on.Load() {
		g.a.Responseheaders(f)
	}
}
func (g gatedAddon) Response(f *proxy.Flow) {
	if g.on.Load() {
		g.a.Response(f)
	}
}
func (g gatedAddon) StreamRequestModifier(f *proxy.Flow, in io.Reader) io.Reader {
	if g.on.Load() {
		return g.a.StreamRequestModifier(f, in)
	}
	return in
}
func (g gatedAddon) StreamResponseModifier(f *proxy.Flow, in io.Reader) io.Reader {
	if g.on.Load() {
		return g.a.StreamResponseModifier(f, in)
	}
	return in
}
func (g gatedAddon) AccessProxyServer(r *http.Request, w http.ResponseWriter) {
	if g.on.Load() {
		g.a.AccessProxyServer(r, w)
	}
}
func (g gatedAddon) WebSocketStart(f *proxy.Flow) {
	if g.on.Load() {
		g.a.WebSocketStart(f)
	}
}
func (g gatedAddon) WebSocketMessage(f *proxy.Flow) {
	if g.on.Load() {
		g.a.WebSocketMessage(f)
	}
}
func (g gatedAddon) WebSocketEnd(f *proxy.Flow) {
	if g.on.Load() {
		g.a.WebSocketEnd(f)
	}
}
func (g gatedAddon) SSEStart(f *proxy.Flow) {
	if g.on.Load() {
		g.a.SSEStart(f)
	}
}
func (g gatedAddon) SSEMessage(f *proxy.Flow) {
	if g.on.Load() {
		g.a.SSEMessage(f)
	}
}
func (g gatedAddon) SSEEnd(f *proxy.Flow) {
	if g.on.Load() {
		g.a.SSEEnd(f)
	}
}
func (g gatedAddon) RequestError(f *proxy.Flow, err error) {
	if g.on.Load() {
		g.a.RequestError(f, err)
	}
}
func (g gatedAddon) HTTPConnectError(f *proxy.Flow, err error) {
	if g.on.Load() {
		g.a.HTTPConnectError(f, err)
	}
}
