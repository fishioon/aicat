// Package plugins hosts bounded, single-file JS/TS modules without UI or proxy dependencies.
package plugins

import "net/http"

const (
	RequestHeaders  = "onRequestHeaders"
	RequestBody     = "onRequest"
	ResponseHeaders = "onResponseHeaders"
	ResponseBody    = "onResponse"
)

var Stages = []string{RequestHeaders, RequestBody, ResponseHeaders, ResponseBody}

// Body is base64 of decoded HTTP entity bytes. It is absent at headers stages;
// an empty string at body stages represents a complete, empty body.
// Header names in the SDK are lower-case and every value is a string array.
type Message struct {
	URL        string         `json:"url,omitempty"`
	Method     string         `json:"method,omitempty"`
	StatusCode int            `json:"statusCode,omitempty"`
	Headers    http.Header    `json:"headers"`
	Body       *string        `json:"body,omitempty"`
	Context    map[string]any `json:"context"`
	Response   *Message       `json:"response,omitempty"`
}
type Manifest struct {
	APIVersion int    `json:"apiVersion"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Main       string `json:"main"`
}
type Info struct {
	Manifest
	Hooks []string `json:"hooks"`
	Hash  string   `json:"hash"`
	Error string   `json:"error,omitempty"`
}
type Limits struct {
	MemoryBytes int64
	ResultBytes int64
}
type CallLog struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}
