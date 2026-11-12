package codex

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// maxBody bounds a turn's body as Codex sent it and once decompressed. It is
// read whole to find the model, so a turn with screenshots still fits.
const maxBody = 64 << 20

var errTooLarge = errors.New("Codex 请求或解压后的请求超过 64 MiB 上限")

// readBody reads a request body whole and puts it back, so the request can
// still be sent on as it came. decoded is the body Codex compressed (zstd for
// the ChatGPT backend), as JSON.
func readBody(r *http.Request) (raw, decoded []byte, err error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil, nil
	}
	raw, err = io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	if err != nil {
		return raw, nil, errors.New("读取 Codex 请求失败")
	}
	if len(raw) > maxBody {
		return raw, nil, errTooLarge
	}
	var reader io.Reader
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))) {
	case "", "identity":
		return raw, raw, nil
	case "gzip":
		z, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return raw, nil, errors.New("无效的 gzip 请求")
		}
		defer z.Close()
		reader = z
	case "zstd":
		z, err := zstd.NewReader(bytes.NewReader(raw), zstd.WithDecoderMaxMemory(128<<20), zstd.WithDecoderConcurrency(1))
		if err != nil {
			return raw, nil, errors.New("无效的 zstd 请求")
		}
		defer z.Close()
		reader = z
	default:
		return raw, nil, errors.New("不支持的 Codex Content-Encoding")
	}
	decoded, err = io.ReadAll(io.LimitReader(reader, maxBody+1))
	if err != nil {
		return raw, nil, errors.New("解压 Codex 请求失败")
	}
	if len(decoded) > maxBody {
		return raw, nil, errTooLarge
	}
	return raw, decoded, nil
}

// modelOf is the model a Responses request asks for.
func modelOf(body []byte) string {
	var q struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &q)
	return q.Model
}

// withModel is the request asking for another model, its other fields as
// they were.
func withModel(body []byte, model string) ([]byte, error) {
	var q map[string]json.RawMessage
	if err := json.Unmarshal(body, &q); err != nil {
		return nil, err
	}
	q["model"], _ = json.Marshal(model)
	return marshal(q)
}

// compacting reports whether Codex asks for the conversation to be compacted
// in this turn: a compaction_trigger among its input.
func compacting(body []byte) bool {
	var q struct {
		Input []struct {
			Type string `json:"type"`
		} `json:"input"`
	}
	if json.Unmarshal(body, &q) != nil {
		return false
	}
	return slices.ContainsFunc(q.Input, func(it struct {
		Type string `json:"type"`
	}) bool {
		return it.Type == "compaction_trigger"
	})
}

// openaiOnly are the parts of a Responses request Codex sends only to OpenAI
// (client.rs, !is_openai), and that a relay checking for Codex's request
// turns away. Taken from Magpie's forVendor (MIT, github.com/yetone/magpie).
var openaiOnly = [][]byte{[]byte(`"internal_chat_message_metadata_passthrough"`),
	[]byte(`"encrypted_function_args"`), []byte(`"stream_options"`), []byte(`"configuration_update"`)}

// forVendor is a Responses request as Codex sends it to a provider not
// OpenAI's: without its messages' internal metadata, a call's
// encrypted_function_args, stream_options' reasoning_summary_delivery and
// configuration_update items. A body with none of them is left byte for byte.
func forVendor(body []byte) []byte {
	if !slices.ContainsFunc(openaiOnly, func(k []byte) bool { return bytes.Contains(body, k) }) {
		return body
	}
	var q map[string]json.RawMessage
	if json.Unmarshal(body, &q) != nil {
		return body
	}
	changed := false
	var so map[string]json.RawMessage
	if json.Unmarshal(q["stream_options"], &so) == nil {
		if _, ok := so["reasoning_summary_delivery"]; ok {
			changed = true
			delete(so, "reasoning_summary_delivery")
			if q["stream_options"], _ = marshal(so); len(so) == 0 {
				delete(q, "stream_options")
			}
		}
	}
	var items []json.RawMessage
	if json.Unmarshal(q["input"], &items) == nil {
		out := items[:0]
		dropped := false
		for _, raw := range items {
			var it map[string]json.RawMessage
			if json.Unmarshal(raw, &it) != nil {
				out = append(out, raw)
				continue
			}
			if string(it["type"]) == `"configuration_update"` {
				dropped = true
				continue
			}
			_, meta := it["internal_chat_message_metadata_passthrough"]
			_, sealed := it["encrypted_function_args"]
			if meta || sealed {
				dropped = true
				delete(it, "internal_chat_message_metadata_passthrough")
				delete(it, "encrypted_function_args")
				raw, _ = marshal(it)
			}
			out = append(out, raw)
		}
		if dropped {
			changed = true
			q["input"], _ = marshal(out)
		}
	}
	if !changed {
		return body
	}
	b, err := marshal(q)
	if err != nil {
		return body
	}
	return b
}

// marshal is json.Marshal without HTML escaping, so text the model reads
// back keeps its < > &.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}
