package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Model is one model a provider's own list says it serves. The list is the
// truth about what the vendor serves today; anything it leaves out (a name,
// a context window, reasoning levels) is filled in from the models.dev
// catalog where the models are offered, not here.
//
// The way a list is asked for and read follows Magpie (MIT,
// github.com/yetone/magpie internal/catalog/live.go).
type Model struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Context int      `json:"context,omitempty"`
	Output  int      `json:"output,omitempty"`
	Efforts []string `json:"efforts,omitempty"`
	// Images is the vendor's answer on image input; nil when it didn't say.
	Images *bool `json:"images,omitempty"`
	// APIs are the APIs the vendor says the model is served on ("chat",
	// "responses", "anthropic"); empty is not known.
	APIs []string `json:"apis,omitempty"`
}

// ModelList is a provider's model list as last synced.
type ModelList struct {
	Models   []Model   `json:"models"`
	SyncedAt time.Time `json:"syncedAt,omitzero"`
	// Source is the URL that answered.
	Source string `json:"source,omitempty"`
	// Error is why the last sync failed; the models are what an earlier one
	// found, if any.
	Error string `json:"error,omitempty"`
}

// fetchTimeout bounds each list URL asked.
const fetchTimeout = 8 * time.Second

// fetchModels asks the vendor at base which models it serves. The usual list
// paths are tried around base: an OpenAI-style base with a version in its
// path is asked only as written, at base/models.
func fetchModels(ctx context.Context, client *http.Client, k Kind, base, key string) ([]Model, string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	anthropic := k.ID == "anthropic"
	var urls []string
	add := func(u string) {
		if !slices.Contains(urls, u) {
			urls = append(urls, u)
		}
	}
	add(base + "/models")
	if anthropic || !versioned(base) {
		add(base + "/v1/models")
		root := base
		for _, suffix := range []string{"/anthropic", "/v1", "/api", "/api/v1"} {
			root = strings.TrimSuffix(root, suffix)
		}
		add(root + "/v1/models")
		add(root + "/models")
	}
	var errs []string
	for _, u := range urls {
		ms, err := fetchList(ctx, client, k, u, key)
		if err == nil && len(ms) > 0 {
			return ms, u, nil
		}
		if err == nil {
			err = errors.New(u + "：列表为空")
		}
		errs = append(errs, err.Error())
		if ctx.Err() != nil {
			break
		}
	}
	return nil, "", errors.New("没有取得模型列表：" + strings.Join(errs, "；"))
}

// versioned reports whether a base URL has a version segment in its path
// (v1, v3, v4, v1beta) and so names the vendor's API as it is.
func versioned(base string) bool {
	rest := base
	if _, after, ok := strings.Cut(base, "://"); ok {
		rest = after
	}
	_, path, _ := strings.Cut(rest, "/")
	for _, seg := range strings.Split(path, "/") {
		if len(seg) >= 2 && (seg[0] == 'v' || seg[0] == 'V') && seg[1] >= '0' && seg[1] <= '9' {
			return true
		}
	}
	return false
}

type listedModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Modalities  struct {
		Input []string `json:"input"`
	} `json:"modalities"`
	Endpoints []string `json:"supported_endpoints"`
	// any: a vendor's odd value must not lose the whole list
	ContextLength any `json:"context_length"`
	Output        any `json:"max_output_tokens"`
	Levels        any `json:"supported_reasoning_levels"`
}

func fetchList(ctx context.Context, client *http.Client, k Kind, url, key string) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aicat")
	req.Header.Set(k.header, k.prefix+key)
	if k.ID == "anthropic" {
		req.Header.Set("Anthropic-Version", "2023-06-01")
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s：%w", url, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode != http.StatusOK {
		if msg := errorMessage(b); msg != "" {
			return nil, fmt.Errorf("%s：%s（%s）", url, res.Status, msg)
		}
		return nil, fmt.Errorf("%s：%s", url, res.Status)
	}
	var v struct {
		Data   []listedModel `json:"data"`
		Models []listedModel `json:"models"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("%s：返回的不是模型列表", url)
	}
	rows := v.Data
	if len(rows) == 0 {
		rows = v.Models
	}
	var out []Model
	for _, r := range rows {
		id := r.ID
		if id == "" {
			id = r.Name
		}
		if id == "" || !chatModel(id) {
			continue
		}
		m := Model{ID: id, Name: r.DisplayName, APIs: endpointAPIs(r.Endpoints), Efforts: levelsOf(r.Levels)}
		if m.Name == "" && r.Name != id {
			m.Name = r.Name
		}
		if r.Modalities.Input != nil {
			images := slices.Contains(r.Modalities.Input, "image")
			m.Images = &images
		}
		if n, ok := r.ContextLength.(float64); ok && n > 0 {
			m.Context = int(n)
		}
		if n, ok := r.Output.(float64); ok && n > 0 {
			m.Output = int(n)
		}
		out = append(out, m)
	}
	return out, nil
}

// chatModel leaves out what is not a model to talk to: embeddings, speech,
// image and video makers, moderation and the like, by the words in its id.
func chatModel(id string) bool {
	id = strings.ToLower(id)
	for _, bad := range []string{"embed", "-tts", "tts-", "image", "imagen", "imagine", "audio", "-live", "robotics", "computer-use",
		"deep-research", "transcribe", "realtime", "moderation", "whisper", "dall-e", "sora", "flux", "seedream", "cogview",
		"stable-diffusion", "sdxl", "wanx", "kolors", "hidream", "rerank"} {
		if strings.Contains(id, bad) {
			return false
		}
	}
	return true
}

// endpointAPIs names the APIs of a list's supported_endpoints; nil when it
// names none aicat knows.
func endpointAPIs(endpoints []string) []string {
	var out []string
	for _, e := range endpoints {
		var api string
		switch strings.TrimPrefix(strings.TrimSuffix(e, "/"), "/v1") {
		case "/chat/completions":
			api = "chat"
		case "/responses":
			api = "responses"
		case "/messages":
			api = "anthropic"
		}
		if api != "" && !slices.Contains(out, api) {
			out = append(out, api)
		}
	}
	return out
}

// levelsOf reads supported_reasoning_levels as Codex writes them
// ([{"effort":"high"}]) or as plain names.
func levelsOf(v any) []string {
	xs, _ := v.([]any)
	var out []string
	for _, x := range xs {
		e, _ := x.(string)
		if o, ok := x.(map[string]any); ok {
			e, _ = o["effort"].(string)
		}
		if e != "" && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}

// errorMessage is the message of a vendor's JSON error reply, cut short.
func errorMessage(b []byte) string {
	var v struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	msg := v.Message
	var e struct {
		Message string `json:"message"`
	}
	var s string
	if json.Unmarshal(v.Error, &e) == nil && e.Message != "" {
		msg = e.Message
	} else if json.Unmarshal(v.Error, &s) == nil && s != "" {
		msg = s
	}
	msg = strings.TrimSpace(msg)
	if r := []rune(msg); len(r) > 160 {
		msg = string(r[:160]) + "…"
	}
	return msg
}
