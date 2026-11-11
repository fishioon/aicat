//go:build !nogui

package app

import (
	"errors"
	"github.com/fishioon/aicat/internal/traffic"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func curlRequest(r traffic.Request, reader io.Reader) (string, error) {
	b, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) || strings.IndexByte(string(b), 0) >= 0 {
		return "", errors.New("请求正文不可用")
	}
	parts := []string{"curl --request " + shellQuote(r.Method), "--url " + shellQuote(r.URL), "--compressed"}
	skip := map[string]bool{"content-length": true, "transfer-encoding": true, "connection": true, "proxy-connection": true, "keep-alive": true, "te": true, "trailer": true, "upgrade": true, "accept-encoding": true}
	for _, value := range r.Header.Values("Connection") {
		for _, key := range strings.Split(value, ",") {
			skip[strings.ToLower(strings.TrimSpace(key))] = true
		}
	}
	keys := []string{}
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !skip[strings.ToLower(k)] {
			for _, v := range r.Header[k] {
				parts = append(parts, "--header "+shellQuote(k+": "+v))
			}
		}
	}
	if len(b) > 0 {
		parts = append(parts, "--data-raw "+shellQuote(string(b)))
	}
	return strings.Join(parts, " \\\n  "), nil
}
