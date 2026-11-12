package traffic

import (
	"errors"
	"io"
	"net/http"
)

// Mirror records one exchange that aicat forwards itself, outside the proxy
// pipeline: a provider route or the Codex entry. A nil Mirror records
// nothing; every method accepts it.
type Mirror struct {
	r    *Recorder
	id   ID
	req  Request
	res  *Response
	done bool
}

// Mirror starts a record of a forwarded request. The caller decides what the
// request looks like in the record (its URL is where it was sent, and
// credentials are masked); a failure to record never fails the exchange.
func (r *Recorder) Mirror(req Request) *Mirror {
	if r == nil {
		return nil
	}
	m := &Mirror{r: r, req: req}
	id, err := r.Begin(Start{Request: m.req})
	if err != nil {
		return nil
	}
	m.id = id
	if m.req.Body, err = r.NewBody(id); err != nil {
		return nil
	}
	return m
}

// RequestBody records the request body as it is read.
func (m *Mirror) RequestBody(body io.ReadCloser) io.ReadCloser {
	if m == nil {
		return body
	}
	if body == nil || body == http.NoBody {
		m.requestBody(nil, true, nil)
		return body
	}
	return &capture{ReadCloser: body, write: m.requestBody}
}

func (m *Mirror) requestBody(p []byte, done bool, err error) {
	info, e := m.r.AppendBody(m.req.Body.ID, p, done, err)
	if e != nil || !done {
		return
	}
	m.req.Body = info
	_ = m.r.Update(m.id, Observation{Point: RequestReceived, Request: &m.req})
}

// Response records a response's status and headers, and returns its body,
// which records itself as it is read and finishes the record at its end.
func (m *Mirror) Response(res *http.Response) io.ReadCloser {
	if m == nil {
		return res.Body
	}
	m.res = &Response{StatusCode: res.StatusCode, Proto: res.Proto, Header: res.Header.Clone()}
	body, err := m.r.NewBody(m.id)
	if err != nil {
		return res.Body
	}
	m.res.Body = body
	_ = m.r.Update(m.id, Observation{Point: ResponseSent, Response: m.res})
	return &capture{ReadCloser: res.Body, write: m.responseBody, abandoned: io.ErrUnexpectedEOF}
}

func (m *Mirror) responseBody(p []byte, done bool, err error) {
	info, e := m.r.AppendBody(m.res.Body.ID, p, done, err)
	if e != nil || !done {
		return
	}
	m.res.Body = info
	_ = m.r.Update(m.id, Observation{Point: ResponseSent, Response: m.res})
	switch {
	case err == nil:
		m.Finish(Completed, "")
	case errors.Is(err, io.ErrUnexpectedEOF):
		m.Finish(Canceled, "the client stopped reading the response")
	default:
		m.Finish(Failed, err.Error())
	}
}

// Finish ends the record; only the first call counts.
func (m *Mirror) Finish(state State, message string) {
	if m == nil || m.done {
		return
	}
	m.done = true
	_ = m.r.Finish(m.id, Completion{State: state, Error: message})
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
