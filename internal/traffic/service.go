package traffic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type storedBody struct {
	owner ID
	info  BodyInfo
	data  []byte
}
type Service struct {
	mu        sync.Mutex
	opts      Options
	next      ID
	nextBody  uint64
	records   map[ID]*Record
	order     []ID
	bodies    map[BodyID]*storedBody
	bodyBytes int64
	subs      map[*Subscription]struct{}
	closed    bool
	recorder  Recorder
}

func New(o Options) (*Service, error) {
	if o.MaxRecords == 0 {
		o.MaxRecords = 2000
	}
	if o.MaxBodyBytes == 0 {
		o.MaxBodyBytes = 256 << 10
	}
	if o.MaxTotalBodyBytes == 0 {
		o.MaxTotalBodyBytes = 64 << 20
	}
	if o.SubscriberBuffer == 0 {
		o.SubscriberBuffer = 256
	}
	if o.MaxRecords < 1 || o.MaxBodyBytes < 1 || o.MaxTotalBodyBytes < 1 || o.SubscriberBuffer < 1 {
		return nil, fmt.Errorf("traffic: all limits must be positive")
	}
	s := &Service{opts: o, records: make(map[ID]*Record), bodies: make(map[BodyID]*storedBody), subs: make(map[*Subscription]struct{})}
	s.recorder.s = s
	return s, nil
}
func (s *Service) Recorder() *Recorder { return &s.recorder }
func (s *Service) List(ctx context.Context, q Query) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Page{}, ErrClosed
	}
	return s.list(q), nil
}
func (s *Service) list(q Query) Page {
	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	p := Page{Items: make([]Summary, 0)}
	for i := len(s.order) - 1; i >= 0; i-- {
		id := s.order[i]
		r := s.records[id]
		if q.Before != 0 && id >= q.Before {
			continue
		}
		if q.Method != "" && !strings.EqualFold(q.Method, r.Method) {
			continue
		}
		if q.State != "" && q.State != r.State {
			continue
		}
		if q.Host != "" {
			u, _ := url.Parse(r.URL)
			if u == nil || !strings.EqualFold(u.Hostname(), q.Host) {
				continue
			}
		}
		if q.Text != "" && !strings.Contains(strings.ToLower(r.Method+" "+r.URL+" "+r.Error), strings.ToLower(q.Text)) {
			continue
		}
		if len(p.Items) == limit {
			p.NextPage = strconv.FormatUint(uint64(p.Items[len(p.Items)-1].ID), 10)
			break
		}
		p.Items = append(p.Items, r.Summary)
	}
	return p
}
func cloneRequest(r Request) Request { r.Header = r.Header.Clone(); return r }
func cloneResponse(r *Response) *Response {
	if r == nil {
		return nil
	}
	c := *r
	c.Header = r.Header.Clone()
	return &c
}
func (s *Service) Get(ctx context.Context, id ID) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Record{}, ErrClosed
	}
	r, ok := s.records[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	c := *r
	c.Executions = cloneExecutions(r.Executions)
	c.RequestReceived = cloneRequest(r.RequestReceived)
	if r.RequestSent != nil {
		v := cloneRequest(*r.RequestSent)
		c.RequestSent = &v
	}
	c.ResponseReceived = cloneResponse(r.ResponseReceived)
	c.ResponseSent = cloneResponse(r.ResponseSent)
	return c, nil
}
func (s *Service) OpenBody(ctx context.Context, id BodyID) (Body, error) {
	if err := ctx.Err(); err != nil {
		return Body{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Body{}, ErrClosed
	}
	b, ok := s.bodies[id]
	if !ok {
		return Body{}, ErrNotFound
	}
	return Body{Info: b.info, Reader: io.NopCloser(bytes.NewReader(bytes.Clone(b.data)))}, nil
}

// Clear never resets IDs, including while evicted requests are still forwarding.
func (s *Service) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.records = make(map[ID]*Record)
	s.order = nil
	s.bodies = make(map[BodyID]*storedBody)
	s.bodyBytes = 0
	s.notify(Change{Kind: "reset"})
	return nil
}
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	for sub := range s.subs {
		s.closeSub(sub, ErrClosed)
	}
	s.records = nil
	s.order = nil
	s.bodies = nil
	s.bodyBytes = 0
	return nil
}
func (s *Service) evict(id ID) {
	delete(s.records, id)
	for key, b := range s.bodies {
		if b.owner == id {
			s.bodyBytes -= int64(len(b.data))
			delete(s.bodies, key)
		}
	}
	s.notify(Change{Kind: "removed", ID: id})
}

func cloneExecutions(in []Execution) []Execution {
	out := append([]Execution(nil), in...)
	for i := range out {
		out[i].Logs = append([]ExecutionLog(nil), in[i].Logs...)
	}
	return out
}
