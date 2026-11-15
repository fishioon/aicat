package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"

	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/messages"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

// Host holds the registered extensions and turns them on and off.
type Host struct {
	env       Env
	itemsMu   sync.RWMutex
	packageMu sync.Mutex
	ctx       context.Context
	home      string
	plugins   *channelplugin.Store
	items     []*item
	changed   func()
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
	h := &Host{env: env, ctx: ctx, home: env.Home, plugins: channelplugin.New(env.Home)}
	descriptors, err := h.plugins.Descriptors()
	if err != nil {
		return nil, err
	}
	for _, descriptor := range descriptors {
		if err := h.plugins.RefreshRuntime(descriptor.ID); err != nil {
			return nil, fmt.Errorf("渠道兼容层更新失败：%w", err)
		}
		extensions = append(extensions, packageExtension(descriptor))
	}
	for _, e := range extensions {
		if h.find(e.ID) != nil {
			return nil, fmt.Errorf("扩展 ID 重复：%s", e.ID)
		}
		parts, err := e.Build(env)
		if err != nil {
			return nil, fmt.Errorf("extension %s: %w", e.ID, err)
		}
		if preparer, ok := parts.Messages.(interface{ PrepareRooms() error }); ok {
			if err := preparer.PrepareRooms(); err != nil {
				return nil, fmt.Errorf("extension %s rooms: %w", e.ID, err)
			}
		}
		h.items = append(h.items, &item{Extension: e, parts: parts})
	}
	return h, nil
}

func (h *Host) find(id string) *item {
	for _, it := range h.registered() {
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
	if it == nil {
		return false, false
	}
	if it.PackageFormat == "aicat" {
		m, err := h.plugins.Manifest(id)
		return true, err == nil && len(m.Capabilities.Intercepts) > 0
	}
	return true, it.NeedsTrust
}

// State is a registered extension and whether it is on.
type State struct {
	Extension
	Enabled          bool                    `json:"enabled"`
	Installed        bool                    `json:"installed"`
	Builtin          bool                    `json:"builtin"`
	CanPrepare       bool                    `json:"canPrepare"`
	Ready            bool                    `json:"ready"`
	InstalledVersion string                  `json:"installedVersion"`
	UpdateAvailable  bool                    `json:"updateAvailable"`
	PackagePresent   bool                    `json:"packagePresent"`
	Operation        channelplugin.Operation `json:"operation"`
}

// List is every registered extension, in the order they were registered.
func (h *Host) List() []State {
	out := []State{}
	for _, it := range h.registered() {
		descriptor := it.Extension
		if d, ok := h.plugins.Descriptor(it.ID); ok {
			descriptor.Version = d.Version
			descriptor.Name, descriptor.Summary, descriptor.Lede = d.Name, d.Summary, d.Summary
			descriptor.PackageOrigin = d.Registry
			if d.Source != "" {
				descriptor.PackageOrigin = channelplugin.DisplaySource(d.Source)
				if source, err := channelplugin.ParseSource(d.Source, d.Reference); err == nil {
					descriptor.PackageSource = source.Kind
				}
			}
		}
		if sender, ok := it.parts.Messages.(interface{ CanSend() bool }); ok {
			descriptor.CanSend = sender.CanSend()
		}
		if descriptor.PackageFormat == "aicat" {
			if manifest, err := h.plugins.Manifest(it.ID); err == nil {
				descriptor.AutomaticSetup = manifest.Prepare != ""
				descriptor.SendTargetLabel = manifest.SendTargetLabel
				descriptor.ConfigFields = manifest.Fields
				descriptor.StatusMessages = manifest.StatusMessages
				descriptor.NeedsTrust = len(manifest.Capabilities.Intercepts) > 0
			}
		}
		builtin := it.Package == ""
		installed := builtin || it.parts.Messages != nil && it.parts.Messages.Installed()
		ready := it.parts.Messages == nil || it.parts.Messages.Validated()
		installedVersion := ""
		if current, err := h.plugins.Current(it.ID); err == nil {
			installedVersion = current.Version
		}
		out = append(out, State{CanPrepare: it.parts.Prepare != nil, Operation: h.plugins.Operation(it.ID), Extension: descriptor, Enabled: it.on.Load(), Installed: installed, Builtin: builtin, Ready: ready, InstalledVersion: installedVersion, UpdateAvailable: installed && !builtin && (installedVersion != descriptor.Version || descriptor.PackageSource != "registry"), PackagePresent: !builtin && h.plugins.Present(it.ID)})
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
	return h.set(it, on)
}

func (h *Host) SetEnabled(id string, on bool) error {
	it := h.find(id)
	if it == nil {
		return fmt.Errorf("没有这个扩展：%s", id)
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	if on {
		if err := h.CanEnable(id); err != nil {
			return err
		}
	}
	if on && !it.on.Load() && it.parts.Prepare != nil {
		if err := it.parts.Prepare(h.ctx); err != nil {
			return err
		}
	}
	if err := config.SetExtensionEnabled(h.home, id, on); err != nil {
		return err
	}
	return h.set(it, on)
}

func (h *Host) set(it *item, on bool) error {
	if it.on.Load() == on {
		return nil
	}
	if on {
		if err := h.CanEnable(it.ID); err != nil {
			return err
		}
	}
	if on {
		if preparer, ok := it.parts.Messages.(interface{ PrepareRooms() error }); ok {
			if err := preparer.PrepareRooms(); err != nil {
				return err
			}
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
	for _, it := range h.registered() {
		_ = h.Set(it.ID, false)
	}
}

func (h *Host) MessageStates() map[string]messages.State {
	out := make(map[string]messages.State)
	for _, it := range h.registered() {
		if it.parts.Messages != nil {
			out[it.ID] = it.parts.Messages.Snapshot()
		}
	}
	return out
}

func (h *Host) OnChange(fn func()) {
	h.changed = fn
	h.plugins.OnChange(fn)
	for _, it := range h.registered() {
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

func (h *Host) SendMessage(ctx context.Context, channel string, event messages.Event) (messages.Receipt, error) {
	it := h.find(channel)
	if it == nil || it.parts.Messages == nil || !it.on.Load() {
		return messages.Receipt{}, fmt.Errorf("消息渠道未启用")
	}
	if event.Type == messages.ReplyPublished {
		binding, ok := it.parts.Messages.(interface{ BoundSource() (string, error) })
		if !ok {
			return messages.Receipt{}, fmt.Errorf("渠道缺少 Room 来源绑定")
		}
		source, err := binding.BoundSource()
		if err != nil {
			return messages.Receipt{}, err
		}
		if err = messages.CheckRoomBinding(channel, source, event); err != nil {
			return messages.Receipt{}, err
		}
		receipt, err := messages.PublishReply(ctx, h.home, channel, source, event)
		if err == nil && h.changed != nil {
			h.changed()
		}
		return receipt, err
	}
	capability, ok := it.parts.Messages.(interface{ CanSend() bool })
	if !ok || !capability.CanSend() {
		return messages.Receipt{}, fmt.Errorf("这个渠道不支持外部发送")
	}
	binding, ok := it.parts.Messages.(interface{ BoundSource() (string, error) })
	if !ok {
		return messages.Receipt{}, fmt.Errorf("渠道缺少 Room 来源绑定")
	}
	source, err := binding.BoundSource()
	if err != nil {
		return messages.Receipt{}, err
	}
	if err = messages.CheckRoomBinding(channel, source, event); err != nil {
		return messages.Receipt{}, err
	}
	if err = messages.CheckOutbound(ctx, h.home, channel, source, event.Data.RoomID); err != nil {
		return messages.Receipt{}, err
	}
	receipt, err := messages.Deliver(ctx, h.home, channel, source, event, func(ctx context.Context, _ string, outbound messages.Event) (messages.Receipt, error) {
		if !it.on.Load() {
			return messages.Receipt{}, fmt.Errorf("消息渠道已停用")
		}
		if h.changed != nil {
			h.changed()
		}
		return it.parts.Messages.Send(ctx, outbound)
	})
	if h.changed != nil {
		h.changed()
	}
	return receipt, err
}

// Intercepts reports whether an extension that is on wants a CONNECT target
// decrypted.
func (h *Host) Intercepts(r *http.Request) bool {
	for _, it := range h.registered() {
		if it.on.Load() && it.PackageFormat == "aicat" {
			if channel, ok := it.parts.Messages.(gatewayChannel); ok {
				u, err := url.Parse("https://" + r.Host)
				if err == nil && matchesGatewayHosts(channel.GatewayCapabilities().Intercepts, u) {
					return true
				}
			}
		}
		if it.parts.Intercepts != nil && it.on.Load() && it.parts.Intercepts(r) {
			return true
		}
	}
	return false
}

// Mount serves every extension's routes on mux. While an extension is off its
// routes answer 503 and say so; its handler is not called.
func (h *Host) Mount(mux *http.ServeMux) {
	h.mountRooms(mux)
	for _, it := range h.registered() {
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
	out := []proxy.Addon{&nativeGateway{host: h}}
	for _, it := range h.registered() {
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
func (g gatedAddon) WebSocketMessageReceived(f *proxy.Flow, msg *proxy.WebSocketMessage) {
	if !g.on.Load() {
		return
	}
	if observer, ok := g.a.(proxy.WebSocketMessageObserver); ok {
		observer.WebSocketMessageReceived(f, msg)
	} else {
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
	return h.changePackage(ctx, id, "install")
}

func (h *Host) Update(ctx context.Context, id string) error {
	return h.changePackage(ctx, id, "update")
}

func (h *Host) Uninstall(ctx context.Context, id string) error {
	return h.changePackage(ctx, id, "uninstall")
}

func (h *Host) CancelInstallation(id string) error { return h.plugins.Cancel(id) }

func (h *Host) changePackage(ctx context.Context, id, operation string) error {
	it := h.find(id)
	if it == nil || it.Package == "" {
		return fmt.Errorf("没有可安装的渠道插件：%s", id)
	}
	if !it.mu.TryLock() {
		return fmt.Errorf("扩展正在执行其他操作")
	}
	defer it.mu.Unlock()
	if it.on.Load() {
		return fmt.Errorf("请先停用渠道插件")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := config.SetExtensionEnabled(h.home, id, false); err != nil {
		return err
	}
	operationCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(h.ctx, cancel)
	defer func() { stop(); cancel() }()
	switch operation {
	case "install":
		return h.plugins.Install(operationCtx, id)
	case "update":
		return h.plugins.Update(operationCtx, id)
	case "uninstall":
		return h.plugins.Uninstall(operationCtx, id)
	}
	return fmt.Errorf("未知包操作")
}

func (h *Host) CanEnable(id string) error {
	it := h.find(id)
	if it == nil {
		return fmt.Errorf("没有这个扩展：%s", id)
	}
	if it.parts.Prepare == nil && it.parts.Messages != nil && !it.parts.Messages.Validated() {
		return fmt.Errorf("请先完成消息渠道配置和验证")
	}
	return nil
}
