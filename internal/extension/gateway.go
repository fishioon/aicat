package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

type gatewayChannel interface {
	GatewayCapabilities() channelplugin.Capabilities
	Gateway(context.Context, any) (json.RawMessage, error)
	Observe(any)
}

type gatewayEvent struct {
	Stage           string      `json:"stage"`
	URL             string      `json:"url"`
	Method          string      `json:"method"`
	RequestHeaders  http.Header `json:"requestHeaders"`
	ResponseHeaders http.Header `json:"responseHeaders,omitempty"`
	Status          int         `json:"status,omitempty"`
	Body            []byte      `json:"body,omitempty"`
	Time            time.Time   `json:"time"`
	Connection      string      `json:"connection,omitempty"`
	FromClient      bool        `json:"fromClient,omitempty"`
	MessageType     int         `json:"messageType,omitempty"`
	OriginalBytes   int         `json:"originalBytes,omitempty"`
	Truncated       bool        `json:"truncated,omitempty"`
}

type gatewayResult struct {
	RequestHeaders http.Header `json:"requestHeaders,omitempty"`
	Response       *struct {
		Status  int         `json:"status"`
		Headers http.Header `json:"headers"`
		Body    []byte      `json:"body"`
	} `json:"response,omitempty"`
}

type nativeGateway struct {
	proxy.BaseAddon
	host *Host
}

func matchesGatewayHost(pattern string, target *url.URL) bool {
	if target == nil {
		return false
	}
	u, err := url.Parse("https://" + pattern)
	if err != nil || !strings.EqualFold(u.Hostname(), target.Hostname()) {
		return false
	}
	port := target.Port()
	if port == "" {
		if target.Scheme == "https" || target.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return u.Port() == "" || u.Port() == port
}

func matchesGatewayHosts(hosts []string, target *url.URL) bool {
	for _, host := range hosts {
		if matchesGatewayHost(host, target) {
			return true
		}
	}
	return false
}

func (g nativeGateway) channels(f *proxy.Flow, visit func(*item, gatewayChannel, channelplugin.Capabilities)) {
	if f.Request == nil || f.Request.URL == nil {
		return
	}
	for _, it := range g.host.registered() {
		channel, ok := it.parts.Messages.(gatewayChannel)
		if !ok || it.PackageFormat != "aicat" {
			continue
		}
		caps := channel.GatewayCapabilities()
		if matchesGatewayHosts(append(append([]string{}, caps.Hosts...), caps.Intercepts...), f.Request.URL) {
			visit(it, channel, caps)
		}
	}
}

func makeGatewayEvent(f *proxy.Flow, stage string) gatewayEvent {
	return gatewayEvent{Stage: stage, URL: f.Request.URL.String(), Method: f.Request.Method, RequestHeaders: f.Request.Header.Clone(), Time: time.Now().UTC(), Connection: f.Id.String()}
}

func localGatewayResponse(f *proxy.Flow, status int) {
	f.Response = &proxy.Response{StatusCode: status, Header: http.Header{"Cache-Control": {"no-store"}, "X-Content-Type-Options": {"nosniff"}}, Body: []byte{}}
}

func applyGatewayResponse(f *proxy.Flow, result gatewayResult) bool {
	if result.Response == nil || result.Response.Status < 100 || result.Response.Status > 599 || len(result.Response.Body) > 2<<20 {
		return false
	}
	headers := result.Response.Headers.Clone()
	if headers == nil {
		headers = http.Header{}
	}
	headers.Set("Content-Length", strconv.Itoa(len(result.Response.Body)))
	f.Response = &proxy.Response{StatusCode: result.Response.Status, Header: headers, Body: result.Response.Body}
	return true
}

func (g nativeGateway) Requestheaders(f *proxy.Flow) {
	g.channels(f, func(it *item, channel gatewayChannel, caps channelplugin.Capabilities) {
		reserved := false
		for _, prefix := range caps.Routes {
			reserved = reserved || strings.HasPrefix(f.Request.URL.Path, prefix)
		}
		if !it.on.Load() {
			if reserved {
				localGatewayResponse(f, http.StatusServiceUnavailable)
			}
			return
		}
		if !caps.Page {
			return
		}
		event := makeGatewayEvent(f, "request")
		if reserved {
			raw := f.Request.Raw()
			if raw.ContentLength > 64<<10 {
				localGatewayResponse(f, http.StatusRequestEntityTooLarge)
				return
			}
			var reader io.Reader = raw.Body
			if reader == nil {
				reader = bytes.NewReader(f.Request.Body)
			}
			body, err := io.ReadAll(io.LimitReader(reader, 64<<10+1))
			if err != nil || len(body) > 64<<10 {
				localGatewayResponse(f, http.StatusRequestEntityTooLarge)
				return
			}
			event.Body = body
		}
		raw, err := channel.Gateway(g.host.ctx, event)
		var result gatewayResult
		if err != nil || json.Unmarshal(raw, &result) != nil {
			if reserved {
				localGatewayResponse(f, http.StatusBadGateway)
			}
			return
		}
		if result.RequestHeaders != nil {
			f.Request.Header = result.RequestHeaders
		}
		if !applyGatewayResponse(f, result) && reserved {
			localGatewayResponse(f, http.StatusNotFound)
		}
	})
}

func (g nativeGateway) Response(f *proxy.Flow) {
	if f.Response == nil || f.Stream || len(f.Response.Body) > 2<<20 {
		return
	}
	g.channels(f, func(it *item, channel gatewayChannel, caps channelplugin.Capabilities) {
		if !it.on.Load() || !caps.Page {
			return
		}
		for _, route := range caps.Routes {
			if strings.HasPrefix(f.Request.URL.Path, route) {
				return
			}
		}
		event := makeGatewayEvent(f, "response")
		event.Status, event.ResponseHeaders, event.Body = f.Response.StatusCode, f.Response.Header.Clone(), append([]byte{}, f.Response.Body...)
		raw, err := channel.Gateway(g.host.ctx, event)
		if err != nil {
			slog.Warn("channel response observer failed", "channel", it.ID)
			return
		}
		var result gatewayResult
		if json.Unmarshal(raw, &result) == nil {
			applyGatewayResponse(f, result)
		}
	})
}

func (g nativeGateway) observe(f *proxy.Flow, stage string, message *proxy.WebSocketMessage) {
	g.channels(f, func(it *item, channel gatewayChannel, caps channelplugin.Capabilities) {
		if !it.on.Load() || !caps.WebSocket {
			return
		}
		event := makeGatewayEvent(f, stage)
		if message != nil {
			event.Time, event.FromClient, event.MessageType, event.OriginalBytes = message.Timestamp, message.FromClient, message.Type, len(message.Content)
			event.Truncated = len(message.Content) > 64<<10
			event.Body = append([]byte{}, message.Content[:min(len(message.Content), 64<<10)]...)
		}
		channel.Observe(event)
	})
}

func (g nativeGateway) WebSocketStart(f *proxy.Flow) { g.observe(f, "websocket_start", nil) }
func (g nativeGateway) WebSocketEnd(f *proxy.Flow)   { g.observe(f, "websocket_end", nil) }
func (g nativeGateway) WebSocketMessageReceived(f *proxy.Flow, m *proxy.WebSocketMessage) {
	g.observe(f, "websocket_message", m)
}
