package rewrite

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// Bound decoded bytes as well as wire bytes. Decode failures never replace the
// original payload; unchanged decoded output preserves its original encoding.
func decodeBody(encoding string, raw []byte, limit int64) ([]byte, error) {
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("encoded body exceeds %d bytes", limit)
	}
	if len(raw) == 0 {
		return raw, nil
	}
	encodings := strings.Split(strings.ToLower(strings.TrimSpace(encoding)), ",")
	body := raw
	for i := len(encodings) - 1; i >= 0; i-- {
		var reader io.Reader = bytes.NewReader(body)
		var close func() error
		switch strings.TrimSpace(encodings[i]) {
		case "", "identity":
			continue
		case "gzip":
			r, e := gzip.NewReader(reader)
			if e != nil {
				return nil, e
			}
			reader = r
			close = r.Close
		case "deflate":
			r, e := zlib.NewReader(reader)
			if e == nil {
				reader, close = r, r.Close
			} else {
				// Some HTTP peers send raw DEFLATE despite the zlib wrapper
				// required by the content-coding definition.
				raw := flate.NewReader(bytes.NewReader(body))
				reader, close = raw, raw.Close
			}
		case "br":
			reader = brotli.NewReader(reader)
		case "zstd":
			r, e := zstd.NewReader(reader, zstd.WithDecoderMaxMemory(32<<20), zstd.WithDecoderConcurrency(1))
			if e != nil {
				return nil, e
			}
			reader = r
			close = func() error { r.Close(); return nil }
		default:
			return nil, fmt.Errorf("unsupported content encoding %q", encodings[i])
		}
		b, err := io.ReadAll(io.LimitReader(reader, limit+1))
		if close != nil {
			_ = close()
		}
		if err != nil {
			return nil, err
		}
		if int64(len(b)) > limit {
			return nil, fmt.Errorf("decoded body exceeds %d bytes", limit)
		}
		body = b
	}
	return body, nil
}
