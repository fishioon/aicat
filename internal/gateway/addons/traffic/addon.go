// Package traffic adapts proxy callbacks to observations and the rewrite service.
package traffic

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fishioon/aicat/internal/plugins"
	"github.com/fishioon/aicat/internal/rewrite"
	store "github.com/fishioon/aicat/internal/traffic"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

type flowState struct {
	mu                      sync.Mutex
	id                      store.ID
	req, sent               store.Request
	res, sentRes            *store.Response
	session                 *rewrite.Session
	synthetic               bool
	err                     error
	requestEOF, responseEOF bool
}
type Addon struct {
	proxy.BaseAddon
	recording      atomic.Bool
	r              *store.Recorder
	rewrite        *rewrite.Service
	flows, clients sync.Map
	clientApps     clientAppLookup
	wg             sync.WaitGroup
}

func New(r *store.Recorder, rewriters ...*rewrite.Service) *Addon {
	a := &Addon{r: r}
	if len(rewriters) > 0 {
		a.rewrite = rewriters[0]
	}
	a.recording.Store(true)
	return a
}
func (a *Addon) Wait() { a.wg.Wait() }

// Recording reports whether new requests are being captured.
func (a *Addon) Recording() bool { return a.recording.Load() }
func (a *Addon) state(f *proxy.Flow) *flowState {
	v, ok := a.flows.Load(f)
	if !ok {
		return nil
	}
	return v.(*flowState)
}
func (a *Addon) report(err error) {
	if err != nil && !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrClosed) {
		log.Printf("traffic capture: %v", err)
	}
}
func request(f *proxy.Flow) store.Request {
	r := store.Request{Method: f.Request.Method, URL: f.Request.URL.String(), Proto: f.Request.Proto, Header: f.Request.Header.Clone()}
	if r.Method == http.MethodConnect {
		r.URL = "https://" + f.Request.URL.Host
	}
	return r
}
func response(f *proxy.Flow) *store.Response {
	if f.Response == nil {
		return nil
	}
	return &store.Response{StatusCode: f.Response.StatusCode, Header: f.Response.Header.Clone()}
}
func (a *Addon) Requestheaders(f *proxy.Flow) {
	if f.Request.Method == http.MethodConnect && f.ConnContext.Intercept {
		return
	}
	st := &flowState{req: request(f)}
	if a.rewrite != nil && f.Request.Method != http.MethodConnect && f.Request.Header.Get("Upgrade") == "" {
		st.session = a.rewrite.Begin()
	}
	if !a.recording.Load() && st.session == nil {
		return
	}
	if a.recording.Load() {
		id, err := a.begin(f.ConnContext.ClientConn, store.Start{At: f.StartTime, Request: st.req, Tunnel: f.Request.Method == http.MethodConnect, RuleRevision: st.session.Revision()})
		a.report(err)
		if err == nil {
			st.id = id
			st.req.Body, err = a.r.NewBody(id)
			a.report(err)
		}
	}
	a.flows.Store(f, st)
	st.mu.Lock()
	a.apply(f, st, plugins.RequestHeaders)
	st.sent = request(f)
	st.sent.Body = st.req.Body
	a.request(st)
	st.mu.Unlock()
	var disconnected <-chan struct{}
	if f.Request.Method == http.MethodConnect {
		if client, ok := a.clients.Load(f.ConnContext.ClientConn); ok {
			disconnected = client.(*clientState).done
		}
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		select {
		case <-f.Done():
		case <-disconnected:
		}
		a.finish(f, st)
		a.flows.Delete(f)
	}()
}
func (a *Addon) trace(st *flowState) []store.Execution {
	if st.session == nil {
		return nil
	}
	calls := st.session.Executions()
	out := make([]store.Execution, 0, len(calls))
	for _, v := range calls {
		e := store.Execution{RuleID: v.RuleID, PluginID: v.PluginID, Stage: v.Stage, Status: v.Status, Error: v.Error, Duration: v.Duration}
		for _, l := range v.Logs {
			e.Logs = append(e.Logs, store.ExecutionLog{Level: l.Level, Message: l.Message})
		}
		out = append(out, e)
	}
	return out
}
func (a *Addon) request(st *flowState) {
	if st.id == 0 {
		return
	}
	a.report(a.r.Update(st.id, store.Observation{Point: store.RequestReceived, Request: &st.req, Executions: a.trace(st), Synthetic: st.synthetic}))
	a.report(a.r.Update(st.id, store.Observation{Point: store.RequestSent, Request: &st.sent}))
}
func (a *Addon) response(st *flowState) {
	if st.id == 0 || st.res == nil {
		return
	}
	a.report(a.r.Update(st.id, store.Observation{Point: store.ResponseReceived, Response: st.res, Executions: a.trace(st), Synthetic: st.synthetic}))
	a.report(a.r.Update(st.id, store.Observation{Point: store.ResponseSent, Response: st.sentRes}))
}
func (a *Addon) append(id store.BodyID, p []byte, done bool, err error) store.BodyInfo {
	info, e := a.r.AppendBody(id, p, done, err)
	a.report(e)
	return info
}
func (a *Addon) apply(f *proxy.Flow, st *flowState, stage string) {
	if st.session == nil {
		return
	}
	e := rewrite.Exchange{Request: rewrite.Request{Method: f.Request.Method, URL: f.Request.URL.String(), Header: f.Request.Header, Body: f.Request.Body}, Synthetic: st.synthetic}
	if f.Response != nil {
		e.Response = &rewrite.Response{StatusCode: f.Response.StatusCode, Header: f.Response.Header, Body: f.Response.Body}
	}
	next := st.session.Apply(f.Request.Raw().Context(), stage, e)
	parsed, err := url.Parse(next.Request.URL)
	if err != nil {
		return
	}
	f.Request.Method, f.Request.URL, f.Request.Header, f.Request.Body = next.Request.Method, parsed, next.Request.Header, next.Request.Body
	if next.Response != nil {
		if f.Response == nil {
			f.Response = &proxy.Response{}
		}
		f.Response.StatusCode, f.Response.Header, f.Response.Body = next.Response.StatusCode, next.Response.Header, next.Response.Body
	}
	st.synthetic = next.Synthetic
}
func (a *Addon) Request(f *proxy.Flow) {
	st := a.state(f)
	if st == nil {
		return
	}
	st.mu.Lock()
	original := f.Request.Body
	a.apply(f, st, plugins.RequestBody)
	st.requestEOF = true
	if st.id != 0 {
		st.req.Body = a.append(st.req.Body.ID, original, true, nil)
		body := st.req.Body
		if !bytes.Equal(original, f.Request.Body) {
			var e error
			body, e = a.r.NewBody(st.id)
			a.report(e)
			body = a.append(body.ID, f.Request.Body, true, nil)
		}
		st.sent = request(f)
		st.sent.Body = body
		a.request(st)
	}
	synthetic := st.synthetic
	st.mu.Unlock()
	// The upstream early-reply path skips response hooks; synthetic responses
	// deliberately enter the same response pipeline here, exactly once.
	if synthetic {
		a.Responseheaders(f)
		a.Response(f)
	}
}
func (a *Addon) Responseheaders(f *proxy.Flow) {
	st := a.state(f)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.res = response(f)
	if st.res == nil {
		return
	}
	if strings.Contains(strings.ToLower(f.Response.Header.Get("Content-Type")), "text/event-stream") {
		f.Stream = true
	}
	if st.id != 0 {
		var err error
		st.res.Body, err = a.r.NewBody(st.id)
		a.report(err)
	}
	a.apply(f, st, plugins.ResponseHeaders)
	st.sentRes = response(f)
	st.sentRes.Body = st.res.Body
	a.response(st)
}
func (a *Addon) Response(f *proxy.Flow) {
	st := a.state(f)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.res == nil {
		return
	}
	original := f.Response.Body
	a.apply(f, st, plugins.ResponseBody)
	st.responseEOF = true
	if st.id == 0 {
		return
	}
	st.res.Body = a.append(st.res.Body.ID, original, true, nil)
	body := st.res.Body
	if !bytes.Equal(original, f.Response.Body) {
		var err error
		body, err = a.r.NewBody(st.id)
		a.report(err)
		body = a.append(body.ID, f.Response.Body, true, nil)
	}
	st.sentRes = response(f)
	st.sentRes.Body = body
	a.response(st)
}
func (a *Addon) StreamRequestModifier(f *proxy.Flow, r io.Reader) io.Reader {
	st := a.state(f)
	if st == nil || !f.Stream {
		return r
	}
	st.mu.Lock()
	st.session.Skip(plugins.RequestBody, rewrite.Request{Method: f.Request.Method, URL: f.Request.URL.String()}, "request is streaming or exceeds the buffer limit")
	a.request(st)
	record := st.id != 0
	st.mu.Unlock()
	if !record || r == nil {
		return r
	}
	return &reader{Reader: r, save: func(p []byte, e error) { a.capture(st, true, p, e) }}
}
func (a *Addon) StreamResponseModifier(f *proxy.Flow, r io.Reader) io.Reader {
	st := a.state(f)
	if st == nil || !f.Stream {
		return r
	}
	st.mu.Lock()
	st.session.Skip(plugins.ResponseBody, rewrite.Request{Method: f.Request.Method, URL: f.Request.URL.String()}, "response is streaming or exceeds the buffer limit")
	a.response(st)
	record := st.id != 0
	st.mu.Unlock()
	if !record || r == nil {
		return r
	}
	return &reader{Reader: r, save: func(p []byte, e error) { a.capture(st, false, p, e) }}
}
func (a *Addon) capture(st *flowState, req bool, p []byte, e error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	info := st.req.Body
	if !req {
		if st.res == nil {
			return
		}
		info = st.res.Body
	}
	failure := e
	if errors.Is(e, io.EOF) {
		failure = nil
	}
	if failure != nil {
		st.err = failure
	}
	info = a.append(info.ID, p, e != nil, failure)
	if req {
		st.req.Body, st.sent.Body = info, info
		st.requestEOF = e == io.EOF
		a.request(st)
	} else {
		st.res.Body, st.sentRes.Body = info, info
		st.responseEOF = e == io.EOF
		a.response(st)
	}
}
func (a *Addon) RequestError(f *proxy.Flow, err error) {
	if st := a.state(f); st != nil {
		st.mu.Lock()
		st.err = err
		st.mu.Unlock()
	}
}
func (a *Addon) HTTPConnectError(f *proxy.Flow, err error) { a.RequestError(f, err) }
func (a *Addon) finish(f *proxy.Flow, st *flowState) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.id == 0 {
		return
	}
	tunnel := st.req.Method == http.MethodConnect
	if tunnel {
		st.req.Body = a.append(st.req.Body.ID, nil, true, nil)
		st.sent.Body = st.req.Body
		a.request(st)
		if st.res != nil {
			st.res.Body = a.append(st.res.Body.ID, nil, true, nil)
			st.sentRes.Body = st.res.Body
			a.response(st)
		}
	}
	state := store.Completed
	message := ""
	if st.err != nil {
		state = store.Failed
		message = st.err.Error()
		if errors.Is(st.err, context.Canceled) {
			state = store.Canceled
		}
	} else if st.res == nil || (!tunnel && !st.responseEOF) {
		state = store.Failed
		message = "exchange ended before response body completed"
		if f.Request.Raw().Context().Err() != nil {
			state = store.Canceled
		}
	}
	if !tunnel {
		if !st.requestEOF {
			st.req.Body = a.append(st.req.Body.ID, nil, true, io.ErrUnexpectedEOF)
			st.sent.Body = st.req.Body
			a.request(st)
		}
		if st.res != nil && !st.responseEOF {
			st.res.Body = a.append(st.res.Body.ID, nil, true, io.ErrUnexpectedEOF)
			st.sentRes.Body = st.res.Body
			a.response(st)
		}
	}
	a.report(a.r.Finish(st.id, store.Completion{At: time.Now(), State: state, Error: message}))
}

type reader struct {
	io.Reader
	save func([]byte, error)
}

func (r *reader) Read(p []byte) (int, error)         { n, e := r.Reader.Read(p); r.save(p[:n], e); return n, e }
func (a *Addon) SetRecording(enabled bool)           { a.recording.Store(enabled) }
func (a *Addon) ClientConnected(c *proxy.ClientConn) { a.clients.Store(c, newClientState(c)) }

// End opaque CONNECT capture at client disconnect rather than waiting for the
// library's upstream half-connection; ordinary HTTP waits for Flow.Done.
func (a *Addon) ClientDisconnected(c *proxy.ClientConn) {
	if client, ok := a.clients.LoadAndDelete(c); ok {
		close(client.(*clientState).done)
	}
}
