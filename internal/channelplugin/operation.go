package channelplugin

import (
	"context"
	"errors"
	"sync"
	"time"
)

type Operation struct {
	Action      string `json:"action"`
	Status      string `json:"status"`
	Stage       string `json:"stage"`
	StartedAt   string `json:"startedAt"`
	FinishedAt  string `json:"finishedAt"`
	Error       string `json:"error"`
	Cancellable bool   `json:"cancellable"`
}

type operationState struct {
	mu      sync.Mutex
	values  map[string]Operation
	cancel  map[string]context.CancelFunc
	changed func()
}

func (s *Store) OnChange(fn func()) {
	s.operations.mu.Lock()
	s.operations.changed = fn
	s.operations.mu.Unlock()
}

func (s *Store) Operation(id string) Operation {
	s.operations.mu.Lock()
	defer s.operations.mu.Unlock()
	return s.operations.values[id]
}

func (s *Store) changeOperation(id string, update func(*Operation)) {
	s.operations.mu.Lock()
	value := s.operations.values[id]
	update(&value)
	s.operations.values[id] = value
	changed := s.operations.changed
	s.operations.mu.Unlock()
	if changed != nil {
		changed()
	}
}

func (s *Store) stage(id, stage string) {
	s.changeOperation(id, func(value *Operation) {
		if value.Stage == "cancelling" {
			return
		}
		value.Stage = stage
		value.Cancellable = stage != "activating" && stage != "removing"
	})
}

func (s *Store) activate(ctx context.Context, id string) error {
	s.operations.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.operations.mu.Unlock()
		return err
	}
	value := s.operations.values[id]
	value.Stage = "activating"
	value.Cancellable = false
	s.operations.values[id] = value
	changed := s.operations.changed
	s.operations.mu.Unlock()
	if changed != nil {
		changed()
	}
	return nil
}

func (s *Store) Cancel(id string) error {
	s.operations.mu.Lock()
	value := s.operations.values[id]
	cancel := s.operations.cancel[id]
	if value.Status != "running" || !value.Cancellable || cancel == nil {
		s.operations.mu.Unlock()
		return errors.New("当前安装不能取消")
	}
	value.Cancellable = false
	value.Stage = "cancelling"
	s.operations.values[id] = value
	changed := s.operations.changed
	cancel()
	s.operations.mu.Unlock()
	if changed != nil {
		changed()
	}
	return nil
}

func (s *Store) operate(ctx context.Context, id, action string, run func(context.Context) error) error {
	if !s.mu.TryLock() {
		return errors.New("已有扩展安装操作正在进行")
	}
	defer s.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.operations.mu.Lock()
	if s.operations.values == nil {
		s.operations.values = map[string]Operation{}
		s.operations.cancel = map[string]context.CancelFunc{}
	}
	s.operations.values[id] = Operation{Action: action, Status: "running", Stage: "preparing", StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Cancellable: action != "uninstall"}
	s.operations.cancel[id] = cancel
	changed := s.operations.changed
	s.operations.mu.Unlock()
	if changed != nil {
		changed()
	}
	err := run(ctx)
	s.changeOperation(id, func(value *Operation) {
		value.Cancellable = false
		value.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		value.Status = "completed"
		if err != nil {
			value.Status = "failed"
			value.Error = err.Error()
			if errors.Is(err, context.Canceled) {
				value.Status = "cancelled"
				value.Error = ""
			}
		}
	})
	s.operations.mu.Lock()
	delete(s.operations.cancel, id)
	s.operations.mu.Unlock()
	return err
}
