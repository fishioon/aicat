package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/messages"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

// Host holds the registered extensions and turns them on and off.
type Host struct {
	ctx     context.Context
	plugins *channelplugin.Store
	items   []*item
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
	h := &Host{ctx: ctx, plugins: channelplugin.New(env.Home)}
	for _, descriptor := range channelplugin.Catalog {
		if err := h.plugins.RefreshRuntime(descriptor.ID); err != nil {
			return nil, fmt.Errorf("渠道兼容层更新失败：%w", err)
		}
		extensions = append(extensions, Extension{ID: descriptor.ID, Name: descriptor.Name, Summary: descriptor.Summary, Lede: descriptor.Summary, Categories: []string{"消息"}, Package: descriptor.Package, Version: descriptor.Version, Publisher: descriptor.Publisher,
			Build: func(env Env) (Parts, error) {
				channel := messages.New(env.Home, descriptor.ID, platformEndpoint(), env.ChannelHost)
				return Parts{Messages: channel, Run: channel.Run}, nil
			}})
	}
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
	Enabled   bool `json:"enabled"`
	Installed bool `json:"installed"`
	Builtin   bool `json:"builtin"`
	Ready     bool `json:"ready"`
}

// List is every registered extension, in the order they were registered.
func (h *Host) List() []State {
	out := make([]State, 0, len(h.items))
	for _, it := range h.items {
		builtin := it.Package == ""
		installed := builtin || it.parts.Messages != nil && it.parts.Messages.Installed()
		ready := it.parts.Messages == nil || it.parts.Messages.Validated()
		out = append(out, State{Extension: it.Extension, Enabled: it.on.Load(), Builtin: builtin, Installed: installed, Ready: ready})
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
	if on {
		if err := h.CanEnable(id); err != nil {
			return err
		}
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

func (h *Host) MessageStates() map[string]messages.State {
	out := make(map[string]messages.State)
	for _, it := range h.items {
		if it.parts.Messages != nil {
			out[it.ID] = it.parts.Messages.Snapshot()
		}
	}
	return out
}

func (h *Host) OnChange(fn func()) {
	for _, it := range h.items {
		if it.parts.Messages != nil {
			it.parts.Messages.OnChange(fn)
		}
	}
}

func (h *Host) ConfigureMessageChannel(ctx context.Context, id string, account messages.AccountConfig) error {
	it := h.find(id)
	if it == nil || it.parts.Messages == nil {
		return fmt.Errorf("没有这个消息渠道：%s", id)
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.on.Load() {
		return fmt.Errorf("请先停用扩展再修改账号")
	}
	if !it.parts.Messages.Installed() {
		return fmt.Errorf("请先安装渠道插件")
	}
	if err := it.parts.Messages.Configure(account); err != nil {
		return err
	}
	return it.parts.Messages.Validate(ctx)
}

func (h *Host) ConfigureMessageAgent(id string, settings config.MessageAgent) error {
	it := h.find(id)
	if it == nil || it.parts.Messages == nil {
		return fmt.Errorf("没有这个消息渠道：%s", id)
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.on.Load() {
		return fmt.Errorf("请先停用扩展再修改 Codex 配置")
	}
	return it.parts.Messages.ConfigureAgent(settings)
}

func (h *Host) SendMessage(ctx context.Context, channel string, event messages.Event) (messages.Receipt, error) {
	it := h.find(channel)
	if it == nil || it.parts.Messages == nil || !it.on.Load() {
		return messages.Receipt{}, fmt.Errorf("消息渠道未启用")
	}
	return it.parts.Messages.Send(ctx, event)
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

func (h *Host) Install(ctx context.Context, id string) error {
	it := h.find(id)
	if it == nil || it.Package == "" {
		return fmt.Errorf("没有可安装的渠道插件：%s", id)
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.on.Load() {
		return fmt.Errorf("请先停用渠道插件")
	}
	installCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(h.ctx, cancel)
	defer func() { stop(); cancel() }()
	return h.plugins.Install(installCtx, id)
}
func (h *Host) CanEnable(id string) error {
	it := h.find(id)
	if it == nil {
		return fmt.Errorf("没有这个扩展：%s", id)
	}
	if it.parts.Messages != nil && !it.parts.Messages.Validated() {
		return fmt.Errorf("请先完成消息渠道配置和验证")
	}
	return nil
}
