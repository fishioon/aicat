package app

import (
	"context"
	"errors"
	"github.com/fishioon/aicat/internal/agent"
	"github.com/fishioon/aicat/internal/certificate"
	"github.com/fishioon/aicat/internal/channelhost"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/extension"
	"github.com/fishioon/aicat/internal/gateway"
	"github.com/fishioon/aicat/internal/gateway/addons/api"
	capture "github.com/fishioon/aicat/internal/gateway/addons/traffic"
	"github.com/fishioon/aicat/internal/listenaddr"
	"github.com/fishioon/aicat/internal/mcpserver"
	"github.com/fishioon/aicat/internal/provider"
	"github.com/fishioon/aicat/internal/scheduler"
	"github.com/fishioon/aicat/internal/systemproxy"
	"github.com/fishioon/aicat/internal/traffic"
	"github.com/fishioon/aicat/internal/usage"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Options struct {
	Home, Addr string
	Intercept  []string
}
type runtime struct {
	closeLog              func()
	traffic               *traffic.Service
	gateway               *gateway.Gateway
	capture               *capture.Addon
	cancel                context.CancelFunc
	done                  chan struct{}
	err                   error
	home                  string
	interception          *interception
	initialTrafficEnabled bool
	initialRecording      bool
	initialPreferences    config.Preferences
	initialNavigationPins []string
	initialPins           []string
	connection            *connection
	providers             *provider.Store
	usage                 *usage.Store
	extensions            *extension.Host
	messageRunner         *agent.Runner
	schedulerDone         chan struct{}
}

func start(ctx context.Context, o Options) (result *runtime, startErr error) {
	closeLog, err := openRuntimeLog(o.Home)
	if err != nil {
		return nil, err
	}
	defer func() {
		if startErr != nil {
			slog.Error("aicat startup failed", "err", startErr)
			closeLog()
		}
	}()

	cfg, err := config.Load(o.Home)
	if err != nil {
		return nil, err
	}
	if o.Addr != "" {
		cfg.Gateway.Addr = o.Addr
	}
	if o.Intercept != nil {
		cfg.Gateway.InterceptHosts = o.Intercept
	}
	caDir := filepath.Join(o.Home, "ca")
	if err = os.MkdirAll(caDir, 0700); err != nil {
		return nil, err
	}
	if err = os.Chmod(caDir, 0700); err != nil {
		return nil, err
	}
	ca, err := certificate.Open(caDir)
	if err != nil {
		return nil, err
	}
	service, err := traffic.New(cfg.TrafficOptions())
	if err != nil {
		return nil, err
	}
	addon := capture.New(service.Recorder())
	endpoints := http.NewServeMux()
	providers, err := provider.Open(o.Home)
	if err != nil {
		service.Close()
		return nil, err
	}
	calls, err := usage.Open(o.Home)
	if err != nil {
		service.Close()
		return nil, err
	}
	models := provider.NewHandler(providers, service.Recorder(), addon.Recording, calls)
	endpoints.Handle(provider.Path, models)
	endpoints.Handle(usage.Path, usage.Handler(calls, providers.Name))
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	var gw *gateway.Gateway
	if _, err = mcpserver.Token(o.Home); err != nil {
		cancel()
		service.Close()
		calls.Close()
		return nil, err
	}
	messageRunner := agent.NewWithMCP(agent.Options{MCPURL: "http://" + listenaddr.ClientAddress(cfg.Gateway.Addr) + "/mcp", MCPTokenFile: filepath.Join(o.Home, "mcp-token")})
	extensions, err := extension.New(runCtx, extension.Env{Home: o.Home, AgentRunner: messageRunner, ChannelHost: channelhost.New(o.Home), Providers: providers, Forward: models, Recorder: service.Recorder(), Recording: addon.Recording}, builtins)
	if err != nil {
		messageRunner.Close()
		cancel()
		service.Close()
		calls.Close()
		return nil, err
	}
	extensions.Mount(endpoints)
	schedules, err := scheduler.Open(o.Home, extensions.CheckScheduledRoom, extensions.DeliverScheduled)
	if err != nil {
		messageRunner.Close()
		cancel()
		service.Close()
		calls.Close()
		return nil, err
	}
	tools, toolsErr := mcpserver.New(o.Home, service, extensions, schedules)
	if toolsErr != nil {
		slog.Warn("MCP unavailable", "err", toolsErr)
		tools = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "MCP credentials unavailable", http.StatusServiceUnavailable)
		})
	}
	endpoints.Handle("/mcp", tools)
	endpoints.Handle("/", api.Handler(service))
	addon.SetRecording(cfg.ExtensionEnabled("traffic") && cfg.Traffic.Recording)
	policy := newInterception(o.Home, cfg.Gateway.InterceptHosts)
	intercept := func(req *http.Request) bool {
		return ca.Available() && (policy.shouldIntercept(req) || extensions.Intercepts(req))
	}
	gw, err = gateway.New(gateway.Options{Addr: cfg.Gateway.Addr, CA: ca, ShouldIntercept: intercept, BufferedBodyLimit: cfg.Gateway.BufferedBodyLimit, AddressAddons: func(address string) []proxy.Addon {
		// extensions answer the requests they serve before capture sees them
		return append(append([]proxy.Addon{api.New(address, endpoints)}, extensions.Addons()...), addon)
	}})
	if err != nil {
		messageRunner.Close()
		cancel()
		service.Close()
		calls.Close()
		return nil, err
	}
	r := &runtime{closeLog: closeLog, traffic: service, gateway: gw, capture: addon, cancel: cancel, done: make(chan struct{}), home: o.Home, interception: policy, initialTrafficEnabled: cfg.ExtensionEnabled("traffic"), initialRecording: cfg.Traffic.Recording, providers: providers, usage: calls, extensions: extensions, messageRunner: messageRunner}
	for id, section := range cfg.Extensions {
		if known, _ := extensions.Known(id); known && section.Enabled {
			_ = extensions.Set(id, true)
		}
	}
	r.initialPreferences = cfg.UI
	r.initialNavigationPins = cfg.Navigation.Pinned
	r.initialPins = append([]string(nil), cfg.Traffic.PinnedDomains...)
	r.connection = &connection{proxy: systemproxy.New(o.Home), home: o.Home, addr: cfg.Gateway.Addr, authority: ca, disconnect: gw.ResetConnections, desired: cfg.SystemProxy.Enabled}
	go func() {
		r.err = gw.Run(runCtx)
		if e := r.connection.prepareQuit(); e != nil {
			slog.Error("restore system proxy failed", "err", e)
		}
		close(r.done)
	}()
	select {
	case <-gw.Ready():
		r.connection.startup()
		r.schedulerDone = make(chan struct{})
		go func() { defer close(r.schedulerDone); schedules.Run(runCtx) }()
		slog.Info("aicat ready at", "addr", cfg.Gateway.Addr, "ca", filepath.Join(caDir, "mitmproxy-ca-cert.pem"), "interceptHosts", cfg.Gateway.InterceptHosts)
		return r, nil
	case <-r.done:
		cancel()
		service.Close()
		calls.Close()
		return nil, r.err
	case <-ctx.Done():
		cancel()
		<-r.done
		service.Close()
		calls.Close()
		return nil, errors.Join(ctx.Err(), r.err)
	}
}
func (r *runtime) close() error {
	defer r.closeLog()
	restoreErr := r.connection.prepareQuit()
	r.cancel()
	<-r.done
	<-r.schedulerDone
	r.extensions.Close()
	r.messageRunner.Close()
	err := r.err
	// Socket teardown terminates flow callbacks before releasing traffic storage.
	drained := make(chan struct{})
	go func() { r.capture.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(time.Second):
		err = errors.Join(err, errors.New("traffic capture did not drain"))
	}
	return errors.Join(restoreErr, err, r.traffic.Close(), r.usage.Close())
}
func Serve(ctx context.Context, o Options) error {
	r, err := start(ctx, o)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case <-r.done:
	}
	return r.close()
}
