package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/listenaddr"
)

// ErrKeepListener asks the switch to retain the candidate when a dependent
// setting could not be rolled back. It must stay reachable for recovery.
var ErrKeepListener = errors.New("listener retained for recovery")

type runningListener struct {
	listener *listener
	done     chan struct{}
	err      error
}

type rebind struct {
	ctx    context.Context
	addr   string
	commit func() error
	result chan error
}

// Gateway keeps a stable identity while replacing single-use proxy instances.
// Run owns listener transitions; callers never swap the gateway pointer.
type Gateway struct {
	mu          sync.Mutex
	options     Options
	initial     *listener
	current     *runningListener
	retained    []*runningListener
	ready, done chan struct{}
	requests    chan rebind
	cancel      context.CancelFunc
	ran         bool
	problem     string
}

func New(o Options) (*Gateway, error) {
	o.Addons = slices.Clone(o.Addons)
	initial, err := newListener(o)
	if err != nil {
		return nil, err
	}
	return &Gateway{options: o, initial: initial, ready: make(chan struct{}), done: make(chan struct{}), requests: make(chan rebind)}, nil
}
func (g *Gateway) Ready() <-chan struct{} { return g.ready }
func (g *Gateway) Status() Status {
	g.mu.Lock()
	defer g.mu.Unlock()
	l := g.initial
	if g.current != nil {
		l = g.current.listener
	}
	s := l.Status()
	if g.problem != "" {
		s.Error = g.problem
	}
	return s
}
func (g *Gateway) ResetConnections() {
	g.mu.Lock()
	all := append([]*runningListener(nil), g.retained...)
	if g.current != nil {
		all = append(all, g.current)
	}
	g.mu.Unlock()
	for _, s := range all {
		s.listener.ResetConnections()
	}
}
func (g *Gateway) setCurrent(s *runningListener, err error) {
	g.mu.Lock()
	g.current = s
	g.problem = ""
	if err != nil {
		g.problem = err.Error()
	}
	g.mu.Unlock()
}

func startListener(l *listener) *runningListener {
	s := &runningListener{listener: l, done: make(chan struct{})}
	go func() { s.err = l.Run(context.Background()); close(s.done) }()
	return s
}
func awaitListener(ctx context.Context, s *runningListener) error {
	select {
	case <-s.listener.Ready():
		return nil
	case <-s.done:
		if s.err != nil {
			return s.err
		}
		return errors.New("listener stopped before readiness")
	case <-ctx.Done():
		return ctx.Err()
	}
}
func stopListener(s *runningListener) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := s.listener.Shutdown(ctx)
	select {
	case <-s.done:
		return errors.Join(err, s.err)
	case <-ctx.Done():
		return errors.Join(err, ctx.Err())
	}
}
func (g *Gateway) create(addr string) (*runningListener, error) {
	o := g.options
	o.Addr = addr
	l, err := newListener(o)
	if err != nil {
		return nil, err
	}
	return startListener(l), nil
}

func (g *Gateway) Run(ctx context.Context) (result error) {
	g.mu.Lock()
	if g.ran {
		g.mu.Unlock()
		return errors.New("gateway: instance can run only once")
	}
	g.ran = true
	runCtx, cancel := context.WithCancel(ctx)
	g.cancel = cancel
	g.mu.Unlock()
	defer cancel()
	defer close(g.done)
	active := startListener(g.initial)
	g.setCurrent(active, nil)
	defer func() {
		result = errors.Join(result, stopListener(active))
		for _, s := range g.retained {
			result = errors.Join(result, stopListener(s))
		}
	}()
	startup, stop := context.WithTimeout(runCtx, 10*time.Second)
	err := awaitListener(startup, active)
	stop()
	if err != nil {
		return err
	}
	close(g.ready)
	for {
		select {
		case <-runCtx.Done():
			return nil
		case <-active.done:
			return active.err
		case req := <-g.requests:
			applyCtx, finish := context.WithCancel(req.ctx)
			stopCancel := context.AfterFunc(runCtx, finish)
			active, err = g.replace(applyCtx, active, req.addr, req.commit)
			stopCancel()
			finish()
			g.setCurrent(active, err)
			req.result <- err
		}
	}
}

func (g *Gateway) replace(ctx context.Context, old *runningListener, addr string, commit func() error) (*runningListener, error) {
	oldAddr := old.listener.Status().Addr
	if addr == oldAddr {
		return old, nil
	}
	if err := listenaddr.Validate(addr); err != nil {
		return old, err
	}
	if err := ctx.Err(); err != nil {
		return old, err
	}
	stoppedOld := listenaddr.Overlaps(oldAddr, addr)
	if stoppedOld {
		_ = stopListener(old)
	}
	restore := func(cause error) (*runningListener, error) {
		if !stoppedOld {
			return old, cause
		}
		back, err := g.create(oldAddr)
		if err == nil {
			ready, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = awaitListener(ready, back)
			cancel()
		}
		if err != nil {
			if back != nil {
				_ = stopListener(back)
			}
			return old, errors.Join(cause, fmt.Errorf("restore previous listener: %w", err))
		}
		return back, cause
	}
	candidate, err := g.create(addr)
	if err == nil {
		ready, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = awaitListener(ready, candidate)
		cancel()
	}
	if err != nil {
		if candidate != nil {
			_ = stopListener(candidate)
		}
		return restore(err)
	}
	if err = ctx.Err(); err == nil && commit != nil {
		err = commit()
	}
	if err != nil {
		if errors.Is(err, ErrKeepListener) {
			if !stoppedOld {
				g.mu.Lock()
				g.retained = append(g.retained, old)
				g.mu.Unlock()
			}
			return candidate, err
		}
		_ = stopListener(candidate)
		return restore(err)
	}
	if !stoppedOld {
		_ = stopListener(old)
	}
	return candidate, nil
}

func (g *Gateway) Rebind(ctx context.Context, addr string, commit func() error) error {
	req := rebind{ctx: ctx, addr: addr, commit: commit, result: make(chan error, 1)}
	select {
	case g.requests <- req:
	case <-g.done:
		return errors.New("gateway is stopped")
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-req.result:
		return err
	case <-g.done:
		return errors.New("gateway is stopped")
	}
}
func (g *Gateway) Shutdown(ctx context.Context) error {
	g.mu.Lock()
	cancel := g.cancel
	g.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-g.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
