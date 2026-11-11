// Package uihost provides the portable desktop HTTP contract and frontend.
// Platform operations are injected by app; this package has no MyGo dependency.
package uihost

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fishioon/aicat/internal/traffic"
	"github.com/fishioon/aicat/ui"
)

type Host struct {
	flows                                      *traffic.Service
	Control                                    func(context.Context, string, json.RawMessage) (any, error)
	batches, bytes, details, bodies, bodyBytes atomic.Uint64
}

func New(flows *traffic.Service) *Host { return &Host{flows: flows} }

func (h *Host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if origin := r.Header.Get("Origin"); origin != "" && origin != "mygo://localhost" && origin != "http://"+r.Host {
		http.Error(w, "origin rejected", http.StatusForbidden)
		return
	}
	switch {
	case h.Control != nil && (r.Method == http.MethodGet && r.URL.Path == "/state" || r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/command/")):
		action := strings.TrimPrefix(r.URL.Path, "/command/")
		if r.Method == http.MethodGet {
			action = "state"
		}
		var input json.RawMessage
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input); err != nil {
				http.Error(w, "invalid command", 400)
				return
			}
		}
		value, err := h.Control(r.Context(), action, input)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if value == nil {
			value = struct{}{}
		}
		_ = json.NewEncoder(w).Encode(value)
	case r.Method == http.MethodGet && r.URL.Path == "/watch":
		h.watch(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/record/"):
		id, err := strconv.ParseUint(strings.TrimPrefix(r.URL.Path, "/record/"), 10, 64)
		if err != nil {
			http.Error(w, "invalid request id", 400)
			return
		}
		value, err := h.flows.Get(r.Context(), traffic.ID(id))
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		h.details.Add(1)
		_ = json.NewEncoder(w).Encode(value)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/body/"):
		body, err := h.flows.OpenBody(r.Context(), traffic.BodyID(strings.TrimPrefix(r.URL.Path, "/body/")))
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		defer body.Reader.Close()
		data, err := io.ReadAll(body.Reader)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		h.bodies.Add(1)
		h.bodyBytes.Add(uint64(len(data)))
		_ = json.NewEncoder(w).Encode(struct {
			Info traffic.BodyInfo `json:"info"`
			Data []byte           `json:"data"`
		}{body.Info, data})
	case r.Method == http.MethodGet && r.URL.Path == "/status":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"batches":    h.batches.Load(),
			"watchBytes": h.bytes.Load(), "detailReads": h.details.Load(),
			"bodyReads": h.bodies.Load(), "bodyBytes": h.bodyBytes.Load(),
		})
	case r.Method == http.MethodPost && r.URL.Path == "/clear":
		if err := h.flows.Clear(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, _ = io.WriteString(w, "{}")

	default:
		http.NotFound(w, r)
	}
}

type batch struct {
	Reset   bool              `json:"reset"`
	Items   []traffic.Summary `json:"items"`
	Removed []traffic.ID      `json:"removed"`
	SentAt  int64             `json:"sentAt"`
}

func (h *Host) watch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	page, sub, err := h.flows.Watch(r.Context(), traffic.Query{Limit: 2000})
	if err != nil {
		http.Error(w, err.Error(), 503)
		return
	}
	defer func() { sub.Close() }()
	previous := make(map[traffic.ID]uint64)
	send := func(page traffic.Page, reset bool) error {
		update := batch{Reset: reset, Items: []traffic.Summary{}, Removed: []traffic.ID{}, SentAt: time.Now().UnixMilli()}
		next := make(map[traffic.ID]uint64, len(page.Items))
		for _, row := range page.Items {
			next[row.ID] = row.Revision
			if reset || previous[row.ID] != row.Revision {
				update.Items = append(update.Items, row)
			}
		}
		for id := range previous {
			if _, exists := next[id]; !exists {
				update.Removed = append(update.Removed, id)
			}
		}
		previous = next
		if !reset && len(update.Items) == 0 && len(update.Removed) == 0 {
			return nil
		}
		slices.SortFunc(update.Items, func(a, b traffic.Summary) int {
			return cmp.Compare(a.ID, b.ID)
		})
		data, err := json.Marshal(update)
		if err != nil {
			return err
		}
		data = append(data, '\n')
		if _, err := w.Write(data); err != nil {
			return err
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			return err
		}
		h.batches.Add(1)
		h.bytes.Add(uint64(len(data)))
		return nil
	}
	if send(page, true) != nil {
		return
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	dirty := false
	for {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-sub.Changes:
			if !open {
				if !errors.Is(sub.Err(), traffic.ErrSlowSubscriber) {
					return
				}
				// Recover service backpressure inside this response. WKWebView
				// custom-scheme readers need not observe an early EOF promptly.
				page, sub, err = h.flows.Watch(r.Context(), traffic.Query{Limit: 2000})
				if err != nil || send(page, true) != nil {
					return
				}
				dirty = false
				continue
			}
			dirty = true
		case <-ticker.C:
			if !dirty {
				continue
			}
			page, err = h.flows.List(r.Context(), traffic.Query{Limit: 2000})
			if err != nil || send(page, false) != nil {
				return
			}
			dirty = false
		}
	}
}

// Preview serves the same frontend and adapter over ordinary HTTP so the
// authenticated test preview can exercise browser pointer and keyboard input.
func (h *Host) Preview() http.Handler {
	files := http.FileServerFS(ui.Files())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "origin rejected", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			copy := r.Clone(r.Context())
			copy.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
			h.ServeHTTP(w, copy)
			return
		}
		files.ServeHTTP(w, r)
	})
}
