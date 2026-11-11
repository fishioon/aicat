package app

import (
	"context"
	"errors"
	"github.com/fishioon/aicat/internal/certificate"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/gateway"
	"github.com/fishioon/aicat/internal/gateway/addons/api"
	capture "github.com/fishioon/aicat/internal/gateway/addons/traffic"
	"github.com/fishioon/aicat/internal/mcpserver"
	"github.com/fishioon/aicat/internal/provider"
	"github.com/fishioon/aicat/internal/rewrite"
	"github.com/fishioon/aicat/internal/systemproxy"
	"github.com/fishioon/aicat/internal/traffic"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
	"log"
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
	traffic               *traffic.Service
	gateway               *gateway.Gateway
	capture               *capture.Addon
	cancel                context.CancelFunc
	done                  chan struct{}
	err                   error
	home                  string
	interception          *interception
	initialTrafficEnabled bool
	initialPreferences    config.Preferences
	initialNavigationPins []string
	initialPins           []string
	connection            *connection
	rules                 *rewrite.Service
	providers             *provider.Store
}

func start(ctx context.Context, o Options) (*runtime, error) {
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
	rules := rewrite.New(ctx, o.Home, cfg.Rewrite, func(next rewrite.Config) error { return config.SetRewrite(o.Home, next) })
	addon := capture.New(service.Recorder(), rules)
	endpoints := http.NewServeMux()
	tools, toolsErr := mcpserver.New(o.Home, rules, service)
	if toolsErr != nil {
		log.Printf("MCP unavailable: %v", toolsErr)
		tools = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "MCP credentials unavailable", http.StatusServiceUnavailable)
		})
	}
	endpoints.Handle("/mcp", tools)
	providers, err := provider.Open(o.Home)
	if err != nil {
		service.Close()
		return nil, err
	}
	models := provider.NewHandler(providers, service.Recorder(), addon.Recording)
	for _, mount := range provider.Mounts() {
		endpoints.Handle(mount, models)
		endpoints.Handle(mount+"/", models)
	}
	endpoints.Handle("/", api.Handler(service))
	addon.SetRecording(cfg.Features.Traffic)
	policy := newInterception(o.Home, cfg.Gateway.InterceptHosts)
	gw, err := gateway.New(gateway.Options{Addr: cfg.Gateway.Addr, CA: ca, ShouldIntercept: func(req *http.Request) bool { return ca.Available() && policy.shouldIntercept(req) }, BufferedBodyLimit: cfg.Gateway.BufferedBodyLimit, AddressAddons: func(address string) []proxy.Addon { return []proxy.Addon{api.New(address, endpoints), addon} }})
	if err != nil {
		service.Close()
		return nil, err
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r := &runtime{traffic: service, gateway: gw, capture: addon, cancel: cancel, done: make(chan struct{}), home: o.Home, interception: policy, initialTrafficEnabled: cfg.Features.Traffic, rules: rules, providers: providers}
	r.initialPreferences = cfg.UI
	r.initialNavigationPins = cfg.Navigation.Pinned
	r.initialPins = append([]string(nil), cfg.Traffic.PinnedDomains...)
	r.connection = &connection{proxy: systemproxy.New(o.Home), home: o.Home, addr: cfg.Gateway.Addr, authority: ca, disconnect: gw.ResetConnections, desired: cfg.SystemProxy.Enabled}
	go func() {
		r.err = gw.Run(runCtx)
		if e := r.connection.prepareQuit(); e != nil {
			log.Printf("restore system proxy: %v", e)
		}
		close(r.done)
	}()
	select {
	case <-gw.Ready():
		r.connection.startup(ctx)
		log.Printf("aicat ready at %s; CA %s; HTTPS interception: %v", cfg.Gateway.Addr, filepath.Join(caDir, "mitmproxy-ca-cert.pem"), cfg.Gateway.InterceptHosts)
		return r, nil
	case <-r.done:
		cancel()
		service.Close()
		return nil, r.err
	case <-ctx.Done():
		cancel()
		<-r.done
		service.Close()
		return nil, errors.Join(ctx.Err(), r.err)
	}
}
func (r *runtime) close() error {
	restoreErr := r.connection.prepareQuit()
	r.cancel()
	<-r.done
	err := r.err
	// Socket teardown terminates flow callbacks before releasing traffic storage.
	drained := make(chan struct{})
	go func() { r.capture.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(time.Second):
		err = errors.Join(err, errors.New("traffic capture did not drain"))
	}
	return errors.Join(restoreErr, err, r.traffic.Close())
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
