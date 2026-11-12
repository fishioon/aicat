// Package codex extends what Codex's ChatGPT backend serves with the models of
// the providers aicat holds keys for.
//
// Codex keeps its own sign-in and sends what it would send chatgpt.com: its
// model list, which is the backend's with the providers' models added as
// provider/model, and its turns, where one that asks for a provider's model
// goes to that provider with the provider's key and none of Codex's
// credentials, and any other goes on to ChatGPT as it came. Codex reaches the
// handler two ways: through the gateway, where its HTTPS to chatgpt.com is
// decrypted (internal/gateway/addons/codex), or with openai_base_url pointing
// at aicat's listener. The way Codex is served follows Magpie (MIT,
// github.com/yetone/magpie internal/gateway/codex_backend.go).
package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fishioon/aicat/internal/catalog"
	"github.com/fishioon/aicat/internal/provider"
	"github.com/fishioon/aicat/internal/traffic"
)

// Path is where Codex's backend requests live, on chatgpt.com and on aicat.
const Path = "/backend-api/codex"

// Backend is Codex's own backend.
const Backend = "https://chatgpt.com"

type Options struct {
	// Backend is where Codex's own requests go, as an origin; empty is
	// chatgpt.com.
	Backend   string
	Providers *provider.Store
	// Forward serves the provider routes (provider.Handler): a provider's
	// turn is sent through it, with its key, as any agent's request is.
	Forward   http.Handler
	Catalog   *catalog.Catalog
	Recorder  *traffic.Recorder
	Recording func() bool
}

type Handler struct {
	backend   *url.URL
	providers *provider.Store
	forward   http.Handler
	catalog   *catalog.Catalog
	recorder  *traffic.Recorder
	recording func() bool
	transport *http.Transport
	client    *http.Client
	enabled   atomic.Bool
}

func New(o Options) (*Handler, error) {
	raw := o.Backend
	if raw == "" {
		raw = Backend
	}
	backend, err := url.Parse(raw)
	if err != nil || backend.Host == "" || backend.Scheme != "https" && backend.Scheme != "http" {
		return nil, errors.New("codex: backend must be an absolute HTTP(S) origin")
	}
	transport := &http.Transport{
		// never through an environment proxy that may be aicat itself
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 15 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}
	return &Handler{backend: backend, providers: o.Providers, forward: o.Forward, catalog: o.Catalog, recorder: o.Recorder, recording: o.Recording,
		transport: transport, client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (h *Handler) Enabled() bool         { return h.enabled.Load() }
func (h *Handler) SetEnabled(on bool)    { h.enabled.Store(on) }
func (h *Handler) BackendHost() *url.URL { u := *h.backend; return &u }

// Takes reports whether the handler answers a request Codex sent to its
// backend through the gateway: its model list, a turn for a provider's model,
// or a WebSocket it turns down. A turn for one of ChatGPT's own models is left
// to the gateway, which sends it on as it came. Its body is read to find the
// model and put back.
func (h *Handler) Takes(r *http.Request) bool {
	if !h.Enabled() || !strings.HasPrefix(r.URL.Path, Path+"/") {
		return false
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, Path), "/")
	switch {
	case webSocket(r.Header):
		return true
	case r.Method == http.MethodGet && rest == "/models":
		return true
	case r.Method == http.MethodPost && (rest == "/responses" || rest == "/responses/compact"):
		_, body, err := readBody(r)
		// an unreadable body goes on to ChatGPT, which answers as it did
		return err == nil && strings.Contains(modelOf(body), "/")
	}
	return false
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "aicat 的 Codex 扩展未启用；在 aicat 的扩展里启用后再试")
		return
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, Path), "/")
	if webSocket(r.Header) {
		// 426 sends Codex to the same call over plain HTTP at once, where
		// anything else costs it retries; plain HTTP is the one aicat reads.
		writeError(w, http.StatusUpgradeRequired, "aicat serves Codex over plain HTTP")
		return
	}
	switch {
	case r.Method == http.MethodGet && rest == "/models":
		h.models(w, r)
		return
	case r.Method == http.MethodPost && (rest == "/responses" || rest == "/responses/compact"):
		_, body, err := readBody(r)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errTooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			writeError(w, status, err.Error())
			return
		}
		if model := modelOf(body); strings.Contains(model, "/") {
			h.vendor(w, r, rest, body, model)
			return
		}
	}
	h.passthrough(w, r)
}

// vendor serves a turn for a provider's model. The provider/model namespace
// owns the route: a model that isn't offered fails here and never falls
// through to ChatGPT.
func (h *Handler) vendor(w http.ResponseWriter, r *http.Request, rest string, body []byte, model string) {
	id, name, _ := strings.Cut(model, "/")
	if rest == "/responses/compact" || compacting(body) {
		writeError(w, http.StatusBadRequest, "aicat 暂不支持压缩供应商模型的对话（"+model+"）。请新开一个对话，或换回 ChatGPT 自带的模型。")
		return
	}
	var chosen *provider.ResponsesModel
	for _, m := range h.providers.ResponsesModels() {
		if m.Provider == id && m.ID == name {
			chosen = &m
			break
		}
	}
	if chosen == nil {
		writeError(w, http.StatusBadRequest, "模型 "+model+" 不可用：供应商未在 aicat 中保存、不支持 Responses 接口，或模型不在已同步的列表里。请在 aicat 的“接入供应商”中重新同步模型列表。")
		return
	}
	body, err := withModel(body, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "请求不是有效的 JSON")
		return
	}
	if !chosen.OpenAI {
		body = forVendor(body)
	}
	// Fresh headers: never ChatGPT's bearer, cookies, account, attestation
	// or backend-only metadata. The provider route adds its own key.
	out, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "http://aicat"+chosen.Mount+"/responses", bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusBadGateway, "无法构造供应商请求")
		return
	}
	out.Header.Set("Content-Type", "application/json")
	if accept := r.Header.Get("Accept"); accept != "" {
		out.Header.Set("Accept", accept)
	}
	out.Header.Set("User-Agent", "aicat")
	h.forward.ServeHTTP(w, out)
}

// models is the backend's model list for Codex's sign-in, with the providers'
// models after it.
func (h *Handler) models(w http.ResponseWriter, r *http.Request) {
	target := *h.backend
	target.Path, target.RawQuery = r.URL.Path, r.URL.RawQuery
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	copyHeaders(req.Header, r.Header)
	for _, key := range []string{"Accept-Encoding", "If-None-Match", "If-Modified-Since"} {
		req.Header.Del(key)
	}
	rec := h.mirror(req)
	rec.RequestBody(nil)
	res, err := h.client.Do(req)
	if err != nil {
		rec.Finish(traffic.Failed, err.Error())
		writeError(w, http.StatusBadGateway, "无法从 "+h.backend.Host+" 获取 Codex 模型列表："+err.Error())
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		// a sign-in to renew, and the like, is Codex's to see
		h.relay(w, res, rec)
		return
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20+1))
	if err != nil || len(body) > 16<<20 {
		rec.Finish(traffic.Failed, "model list unreadable or too large")
		writeError(w, http.StatusBadGateway, "Codex 模型列表读取失败或过大")
		return
	}
	var document map[string]json.RawMessage
	var own []json.RawMessage
	if json.Unmarshal(body, &document) != nil || json.Unmarshal(document["models"], &own) != nil {
		rec.Finish(traffic.Failed, "not a model list")
		writeError(w, http.StatusBadGateway, "Codex 模型列表格式无效")
		return
	}
	seen := map[string]bool{}
	for _, raw := range own {
		var item struct {
			Slug string `json:"slug"`
		}
		_ = json.Unmarshal(raw, &item)
		seen[item.Slug] = true
	}
	for _, e := range h.entries(len(own) + 100) {
		if seen[e.Slug] {
			continue
		}
		b, _ := marshal(e)
		own = append(own, b)
	}
	if own == nil {
		own = []json.RawMessage{}
	}
	document["models"], _ = marshal(own)
	merged, _ := marshal(document)
	header := res.Header.Clone()
	dropHop(header)
	for _, key := range []string{"Content-Length", "Content-Encoding", "Content-Md5", "Digest", "Last-Modified", "Set-Cookie"} {
		header.Del(key)
	}
	header.Set("Content-Type", "application/json")
	header.Set("Cache-Control", "no-cache")
	header.Set("ETag", withTag(res.Header.Get("ETag"), h.tag()))
	h.retag(header)
	h.relay(w, &http.Response{StatusCode: res.StatusCode, Proto: res.Proto, Header: header, Body: io.NopCloser(bytes.NewReader(merged))}, rec)
}

// passthrough sends a request on to Codex's backend as it came, its sign-in
// included. Only the local entry needs it: through the gateway, ChatGPT's own
// turns never reach this handler.
func (h *Handler) passthrough(w http.ResponseWriter, r *http.Request) {
	target := *h.backend
	target.Path, target.RawPath, target.RawQuery = r.URL.Path, r.URL.RawPath, r.URL.RawQuery
	header := r.Header.Clone()
	header.Del("Accept-Encoding")
	rec := h.mirror(&http.Request{Method: r.Method, URL: &target, Proto: r.Proto, Header: header})
	r.Body = rec.RequestBody(r.Body)
	(&httputil.ReverseProxy{
		Transport:     h.transport,
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = &target
			pr.Out.Host = target.Host
			// let the transport negotiate and undo compression, so the
			// record stays readable
			pr.Out.Header.Del("Accept-Encoding")
		},
		ModifyResponse: func(res *http.Response) error {
			h.retag(res.Header)
			res.Body = rec.Response(res)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			rec.Finish(traffic.Failed, err.Error())
			writeError(w, http.StatusBadGateway, "无法连接 "+h.backend.Host+"："+err.Error())
		},
	}).ServeHTTP(w, r)
}

// Retag puts aicat's tag into a backend reply's X-Models-Etag, as the model
// list's own ETag has it, so a reply doesn't send Codex for the list again
// each turn and a change to the providers' models does.
func (h *Handler) Retag(header http.Header) {
	if h.Enabled() {
		h.retag(header)
	}
}

func (h *Handler) retag(header http.Header) {
	if v := header.Get("X-Models-Etag"); v != "" && !strings.Contains(v, tagMark) {
		header.Set("X-Models-Etag", withTag(v, h.tag()))
	}
}

// relay writes a response and records it as it is written.
func (h *Handler) relay(w http.ResponseWriter, res *http.Response, rec *traffic.Mirror) {
	body := rec.Response(res)
	defer body.Close()
	for key, values := range res.Header {
		if !hop(key) {
			w.Header()[key] = values
		}
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, body)
}

// mirror records an exchange with Codex's backend, its credentials masked.
func (h *Handler) mirror(r *http.Request) *traffic.Mirror {
	if h.recorder == nil || h.recording != nil && !h.recording() {
		return nil
	}
	header := r.Header.Clone()
	for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
		if header.Get(name) != "" {
			header.Set(name, "[aicat]")
		}
	}
	return h.recorder.Mirror(traffic.Request{Method: r.Method, URL: r.URL.String(), Proto: r.Proto, Header: header})
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		if !hop(key) && http.CanonicalHeaderKey(key) != "Host" {
			dst[key] = append([]string(nil), values...)
		}
	}
}

func dropHop(header http.Header) {
	for _, key := range strings.Split(header.Get("Connection"), ",") {
		header.Del(strings.TrimSpace(key))
	}
	for key := range header {
		if hop(key) {
			header.Del(key)
		}
	}
}

func hop(key string) bool {
	switch http.CanonicalHeaderKey(key) {
	case "Connection", "Keep-Alive", "Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}

func webSocket(header http.Header) bool {
	return strings.EqualFold(header.Get("Upgrade"), "websocket")
}

// writeError answers in the error shape Codex reads from its backend.
func writeError(w http.ResponseWriter, status int, message string) {
	b, _ := marshal(map[string]any{"error": map[string]string{"type": "aicat_error", "message": message}})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
