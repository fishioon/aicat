package provider

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/fishioon/aicat/internal/traffic"
)

// credentialHeaders are dropped from what an agent sends, so a placeholder or
// a stale key of the agent's own never reaches the vendor, and masked in the
// traffic record.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Api-Key", "Proxy-Authorization"}

const masked = "[aicat]"

// Handler forwards origin-form requests under a provider's mount to its
// endpoint with the saved key. The caller restricts it to local peers.
type Handler struct {
	store     *Store
	recorder  *traffic.Recorder
	recording func() bool
	transport http.RoundTripper
}

// direct dials vendors itself: outbound requests must never inherit an
// environment proxy pointing at aicat.
var direct = &http.Transport{
	Proxy:               nil,
	DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ForceAttemptHTTP2:   true,
	TLSHandshakeTimeout: 15 * time.Second,
	IdleConnTimeout:     90 * time.Second,
}

func NewHandler(store *Store, recorder *traffic.Recorder, recording func() bool) *Handler {
	return &Handler{store: store, recorder: recorder, recording: recording, transport: direct}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e, rest, saved := h.store.route(r.URL.Path)
	if !saved {
		http.Error(w, "aicat: no provider is connected at "+r.URL.Path+"; add it in the aicat window", http.StatusServiceUnavailable)
		return
	}
	k := protocols[e.Protocol]
	target, err := url.Parse(e.BaseURL + rest)
	if err != nil {
		http.Error(w, "aicat: invalid provider endpoint", http.StatusBadGateway)
		return
	}
	target.RawQuery = r.URL.RawQuery
	rec := h.begin(r, target)
	r.Body = rec.RequestBody(r.Body)
	(&httputil.ReverseProxy{
		Transport:     h.transport,
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = target
			pr.Out.Host = target.Host
			for _, name := range credentialHeaders {
				pr.Out.Header.Del(name)
			}
			// Let the transport negotiate and undo compression, so captured
			// bodies stay readable.
			pr.Out.Header.Del("Accept-Encoding")
			pr.Out.Header.Set(k.header, k.prefix+e.Key)
		},
		ModifyResponse: func(res *http.Response) error {
			res.Body = rec.Response(res)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			state := traffic.Failed
			if errors.Is(err, context.Canceled) {
				state = traffic.Canceled
			}
			rec.Finish(state, err.Error())
			http.Error(w, "aicat: "+err.Error(), http.StatusBadGateway)
		},
	}).ServeHTTP(w, r)
}

func (h *Handler) begin(r *http.Request, target *url.URL) *traffic.Mirror {
	if h.recorder == nil || h.recording != nil && !h.recording() {
		return nil
	}
	header := r.Header.Clone()
	for _, name := range credentialHeaders {
		if header.Get(name) != "" {
			header.Set(name, masked)
		}
	}
	return h.recorder.Mirror(traffic.Request{Method: r.Method, URL: target.String(), Proto: r.Proto, Header: header})
}
