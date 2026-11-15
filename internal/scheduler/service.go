package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/config"
	"github.com/robfig/cron/v3"
)

type SetInput struct {
	RoomID   string              `json:"room_id"`
	ID       string              `json:"id"`
	Schedule config.ScheduleSpec `json:"schedule"`
}

type RoomInput struct {
	RoomID string `json:"room_id"`
}

type DeleteInput struct {
	RoomID string `json:"room_id"`
	ID     string `json:"id"`
}

type Occurrence struct {
	ScheduleID   string `json:"schedule_id"`
	Name         string `json:"name"`
	Prompt       string `json:"prompt"`
	ScheduledFor string `json:"scheduled_for"`
}

type View struct {
	SetInput
	NextRun *time.Time `json:"next_run,omitempty"`
}

type registration struct {
	cronID cron.EntryID
	timer  *time.Timer
}

type Service struct {
	home        string
	check       func(context.Context, string) error
	deliver     func(context.Context, string, Occurrence) error
	mu          sync.Mutex
	engine      *cron.Cron
	jobs        map[string]*registration
	definitions map[string]config.Schedule
	ctx         context.Context
	closed      bool
}

func Open(home string, check func(context.Context, string) error, deliver func(context.Context, string, Occurrence) error) (*Service, error) {
	cfg, err := config.Load(home)
	if err != nil {
		return nil, err
	}
	if cfg.Schedules == nil {
		cfg.Schedules = map[string]config.Schedule{}
	}
	for k, definition := range cfg.Schedules {
		if !config.ValidRoomID(definition.RoomID) || !strings.HasPrefix(k, definition.RoomID+"/") || !config.ValidScheduleID(strings.TrimPrefix(k, definition.RoomID+"/")) {
			return nil, errors.New("无效定时任务绑定")
		}
		if _, err := definition.Spec.Next(time.Now()); err != nil {
			return nil, err
		}
	}
	return &Service{home: home, check: check, deliver: deliver, engine: cron.New(), jobs: map[string]*registration{}, definitions: cfg.Schedules}, nil
}

func key(room, id string) string { return room + "/" + id }

func (s *Service) Set(ctx context.Context, in SetInput) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !config.ValidRoomID(in.RoomID) || !config.ValidScheduleID(in.ID) {
		return View{}, errors.New("需要有效 room_id 和由小写字母、数字、下划线或连字符组成的 id（最多 64 字符）")
	}
	if err := ctx.Err(); err != nil {
		return View{}, err
	}
	if s.closed {
		return View{}, errors.New("定时服务已停止")
	}
	now := time.Now()
	next, err := in.Schedule.Next(now)
	if err != nil {
		return View{}, err
	}
	if in.Schedule.Enabled {
		if in.Schedule.At != "" && !next.After(now) {
			return View{}, errors.New("单次任务的 at 必须在未来")
		}
		if err := s.check(ctx, in.RoomID); err != nil {
			return View{}, err
		}
	}
	k := key(in.RoomID, in.ID)
	definition := config.Schedule{RoomID: in.RoomID, Spec: in.Schedule}
	if old, exists := s.definitions[k]; exists && old == definition {
		return s.view(k), nil
	}
	if _, exists := s.definitions[k]; !exists && len(s.definitions) >= 1000 {
		return View{}, errors.New("最多保存 1000 个定时任务")
	}
	if err = config.SetSchedule(s.home, k, &definition); err != nil {
		return View{}, err
	}
	s.definitions[k] = definition
	if err = s.install(k); err != nil {
		return View{}, err
	}
	return s.view(k), nil
}

func (s *Service) List(ctx context.Context, in RoomInput) ([]View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !config.ValidRoomID(in.RoomID) {
		return nil, errors.New("无效 room_id")
	}
	result := []View{}
	for k, definition := range s.definitions {
		if definition.RoomID == in.RoomID {
			result = append(result, s.view(k))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (s *Service) Delete(ctx context.Context, in DeleteInput) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !config.ValidRoomID(in.RoomID) || !config.ValidScheduleID(in.ID) {
		return nil, errors.New("无效 room_id 或定时任务 id")
	}
	k := key(in.RoomID, in.ID)
	if err := config.SetSchedule(s.home, k, nil); err != nil {
		return nil, err
	}
	s.remove(k)
	delete(s.definitions, k)
	return map[string]string{"id": in.ID, "status": "deleted"}, nil
}

func (s *Service) view(k string) View {
	definition := s.definitions[k]
	view := View{SetInput: SetInput{RoomID: definition.RoomID, ID: strings.TrimPrefix(k, definition.RoomID+"/"), Schedule: definition.Spec}}
	if job := s.jobs[k]; job != nil {
		var next time.Time
		if job.timer != nil {
			next, _ = time.Parse(time.RFC3339Nano, definition.Spec.At)
		} else {
			next = s.engine.Entry(job.cronID).Next.UTC()
		}
		if !next.IsZero() {
			view.NextRun = &next
		}
	}
	return view
}

func (s *Service) remove(k string) {
	if job := s.jobs[k]; job != nil {
		if job.timer != nil {
			job.timer.Stop()
		} else {
			s.engine.Remove(job.cronID)
		}
		delete(s.jobs, k)
	}
}

func (s *Service) install(k string) error {
	s.remove(k)
	definition := s.definitions[k]
	if s.ctx == nil || !definition.Spec.Enabled {
		return nil
	}
	job := &registration{}
	if definition.Spec.At != "" {
		at, _ := time.Parse(time.RFC3339Nano, definition.Spec.At)
		if !at.After(time.Now()) {
			return nil
		}
		job.timer = time.AfterFunc(time.Until(at), func() { s.fire(k, job) })
	} else {
		id, err := s.engine.AddFunc("CRON_TZ="+definition.Spec.Timezone+" "+definition.Spec.Cron, func() { s.fire(k, job) })
		if err != nil {
			return err
		}
		job.cronID = id
	}
	s.jobs[k] = job
	return nil
}

func (s *Service) Run(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	for k := range s.definitions {
		if err := s.install(k); err != nil {
			slog.Error("schedule registration failed", "schedule", k, "error", err)
		}
	}
	s.engine.Start()
	s.mu.Unlock()
	<-ctx.Done()
	s.mu.Lock()
	s.closed = true
	for k := range s.jobs {
		s.remove(k)
	}
	s.mu.Unlock()
	<-s.engine.Stop().Done()
}

func (s *Service) fire(k string, job *registration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil || s.jobs[k] != job {
		return
	}
	definition := s.definitions[k]
	var scheduledFor time.Time
	if definition.Spec.At != "" {
		scheduledFor, _ = time.Parse(time.RFC3339Nano, definition.Spec.At)
		s.remove(k)
		definition.Spec.Enabled = false
		if err := config.SetSchedule(s.home, k, &definition); err != nil {
			slog.Error("one-shot schedule consume failed", "schedule", k, "error", err)
			return
		}
		s.definitions[k] = definition
	} else {
		scheduledFor = s.engine.Entry(job.cronID).Prev
	}
	occurrence := Occurrence{ScheduleID: strings.TrimPrefix(k, definition.RoomID+"/"), Name: definition.Spec.Name, Prompt: definition.Spec.Prompt, ScheduledFor: scheduledFor.UTC().Format(time.RFC3339Nano)}
	if err := s.deliver(s.ctx, definition.RoomID, occurrence); err != nil {
		slog.Warn("scheduled message skipped", "schedule", k, "error", err)
	}
}
