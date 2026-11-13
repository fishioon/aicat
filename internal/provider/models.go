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
}

// ModelList is a provider's model list as last synced.
type ModelList struct {
	Models   []Model   `json:"models"`
	SyncedAt time.Time `json:"syncedAt,omitzero"`
	// Source is the URL that answered.
	Source string `json:"source,omitempty"`
	// Error is why the last sync failed; the models are what an earlier one
	// found, if any. ErrorKind sorts it for the page: "auth" (the vendor
	// refused the key), "network" (the vendor was not reached) or "endpoint"
	// (no model list at this address).
	Error     string `json:"error,omitempty"`
	ErrorKind string `json:"errorKind,omitempty"`
}

// fetchError is one list URL's failure.
type fetchError struct {
	kind string
	text string
}

func (e *fetchError) Error() string { return e.text }

func errorKind(err error) string {
	var fe *fetchError
	if errors.As(err, &fe) {
		return fe.kind
	}
	return "endpoint"
}

// fetchTimeout bounds each list URL asked.
const fetchTimeout = 8 * time.Second

// fetchModels asks only the model-list endpoint under the configured API base.
func fetchModels(ctx context.Context, client *http.Client, base, key string) ([]Model, string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(base), "/") + "/models"
	models, err := fetchList(ctx, client, endpoint, key)
	if err != nil {
		return nil, "", err
	}
	if len(models) == 0 {
		return nil, "", &fetchError{"endpoint", endpoint + "：列表为空"}
	}
	return models, endpoint, nil
}

type listedModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Modalities  struct {
		Input []string `json:"input"`
	} `json:"modalities"`
	// any: a vendor's odd value must not lose the whole list
	ContextLength any `json:"context_length"`
	Output        any `json:"max_output_tokens"`
	Levels        any `json:"supported_reasoning_levels"`
}

func fetchList(ctx context.Context, client *http.Client, url, key string) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aicat")
	// Model discovery uses the public list endpoint's common authentication
	// forms. This does not infer or restrict which inference APIs are served.
	req.Header.Set("X-Api-Key", key)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	if req.URL.Hostname() != "api.anthropic.com" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, &fetchError{"network", fmt.Sprintf("%s：%v", url, err)}
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode != http.StatusOK {
		kind := "endpoint"
		if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
			kind = "auth"
		}
		if msg := errorMessage(b); msg != "" {
			return nil, &fetchError{kind, fmt.Sprintf("%s：%s（%s）", url, res.Status, msg)}
		}
		return nil, &fetchError{kind, fmt.Sprintf("%s：%s", url, res.Status)}
	}
	var v struct {
		Data   []listedModel `json:"data"`
		Models []listedModel `json:"models"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, &fetchError{"endpoint", url + "：返回的不是模型列表"}
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
		m := Model{ID: id, Name: r.DisplayName, Efforts: levelsOf(r.Levels)}
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
