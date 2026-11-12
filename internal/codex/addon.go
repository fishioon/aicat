package codex

// The gateway's door for Codex: while the extension is on, Codex's HTTPS to
// its backend is decrypted, and the requests the handler serves (the model
// list, a turn for a provider's model, a WebSocket it turns down) are answered
// here. Every other request to the backend, ChatGPT's own turns among them,
// goes on through the gateway as it came, captured and rewritten like any
// other.

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

type door struct {
	proxy.BaseAddon
	h         *Handler
	authority string
}

func newDoor(h *Handler) *door {
	return &door{h: h, authority: authority(h.backend.Host, h.backend.Scheme)}
}

// intercepts reports whether a CONNECT target is Codex's backend. It adds to
// the user's decryption list without being in it.
func (a *door) intercepts(r *http.Request) bool {
	return authority(r.Host, "https") == a.authority
}

func (a *door) mine(f *proxy.Flow) bool {
	u := f.Request.URL
	return strings.HasPrefix(u.Path, Path+"/") && authority(u.Host, u.Scheme) == a.authority
}

func (a *door) Requestheaders(f *proxy.Flow) {
	if !a.mine(f) {
		return
	}
	// The proxy reads the original request's body only after this hook, so
	// the handler may read it to find the model and put it back.
	r := f.Request.Raw()
	if !a.h.Takes(r) {
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		// a WebSocket handshake is answered from Body alone
		f.Response = &proxy.Response{StatusCode: http.StatusUpgradeRequired,
			Header: http.Header{"Content-Type": {"text/plain; charset=utf-8"}}, Body: []byte("aicat serves Codex over plain HTTP\n")}
		return
	}
	f.Response = serve(f, a.h, r)
}

// Responseheaders tags the model list ETag that ChatGPT's own replies carry.
func (a *door) Responseheaders(f *proxy.Flow) {
	if a.mine(f) && f.Response != nil {
		a.h.retag(f.Response.Header)
	}
}

var errFlowDone = errors.New("codex: flow ended")

// serve runs the handler for a flow and hands its reply to the proxy as it is
// written: headers once the handler sends them, then the body streamed.
func serve(f *proxy.Flow, h *Handler, r *http.Request) *proxy.Response {
	pr, pw := io.Pipe()
	w := &pipeWriter{header: http.Header{}, ready: make(chan struct{}), body: pw}
	go func() {
		defer func() {
			if v := recover(); v != nil && !w.wrote {
				w.WriteHeader(http.StatusBadGateway)
			}
			w.WriteHeader(http.StatusOK) // a handler that wrote nothing
			_ = pw.Close()
		}()
		h.ServeHTTP(w, r)
	}()
	// a client that goes away stops the handler's writes
	go func() { <-f.Done(); _ = pr.CloseWithError(errFlowDone) }()
	<-w.ready
	return &proxy.Response{StatusCode: w.status, Header: w.sent, BodyReader: pr}
}

type pipeWriter struct {
	header, sent http.Header
	status       int
	wrote        bool
	once         sync.Once
	ready        chan struct{}
	body         *io.PipeWriter
}

func (w *pipeWriter) Header() http.Header { return w.header }
func (w *pipeWriter) WriteHeader(status int) {
	if status < 200 {
		return // 1xx are not passed on
	}
	w.once.Do(func() {
		w.wrote, w.status, w.sent = true, status, w.header.Clone()
		close(w.ready)
	})
}
func (w *pipeWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.body.Write(p)
}

// Flush has nothing to do: every write goes straight to the proxy, which
// flushes each chunk it copies.
func (w *pipeWriter) Flush() {}

// authority is a host and port, the scheme's default port filled in, so a
// request and the tunnel it came through compare equal.
func authority(host, scheme string) string {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		name, port = host, ""
	}
	if port == "" {
		port = "443"
		if scheme == "http" {
			port = "80"
		}
	}
	return strings.ToLower(net.JoinHostPort(strings.TrimSuffix(name, "."), port))
}
