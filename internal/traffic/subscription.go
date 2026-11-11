package traffic

import "context"

type Subscription struct {
	Changes <-chan Change
	ch      chan Change
	done    chan struct{}
	s       *Service
	err     error // protected by service mutex
	closed  bool
}

func (s *Service) Watch(ctx context.Context, q Query) (Page, *Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Page{}, nil, err
	}
	if s.closed {
		return Page{}, nil, ErrClosed
	}
	ch := make(chan Change, s.opts.SubscriberBuffer)
	sub := &Subscription{Changes: ch, ch: ch, done: make(chan struct{}), s: s}
	s.subs[sub] = struct{}{}
	page := s.list(q)
	go func() {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.closeSub(sub, ctx.Err())
			s.mu.Unlock()
		case <-sub.done:
		}
	}()
	return page, sub, nil
}
func (s *Subscription) Err() error { s.s.mu.Lock(); defer s.s.mu.Unlock(); return s.err }
func (s *Subscription) Close() error {
	s.s.mu.Lock()
	defer s.s.mu.Unlock()
	s.s.closeSub(s, nil)
	return nil
}
func (s *Service) closeSub(sub *Subscription, err error) {
	if sub.closed {
		return
	}
	sub.closed = true
	sub.err = err
	delete(s.subs, sub)
	close(sub.ch)
	close(sub.done)
}
func (s *Service) notify(c Change) {
	for sub := range s.subs {
		select {
		case sub.ch <- c:
		default:
			s.closeSub(sub, ErrSlowSubscriber)
		}
	}
}
