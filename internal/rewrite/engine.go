package rewrite

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/plugins"
	"golang.org/x/net/http/httpguts"
)

type compiledRule struct {
	rule    Rule
	plugin  *plugins.Plugin
	problem string
}
type program struct {
	version string
	logMu   sync.Mutex
	recent  []Execution
	cfg     Config
	rules   []compiledRule
	plugins []plugins.Info
	slots   chan struct{}
}
type Session struct {
	program    *program
	contexts   map[string]map[string]any
	executions []Execution
	traceBytes int
}

func (p *program) session() *Session {
	if !p.cfg.Enabled {
		return nil
	}
	return &Session{program: p, contexts: make(map[string]map[string]any)}
}
func (s *Session) Revision() string {
	if s == nil {
		return ""
	}
	return s.program.version
}
func (s *Session) Executions() []Execution {
	if s == nil {
		return nil
	}
	return slices.Clone(s.executions)
}
func (s *Session) add(e Execution) {
	if len(e.Error) > 1024 {
		e.Error = e.Error[:1024]
	}
	for i := range e.Logs {
		if len(e.Logs[i].Message) > 256 {
			e.Logs[i].Message = e.Logs[i].Message[:256]
		}
	}
	if len(e.Logs) > 8 {
		e.Logs = e.Logs[:8]
	}
	s.program.logMu.Lock()
	if len(s.program.recent) == 64 {
		s.program.recent = s.program.recent[1:]
	}
	s.program.recent = append(s.program.recent, e)
	s.program.logMu.Unlock()
	b, _ := json.Marshal(e)
	if len(s.executions) >= 64 || s.traceBytes+len(b) > 16384 {
		if len(s.executions) > 0 && s.executions[len(s.executions)-1].Status != "omitted" {
			s.executions[len(s.executions)-1] = Execution{Stage: e.Stage, Status: "omitted", Error: "execution trace limit reached"}
		}
		return
	}
	s.traceBytes += len(b)
	s.executions = append(s.executions, e)
}
func (s *Session) Skip(stage string, req Request, reason string) {
	if s == nil {
		return
	}
	for _, r := range s.program.rules {
		if r.rule.Enabled && matches(r.rule.Match, req) && r.plugin != nil && r.plugin.Has(stage) {
			s.add(Execution{RuleID: r.rule.ID, PluginID: r.rule.Plugin, Stage: stage, Status: "skipped", Error: reason})
		}
	}
}
func matches(m Match, req Request) bool {
	u, e := url.Parse(req.URL)
	if e != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if len(m.Hosts) > 0 {
		ok := false
		for _, target := range m.Hosts {
			target = strings.ToLower(strings.TrimSuffix(target, "."))
			if strings.HasPrefix(target, "*.") {
				suffix := target[1:]
				ok = ok || (strings.HasSuffix(host, suffix) && host != target[2:])
			} else {
				ok = ok || host == target
			}
		}
		if !ok {
			return false
		}
	}
	if len(m.Methods) > 0 && !slices.ContainsFunc(m.Methods, func(m string) bool { return strings.EqualFold(m, req.Method) }) {
		return false
	}
	return strings.HasPrefix(u.EscapedPath(), m.PathPrefix)
}

func (s *Session) Apply(parent context.Context, stage string, e Exchange) Exchange {
	if s == nil {
		return e
	}
	ctx, cancel := context.WithTimeout(parent, s.program.cfg.Timeout)
	defer cancel()
	select {
	case s.program.slots <- struct{}{}:
		defer func() { <-s.program.slots }()
	case <-ctx.Done():
		s.add(Execution{Stage: stage, Status: "error", Error: ctx.Err().Error()})
		return e
	}
	for _, r := range s.program.rules {
		if !r.rule.Enabled || !matches(r.rule.Match, e.Request) {
			continue
		}
		if r.plugin == nil {
			s.add(Execution{RuleID: r.rule.ID, PluginID: r.rule.Plugin, Stage: stage, Status: "error", Error: r.problem})
			continue
		}
		if !r.plugin.Has(stage) {
			continue
		}
		call := Execution{RuleID: r.rule.ID, PluginID: r.rule.Plugin, Stage: stage, Status: "success"}
		started := time.Now()
		next, contextValue, logs, err := s.applyRule(ctx, r, stage, e)
		call.Duration = time.Since(started)
		call.Logs = logs
		if err != nil {
			call.Status = "error"
			call.Error = err.Error()
		} else {
			e = next
			s.contexts[r.rule.ID] = contextValue
		}
		s.add(call)
		if ctx.Err() != nil {
			break
		}
		if e.Synthetic && (stage == plugins.RequestBody || stage == plugins.RequestHeaders) {
			break
		}
	}
	// HTTP forbids a body for these final responses, even when an upstream
	// representation existed before a status rewrite.
	if stage == plugins.ResponseBody && e.Response != nil && (e.Request.Method == http.MethodHead || e.Response.StatusCode == 204 || e.Response.StatusCode == 304) {
		if len(e.Response.Body) > 0 {
			e.Response = &Response{StatusCode: e.Response.StatusCode, Header: e.Response.Header.Clone(), Body: nil}
			normalizeBody(e.Response.Header, nil)
		}
	}
	return e
}

func (s *Session) applyRule(ctx context.Context, r compiledRule, stage string, e Exchange) (Exchange, map[string]any, []plugins.CallLog, error) {
	bodyStage := stage == plugins.RequestBody || stage == plugins.ResponseBody
	responseStage := stage == plugins.ResponseHeaders || stage == plugins.ResponseBody
	header := e.Request.Header
	raw := e.Request.Body
	in := plugins.Message{URL: e.Request.URL, Method: e.Request.Method, Context: s.contexts[r.rule.ID]}
	if in.Context == nil {
		in.Context = map[string]any{}
	}
	if responseStage {
		if e.Response == nil {
			return e, nil, nil, errors.New("missing response")
		}
		header = e.Response.Header
		raw = e.Response.Body
		in.StatusCode = e.Response.StatusCode
	}
	in.Headers = lowerHeaders(header)
	var decoded []byte
	if bodyStage {
		var err error
		decoded, err = decodeBody(header.Get("Content-Encoding"), raw, s.program.cfg.MaxBodyBytes)
		if err != nil {
			return e, nil, nil, fmt.Errorf("body unavailable: %w", err)
		}
		value := base64.StdEncoding.EncodeToString(decoded)
		in.Body = &value
	}
	output, logs, err := r.plugin.Call(ctx, stage, in, r.rule.Settings, plugins.Limits{MemoryBytes: s.program.cfg.MemoryBytes, ResultBytes: s.program.cfg.MaxBodyBytes*2 + 128<<10})
	if err != nil {
		return e, nil, logs, err
	}
	stateBytes, err := json.Marshal(output.Context)
	if err != nil || len(stateBytes) > 16384 {
		return e, nil, logs, errors.New("plugin context exceeds 16 KiB or is invalid")
	}
	// Decode again into private Go data: no runtime object survives this call.
	contextValue := map[string]any{}
	if err = json.Unmarshal(stateBytes, &contextValue); err != nil {
		return e, nil, logs, err
	}
	if output.Response != nil {
		if stage != plugins.RequestBody {
			return e, nil, logs, errors.New("custom responses are only allowed in onRequest")
		}
		response, err := s.customResponse(output.Response, e.Request.Method)
		if err != nil {
			return e, nil, logs, err
		}
		next := e
		next.Response = response
		next.Synthetic = true
		return next, contextValue, logs, nil
	}
	headers, err := validateHeaders(output.Headers, in.Headers)
	if err != nil {
		return e, nil, logs, err
	}
	if responseStage {
		if output.URL != in.URL || output.Method != in.Method {
			return e, nil, logs, errors.New("response hooks cannot rewrite request metadata")
		}
		if output.StatusCode < 200 || output.StatusCode > 599 {
			return e, nil, logs, errors.New("invalid final response status (200..599)")
		}
	} else {
		if !httpguts.ValidHeaderFieldName(output.Method) {
			return e, nil, logs, errors.New("invalid HTTP method")
		}
		u, err := url.Parse(output.URL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
			return e, nil, logs, errors.New("invalid absolute HTTP(S) URL")
		}
		if output.StatusCode != 0 {
			return e, nil, logs, errors.New("request hooks cannot set statusCode")
		}
	}
	newBody := raw
	if bodyStage {
		if output.Body == nil {
			return e, nil, logs, errors.New("body hook must return its complete base64 body")
		}
		b, err := base64.StdEncoding.DecodeString(*output.Body)
		if err != nil {
			return e, nil, logs, errors.New("invalid base64 body")
		}
		if int64(len(b)) > s.program.cfg.MaxBodyBytes {
			return e, nil, logs, errors.New("rewritten body exceeds limit")
		}
		if !bytes.Equal(b, decoded) {
			newBody = b
			normalizeBody(headers, newBody)
		}
	} else if output.Body != nil {
		return e, nil, logs, errors.New("headers hooks cannot read or replace a body")
	}
	next := e
	if responseStage {
		next.Response = &Response{StatusCode: output.StatusCode, Header: headers, Body: newBody}
	} else {
		next.Request = Request{Method: output.Method, URL: output.URL, Header: headers, Body: newBody}
	}
	return next, contextValue, logs, nil
}
func lowerHeaders(h http.Header) http.Header {
	n := make(http.Header, len(h))
	for k, v := range h {
		n[strings.ToLower(k)] = slices.Clone(v)
	}
	return n
}
func validateHeaders(next, original http.Header) (http.Header, error) {
	if next == nil {
		return nil, errors.New("headers must be an object")
	}
	out := make(http.Header, len(next))
	size := 0
	for k, v := range next {
		if k != strings.ToLower(k) || !httpguts.ValidHeaderFieldName(k) {
			return nil, errors.New("header names must be valid lower-case HTTP field names")
		}
		size += len(k)
		for _, value := range v {
			size += len(value)
			if !httpguts.ValidHeaderFieldValue(value) {
				return nil, errors.New("invalid HTTP header value")
			}
		}
		out[http.CanonicalHeaderKey(k)] = slices.Clone(v)
	}
	if len(out) > 256 || size > 65536 {
		return nil, errors.New("headers exceed limit")
	}
	for _, k := range []string{"content-length", "transfer-encoding", "content-encoding", "host", "connection", "proxy-connection", "keep-alive", "te", "trailer", "upgrade"} {
		if !slices.Equal(next[k], original[k]) {
			return nil, fmt.Errorf("%s is maintained by the host and cannot be edited directly", k)
		}
	}
	return out, nil
}
func normalizeBody(h http.Header, b []byte) {
	h.Del("Content-Encoding")
	h.Del("Transfer-Encoding")
	h.Del("Content-Md5")
	h.Del("Digest")
	h.Del("Etag")
	h.Del("Content-Range")
	h.Set("Content-Length", fmt.Sprint(len(b)))
}
func (s *Session) customResponse(m *plugins.Message, method string) (*Response, error) {
	if m.StatusCode < 200 || m.StatusCode > 599 || m.Response != nil {
		return nil, errors.New("invalid custom response")
	}
	h, err := validateHeaders(m.Headers, http.Header{})
	if err != nil {
		return nil, err
	}
	b := []byte{}
	if m.Body != nil {
		b, err = base64.StdEncoding.DecodeString(*m.Body)
		if err != nil {
			return nil, err
		}
	}
	if int64(len(b)) > s.program.cfg.MaxBodyBytes {
		return nil, errors.New("custom response exceeds body limit")
	}
	if (method == http.MethodHead || m.StatusCode == 204 || m.StatusCode == 304) && len(b) != 0 {
		return nil, errors.New("response must not contain a body")
	}
	normalizeBody(h, b)
	return &Response{StatusCode: m.StatusCode, Header: h, Body: b}, nil
}
