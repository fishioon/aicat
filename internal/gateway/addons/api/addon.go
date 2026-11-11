// Package api mounts application handlers on the proxy's origin-form entry.
package api

import (
	"encoding/json"
	"errors"
	"github.com/fishioon/aicat/internal/listenaddr"
	"github.com/fishioon/aicat/internal/traffic"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
	"io"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
)

type Addon struct {
	proxy.BaseAddon
	authority string
	handler   http.Handler
}

func New(authority string, handler http.Handler) *Addon {
	return &Addon{authority: authority, handler: handler}
}
func (a *Addon) AccessProxyServer(r *http.Request, w http.ResponseWriter) {
	if w.Header().Get("X-Aicat-Instance") != "" {
		return
	}
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	ip, parseErr := netip.ParseAddr(peer)
	if err != nil || parseErr != nil || !listenaddr.IsLocal(ip) {
		http.Error(w, "application endpoints are local only", http.StatusForbidden)
		return
	}
	if !strings.EqualFold(r.Host, a.authority) && !strings.EqualFold(r.Host, listenaddr.ClientAddress(a.authority)) {
		http.Error(w, "unknown authority", http.StatusForbidden)
		return
	}
	if r.Header.Get("Origin") != "" {
		http.Error(w, "browser origins are not allowed", http.StatusForbidden)
		return
	}
	a.handler.ServeHTTP(streaming(w), r)
}

// The proxy hands addons a writer that tracks whether anything was written
// but hides http.Flusher, so streamed responses (SSE from a model vendor, MCP)
// would sit in the buffer. streaming keeps writes on that writer and flushes
// through the one it embeds.
type flushWriter struct {
	http.ResponseWriter
	flusher http.Flusher
}

func (w flushWriter) Flush() { w.flusher.Flush() }

func streaming(w http.ResponseWriter) http.ResponseWriter {
	if _, ok := w.(http.Flusher); ok {
		return w
	}
	v := reflect.ValueOf(w)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return w
	}
	field := v.Elem().FieldByName("ResponseWriter")
	if !field.IsValid() || !field.CanInterface() {
		return w
	}
	if flusher, ok := field.Interface().(http.Flusher); ok {
		return flushWriter{ResponseWriter: w, flusher: flusher}
	}
	return w
}
func Handler(s *traffic.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/traffic", func(w http.ResponseWriter, r *http.Request) {
		limit := 200
		if v := r.URL.Query().Get("limit"); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || n > 2000 {
				http.Error(w, "invalid limit (1..2000)", 400)
				return
			}
			limit = n
		}
		before := uint64(0)
		if v := r.URL.Query().Get("before"); v != "" {
			n, e := strconv.ParseUint(v, 10, 64)
			if e != nil {
				http.Error(w, "invalid before ID", 400)
				return
			}
			before = n
		}
		q := traffic.Query{Limit: limit, Before: traffic.ID(before), Host: r.URL.Query().Get("host"), Method: r.URL.Query().Get("method"), State: traffic.State(r.URL.Query().Get("state")), Text: r.URL.Query().Get("q")}
		page, err := s.List(r.Context(), q)
		writeJSON(w, page, err)
	})
	mux.HandleFunc("GET /api/traffic/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
		if err != nil || id == 0 {
			http.Error(w, "invalid request ID", 400)
			return
		}
		rec, err := s.Get(r.Context(), traffic.ID(id))
		writeJSON(w, rec, err)
	})
	mux.HandleFunc("GET /api/bodies/{id}", func(w http.ResponseWriter, r *http.Request) {
		body, err := s.OpenBody(r.Context(), traffic.BodyID(r.PathValue("id")))
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		defer body.Reader.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Aicat-Body-State", body.Info.State)
		_, _ = io.Copy(w, body.Reader)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, v any, err error) {
	if err != nil {
		status := 500
		if errors.Is(err, traffic.ErrNotFound) {
			status = 404
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
