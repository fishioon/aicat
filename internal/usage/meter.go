package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Meter watches one call's reply as it streams to the agent and appends its
// record when the reply ends. API is "anthropic", "chat" or "responses".
type Meter struct {
	store *Store
	rec   Record
	start time.Time

	once   sync.Once
	stream bool
	line   []byte
	body   []byte
	whole  bool
}

// maxEvent bounds one SSE line and maxJSON a whole non-streamed reply; a
// longer one is passed on but not read for usage.
const (
	maxEvent = 1 << 20
	maxJSON  = 8 << 20
)

// Begin starts the meter of a call; it is safe on a nil store, where it
// records nothing.
func (s *Store) Begin(rec Record, start time.Time) *Meter {
	if s == nil {
		return nil
	}
	rec.At = start
	return &Meter{store: s, rec: rec, start: start}
}

// Response wraps the reply's body; the record is appended when it is read
// to its end or closed.
func (m *Meter) Response(res *http.Response) io.ReadCloser {
	if m == nil {
		return res.Body
	}
	m.rec.Status = res.StatusCode
	media, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	m.stream = media == "text/event-stream"
	m.whole = true
	return &meterBody{ReadCloser: res.Body, m: m}
}

// Fail appends the record of a call that got no reply.
func (m *Meter) Fail(err error) {
	if m == nil {
		return
	}
	m.rec.Error = err.Error()
	m.finish()
}

type meterBody struct {
	io.ReadCloser
	m *Meter
}

func (b *meterBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.m.write(p[:n])
	}
	if err != nil {
		if !errors.Is(err, io.EOF) {
			b.m.rec.Error = err.Error()
		}
		b.m.finish()
	}
	return n, err
}

func (b *meterBody) Close() error {
	err := b.ReadCloser.Close()
	b.m.finish()
	return err
}

func (m *Meter) write(p []byte) {
	if !m.stream {
		if m.whole && len(m.body)+len(p) <= maxJSON {
			m.body = append(m.body, p...)
		} else {
			m.whole, m.body = false, nil
		}
		return
	}
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			if len(m.line)+len(p) <= maxEvent {
				m.line = append(m.line, p...)
			} else {
				m.line = m.line[:0:0]
			}
			return
		}
		m.line = append(m.line, p[:i]...)
		m.event(m.line)
		m.line = m.line[:0]
		p = p[i+1:]
	}
}

// event reads one SSE line; only data lines carry JSON.
func (m *Meter) event(line []byte) {
	line = bytes.TrimRight(line, "\r")
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return
	}
	var e event
	if json.Unmarshal(data, &e) != nil {
		return
	}
	if m.rec.TTFTMs == 0 && e.content(m.rec.API) {
		m.rec.TTFTMs = max(1, time.Since(m.start).Milliseconds())
	}
	m.read(e)
}

func (m *Meter) finish() {
	m.once.Do(func() {
		if m.stream && len(m.line) > 0 {
			m.event(m.line)
		}
		if !m.stream && m.whole && len(m.body) > 0 {
			var e event
			if json.Unmarshal(m.body, &e) == nil {
				m.read(e)
			}
		}
		m.rec.Ms = time.Since(m.start).Milliseconds()
		m.store.Append(m.rec)
	})
}

// event holds the fields of a reply or a streamed event that name the model
// and the usage, in any of the three APIs.
type event struct {
	Type     string     `json:"type"`
	Model    string     `json:"model"`
	Usage    *usageJSON `json:"usage"`
	Message  *event     `json:"message"`  // Anthropic message_start
	Response *event     `json:"response"` // Responses response.*
	// Chat Completions chunks; the other APIs' "delta" differs in shape and
	// is not read.
	Choices []struct {
		Delta *struct {
			Content   *string         `json:"content"`
			Reasoning *string         `json:"reasoning_content"`
			ToolCalls json.RawMessage `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// usageJSON covers the three shapes. Anthropic reports input without the
// cache; OpenAI includes cached tokens in prompt/input tokens.
type usageJSON struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheRead        int64 `json:"cache_read_input_tokens"`
	CacheWrite       int64 `json:"cache_creation_input_tokens"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	PromptDetails    *struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	InputDetails *struct {
		Cached int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	// DeepSeek
	PromptCacheHit int64 `json:"prompt_cache_hit_tokens"`
}

// content reports whether the event carries the first piece of the answer:
// text, reasoning or a tool call.
func (e event) content(api string) bool {
	switch api {
	case "anthropic":
		return e.Type == "content_block_start" || e.Type == "content_block_delta"
	case "responses":
		return strings.HasPrefix(e.Type, "response.") && strings.HasSuffix(e.Type, ".delta") || e.Type == "response.output_item.added"
	default:
		for _, c := range e.Choices {
			if d := c.Delta; d != nil && (d.Content != nil && *d.Content != "" || d.Reasoning != nil && *d.Reasoning != "" || len(d.ToolCalls) > 0 && string(d.ToolCalls) != "null") {
				return true
			}
		}
	}
	return false
}

// read merges what the event says: a later non-zero field replaces an
// earlier one, so Anthropic's message_start and message_delta add up.
func (m *Meter) read(e event) {
	for _, inner := range []*event{e.Message, e.Response} {
		if inner != nil {
			m.read(*inner)
		}
	}
	if e.Model != "" {
		m.rec.Served = e.Model
	}
	u := e.Usage
	if u == nil {
		return
	}
	set := func(dst *int64, v int64) {
		if v > 0 {
			*dst = v
		}
	}
	switch m.rec.API {
	case "anthropic":
		set(&m.rec.Input, u.InputTokens)
		set(&m.rec.Output, u.OutputTokens)
		set(&m.rec.CacheRead, u.CacheRead)
		set(&m.rec.CacheWrite, u.CacheWrite)
	case "responses":
		cached := int64(0)
		if u.InputDetails != nil {
			cached = u.InputDetails.Cached
		}
		set(&m.rec.Input, u.InputTokens-cached)
		set(&m.rec.CacheRead, cached)
		set(&m.rec.Output, u.OutputTokens)
	default:
		cached := u.PromptCacheHit
		if u.PromptDetails != nil && u.PromptDetails.Cached > 0 {
			cached = u.PromptDetails.Cached
		}
		set(&m.rec.Input, u.PromptTokens-cached)
		set(&m.rec.CacheRead, cached)
		set(&m.rec.Output, u.CompletionTokens)
	}
}
