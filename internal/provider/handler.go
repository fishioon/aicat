package provider

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/fishioon/aicat/internal/traffic"
)

// credentialHeaders are dropped from what an agent sends, so a placeholder or
// a stale key of the agent's own never reaches the vendor, and masked in the
// traffic record.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Api-Key", "Proxy-Authorization"}

const masked = "[aicat]"

// Handler forwards origin-form requests under a provider's mount to its
// endpoint with the saved key. The caller restricts it to local peers.
type Handler struct {
	store     *Store
	recorder  *traffic.Recorder
	recording func() bool
	transport http.RoundTripper
}

func NewHandler(store *Store, recorder *traffic.Recorder, recording func() bool) *Handler {
	return &Handler{store: store, recorder: recorder, recording: recording, transport: &http.Transport{
		// Outbound requests must never inherit an environment proxy pointing at us.
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 15 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	k, e, rest, saved := h.store.route(r.URL.Path)
	if k.ID == "" {
		http.NotFound(w, r)
		return
	}
	if !saved {
		http.Error(w, "aicat: provider "+k.ID+" is not connected; add it in the aicat window", http.StatusServiceUnavailable)
		return
	}
	target, err := url.Parse(e.BaseURL + rest)
	if err != nil {
		http.Error(w, "aicat: invalid provider endpoint", http.StatusBadGateway)
		return
	}
	target.RawQuery = r.URL.RawQuery
	rec := h.begin(r, target)
	r.Body = rec.tee(r.Body)
	(&httputil.ReverseProxy{
		Transport:     h.transport,
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = target
			pr.Out.Host = target.Host
			for _, name := range credentialHeaders {
				pr.Out.Header.Del(name)
			}
			// Let the transport negotiate and undo compression, so captured
			// bodies stay readable.
			pr.Out.Header.Del("Accept-Encoding")
			pr.Out.Header.Set(k.header, k.prefix+e.Key)
		},
		ModifyResponse: func(res *http.Response) error {
			res.Body = rec.response(res)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			state := traffic.Failed
			if errors.Is(err, context.Canceled) {
				state = traffic.Canceled
			}
			rec.finish(state, err.Error())
			http.Error(w, "aicat: "+err.Error(), http.StatusBadGateway)
		},
	}).ServeHTTP(w, r)
}

// exchange mirrors one forwarded request into the traffic records. A nil
// exchange records nothing; every method accepts it.
type exchange struct {
	r    *traffic.Recorder
	id   traffic.ID
	req  traffic.Request
	res  *traffic.Response
	done bool
}

func (h *Handler) begin(r *http.Request, target *url.URL) *exchange {
	if h.recorder == nil || h.recording != nil && !h.recording() {
		return nil
	}
	header := r.Header.Clone()
	for _, name := range credentialHeaders {
		if header.Get(name) != "" {
			header.Set(name, masked)
		}
	}
	x := &exchange{r: h.recorder, req: traffic.Request{Method: r.Method, URL: target.String(), Proto: r.Proto, Header: header}}
	id, err := h.recorder.Begin(traffic.Start{Request: x.req})
	if err != nil {
		return nil
	}
	x.id = id
	if x.req.Body, err = h.recorder.NewBody(id); err != nil {
		return nil
	}
	return x
}

func (x *exchange) tee(body io.ReadCloser) io.ReadCloser {
	if x == nil {
		return body
	}
	if body == nil || body == http.NoBody {
		x.requestBody(nil, true, nil)
		return body
	}
	return &capture{ReadCloser: body, write: x.requestBody}
}

func (x *exchange) requestBody(p []byte, done bool, err error) {
	info, e := x.r.AppendBody(x.req.Body.ID, p, done, err)
	if e != nil || !done {
		return
	}
	x.req.Body = info
	_ = x.r.Update(x.id, traffic.Observation{Point: traffic.RequestReceived, Request: &x.req})
}

func (x *exchange) response(res *http.Response) io.ReadCloser {
	if x == nil {
		return res.Body
	}
	x.res = &traffic.Response{StatusCode: res.StatusCode, Proto: res.Proto, Header: res.Header.Clone()}
	body, err := x.r.NewBody(x.id)
	if err != nil {
		return res.Body
	}
	x.res.Body = body
	_ = x.r.Update(x.id, traffic.Observation{Point: traffic.ResponseSent, Response: x.res})
	return &capture{ReadCloser: res.Body, write: x.responseBody, abandoned: io.ErrUnexpectedEOF}
}

func (x *exchange) responseBody(p []byte, done bool, err error) {
	info, e := x.r.AppendBody(x.res.Body.ID, p, done, err)
	if e != nil || !done {
		return
	}
	x.res.Body = info
	_ = x.r.Update(x.id, traffic.Observation{Point: traffic.ResponseSent, Response: x.res})
	switch {
	case err == nil:
		x.finish(traffic.Completed, "")
	case errors.Is(err, io.ErrUnexpectedEOF):
		x.finish(traffic.Canceled, "the client stopped reading the response")
	default:
		x.finish(traffic.Failed, err.Error())
	}
}

func (x *exchange) finish(state traffic.State, message string) {
	if x == nil || x.done {
		return
	}
	x.done = true
	_ = x.r.Finish(x.id, traffic.Completion{State: state, Error: message})
}

// capture reports the bytes read through a body, then its end exactly once.
// abandoned is the error reported when the body is closed before its end: nil
// for a request, whose reader may stop at Content-Length without seeing EOF.
type capture struct {
	io.ReadCloser
	write     func(p []byte, done bool, err error)
	abandoned error
	ended     bool
}

func (c *capture) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if c.ended {
		return n, err
	}
	switch {
	case err == nil:
		c.write(p[:n], false, nil)
	case errors.Is(err, io.EOF):
		c.ended = true
		c.write(p[:n], true, nil)
	default:
		c.ended = true
		c.write(p[:n], true, err)
	}
	return n, err
}

func (c *capture) Close() error {
	if !c.ended {
		c.ended = true
		c.write(nil, true, c.abandoned)
	}
	return c.ReadCloser.Close()
}
