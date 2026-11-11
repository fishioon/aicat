package traffic

import (
	"fmt"
	"strings"
	"time"
)

type Recorder struct{ s *Service }

func terminal(s State) bool { return s == Completed || s == Failed || s == Canceled }
func (r *Recorder) Begin(start Start) (ID, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	if start.At.IsZero() {
		start.At = time.Now()
	}
	s.next++
	id := s.next
	s.records[id] = &Record{Summary: Summary{ID: id, Revision: 1, StartedAt: start.At, Method: start.Request.Method, URL: start.Request.URL, State: Requesting, Tunnel: start.Tunnel, ClientApp: start.ClientApp}, RequestReceived: cloneRequest(start.Request), RuleRevision: start.RuleRevision, pendingClient: start.PendingClient}
	s.order = append(s.order, id)
	if len(s.order) > s.opts.MaxRecords {
		old := s.order[0]
		s.order = s.order[1:]
		s.evict(old)
	}
	s.notify(Change{Kind: "added", ID: id})
	return id, nil
}

// ResolveClientApps updates attribution independently of the HTTP lifecycle. A
// result can arrive after a response completes, or after its record was evicted.
func (r *Recorder) ResolveClientApps(apps map[string]ClientApp) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.records {
		app, resolved := apps[rec.pendingClient]
		if !resolved {
			continue
		}
		rec.pendingClient = ""
		if app.Name != "" {
			rec.ClientApp = app
			rec.Revision++
			s.notify(Change{Kind: "updated", ID: rec.ID})
		}
	}
}
func (r *Recorder) Update(id ID, o Observation) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	rec, ok := s.records[id]
	if !ok {
		return ErrNotFound
	}
	if terminal(rec.State) {
		return fmt.Errorf("traffic: record is terminal")
	}
	switch o.Point {
	case RequestReceived, RequestSent:
		if o.Request == nil || o.Response != nil {
			return fmt.Errorf("traffic: request observation required")
		}
		v := cloneRequest(*o.Request)
		if o.Point == RequestReceived {
			rec.RequestReceived = v
		} else {
			rec.RequestSent = &v
		}
	case ResponseReceived, ResponseSent:
		if o.Response == nil || o.Request != nil {
			return fmt.Errorf("traffic: response observation required")
		}
		v := cloneResponse(o.Response)
		if o.Point == ResponseReceived {
			rec.ResponseReceived = v
		} else {
			rec.ResponseSent = v
		}
		rec.ResponseBytes = v.Body.Observed
		rec.StatusCode = v.StatusCode
		rec.ContentType, _, _ = strings.Cut(v.Header.Get("Content-Type"), ";")
		rec.ContentType = strings.ToLower(strings.TrimSpace(rec.ContentType))
		rec.State = Responding
	default:
		return fmt.Errorf("traffic: unknown observation point")
	}
	if o.Executions != nil {
		rec.Executions = cloneExecutions(o.Executions)
	}
	if o.Synthetic {
		rec.Synthetic = true
	}
	rec.Revision++
	s.notify(Change{Kind: "updated", ID: id})
	return nil
}
func (r *Recorder) Finish(id ID, c Completion) error {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	rec, ok := s.records[id]
	if !ok {
		return ErrNotFound
	}
	if terminal(rec.State) {
		return nil
	}
	if !terminal(c.State) {
		return fmt.Errorf("traffic: completion must be terminal")
	}
	if c.At.IsZero() {
		c.At = time.Now()
	}
	rec.State = c.State
	rec.Error = c.Error
	rec.Duration = c.At.Sub(rec.StartedAt)
	rec.Revision++
	s.notify(Change{Kind: "updated", ID: id})
	return nil
}
func (r *Recorder) NewBody(owner ID) (BodyInfo, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return BodyInfo{}, ErrClosed
	}
	if s.records[owner] == nil {
		return BodyInfo{}, ErrNotFound
	}
	s.nextBody++
	id := BodyID(fmt.Sprintf("%d-%d", owner, s.nextBody))
	info := BodyInfo{ID: id, State: "pending"}
	s.bodies[id] = &storedBody{owner: owner, info: info}
	return info, nil
}

// AppendBody preserves a bounded prefix without slowing down for consumers.
func (r *Recorder) AppendBody(id BodyID, p []byte, done bool, readErr error) (BodyInfo, error) {
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return BodyInfo{}, ErrClosed
	}
	b, ok := s.bodies[id]
	if !ok {
		return BodyInfo{}, ErrNotFound
	}
	b.info.Observed += int64(len(p))
	room := min(s.opts.MaxBodyBytes-int64(len(b.data)), s.opts.MaxTotalBodyBytes-s.bodyBytes)
	n := min(int64(len(p)), max(int64(0), room))
	// Exact allocation keeps slice capacity within the capture budget.
	if n > 0 {
		data := make([]byte, len(b.data)+int(n))
		copy(data, b.data)
		copy(data[len(b.data):], p[:n])
		b.data = data
		s.bodyBytes += n
	}
	b.info.Captured = int64(len(b.data))
	if readErr != nil {
		b.info.State = "error"
	} else if b.info.Captured < b.info.Observed {
		b.info.State = "truncated"
	} else if done {
		b.info.State = "complete"
	}
	return b.info, nil
}
