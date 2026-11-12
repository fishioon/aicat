// Package extension is how aicat hosts its built-in extensions. An extension
// hands the host what it adds to aicat, and the host uses those parts only
// while the extension is on: an extension that is off touches no traffic,
// serves no route and runs nothing in the background.
package extension

import (
	"context"
	"net/http"

	"github.com/fishioon/aicat/internal/provider"
	"github.com/fishioon/aicat/internal/traffic"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

// Extension is a built-in extension as it is registered: what the extensions
// page shows of it, what it needs, and how it is built.
type Extension struct {
	// ID names its section in aicat.yaml (extensions.<ID>). A built-in's ID
	// has no dot; a community package's is publisher.name.
	ID string `json:"id"`
	// Name, Summary, Lede and Categories are shown on the extensions page,
	// in the source language; the page translates them.
	Name       string   `json:"name"`
	Summary    string   `json:"summary"`
	Lede       string   `json:"lede"`
	Categories []string `json:"categories"`
	// NeedsTrust: the extension decrypts HTTPS, so it is turned on only once
	// the root certificate is trusted.
	NeedsTrust bool `json:"needsTrust"`
	// Restart says what must be restarted for turning it on or off to take
	// full effect, if anything.
	Restart string `json:"restart,omitempty"`
	// Build makes the extension's parts once, at startup, whether it is on
	// or not; it must not start anything.
	Build func(Env) (Parts, error) `json:"-"`
}

// Env is the core services an extension may use.
type Env struct {
	Home      string
	Providers *provider.Store
	// Forward is the model endpoint: a request sent through it goes to the
	// provider its model names, with the provider's key.
	Forward   http.Handler
	Recorder  *traffic.Recorder
	Recording func() bool
}

// Parts is what an extension adds to aicat. Every part is optional.
type Parts struct {
	// Addon runs in the gateway, before traffic capture.
	Addon proxy.Addon
	// Intercepts names CONNECT targets to decrypt beyond the user's list.
	Intercepts func(*http.Request) bool
	// Routes are path patterns served on aicat's listener, local peers
	// only.
	Routes map[string]http.Handler
	// Run is the extension's background work. It starts when the extension
	// is turned on and its ctx ends when it is turned off.
	Run func(ctx context.Context)
}
