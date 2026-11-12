package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/fishioon/aicat/internal/traffic"
)

// credentialHeaders are dropped from what an agent sends, so a placeholder or
// a stale key of the agent's own never reaches the vendor, and masked in the
// traffic record.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Api-Key", "Proxy-Authorization"}

const masked = "[aicat]"

// Handler is the one model endpoint agents point at. It serves the OpenAI
// and Anthropic paths under /v1, picks the provider the request's model
// names, and forwards the request to that provider's endpoint with the saved
// key. The caller restricts it to local peers.
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

// Path is where the model endpoint is served on aicat's listener.
const Path = "/v1/"

// maxBody bounds a request's body: it is read whole to find the model.
const maxBody = 64 << 20

// apiOf names the API a path belongs to, "" when it is none aicat routes.
func apiOf(path string) string {
	switch {
	case path == "/v1/messages" || strings.HasPrefix(path, "/v1/messages/"):
		return "anthropic"
	case path == "/v1/chat/completions":
		return "chat"
	case path == "/v1/responses" || strings.HasPrefix(path, "/v1/responses/"):
		return "responses"
	}
	return ""
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
		h.models(w, r)
		return
	}
	api := apiOf(r.URL.Path)
	if api == "" || r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "aicat 按请求里的 model 选择供应商，只提供 /v1/messages、/v1/chat/completions、/v1/responses 和 /v1/models")
		return
	}
	if enc := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); enc != "" && enc != "identity" {
		writeError(w, http.StatusUnsupportedMediaType, "aicat 读不了压缩的请求正文（Content-Encoding: "+enc+"）")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	r.Body.Close()
	if err != nil || len(body) > maxBody {
		writeError(w, http.StatusRequestEntityTooLarge, "读取请求失败，或请求超过 64 MiB 上限")
		return
	}
	var q map[string]json.RawMessage
	var model string
	if json.Unmarshal(body, &q) != nil || json.Unmarshal(q["model"], &model) != nil && q["model"] != nil {
		writeError(w, http.StatusBadRequest, "请求不是有效的 JSON")
		return
	}
	e, name, err := h.store.resolve(api, model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The provider knows the model without its prefix; a request that named
	// it bare goes on byte for byte.
	if name != model {
		q["model"], _ = json.Marshal(name)
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(q); err != nil {
			writeError(w, http.StatusBadRequest, "请求不是有效的 JSON")
			return
		}
		body = bytes.TrimSuffix(b.Bytes(), []byte("\n"))
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Del("Content-Length")
	// An OpenAI-style base URL ends where /v1 does; Anthropic's is the origin.
	rest := r.URL.Path
	if api != "anthropic" {
		rest = strings.TrimPrefix(rest, "/v1")
	}
	h.forward(w, r, e, rest)
}

// models answers the model list: every model of the providers serving the
// caller's API (Anthropic's when it sends Anthropic-Version), each named
// "<provider ID>/<model>". The shape reads as both vendors' lists do.
func (h *Handler) models(w http.ResponseWriter, r *http.Request) {
	api := "chat"
	if r.Header.Get("Anthropic-Version") != "" {
		api = "anthropic"
	}
	type row struct {
		ID          string `json:"id"`
		Object      string `json:"object"`
		Type        string `json:"type"`
		DisplayName string `json:"display_name"`
		OwnedBy     string `json:"owned_by"`
		Created     int    `json:"created"`
		CreatedAt   string `json:"created_at"`
	}
	list := struct {
		Object  string `json:"object"`
		Data    []row  `json:"data"`
		HasMore bool   `json:"has_more"`
		FirstID string `json:"first_id,omitempty"`
		LastID  string `json:"last_id,omitempty"`
	}{Object: "list", Data: []row{}}
	for _, m := range h.store.catalog(api) {
		list.Data = append(list.Data, row{ID: m.ID, Object: "model", Type: "model", DisplayName: m.Name, OwnedBy: m.Provider, CreatedAt: "1970-01-01T00:00:00Z"})
	}
	if n := len(list.Data); n > 0 {
		list.FirstID, list.LastID = list.Data[0].ID, list.Data[n-1].ID
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(list)
}

// writeError answers in the shape both vendors' clients read an error from.
func writeError(w http.ResponseWriter, status int, message string) {
	b, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]string{"type": "invalid_request_error", "message": "aicat: " + message}})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// forward sends r to the provider's endpoint at rest, with the saved key.
func (h *Handler) forward(w http.ResponseWriter, r *http.Request, e entry, rest string) {
	k := protocols[e.Protocol]
	target, err := url.Parse(e.BaseURL + rest)
	if err != nil {
		writeError(w, http.StatusBadGateway, "供应商的接口地址无效")
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
