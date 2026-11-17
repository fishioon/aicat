package messages

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/fishioon/aicat/internal/config"
)

type roomStore struct {
	home, channel string
	logMu         *sync.Mutex
	tasks         *taskStore
	defaults      config.RoomSettings
	defaultsErr   error
}

var roomLogLocks sync.Map

func newRoomStore(home, channel string) *roomStore {
	lock, _ := roomLogLocks.LoadOrStore(filepath.Clean(home), &sync.Mutex{})
	defaults := config.DefaultRoomSettings()
	var defaultsErr error
	if cfg, err := config.Load(home); err == nil {
		defaults = cfg.RoomDefaults.Settings()
	} else {
		defaultsErr = err
	}
	if manifest, err := channelplugin.New(home).Manifest(channel); err == nil {
		defaults = config.ApplyRoomTemplate(defaults, manifest.Room)
	}
	return &roomStore{home: home, channel: channel, logMu: lock.(*sync.Mutex), tasks: newTaskStore(), defaults: defaults, defaultsErr: defaultsErr}
}

func (s *roomStore) applyTemplate(template *config.RoomTemplate) error {
	defaults := config.DefaultRoomSettings()
	cfg, err := config.Load(s.home)
	if err != nil {
		s.defaultsErr = err
		return err
	}
	defaults = cfg.RoomDefaults.Settings()
	s.defaults = config.ApplyRoomTemplate(defaults, template)
	s.defaultsErr = nil
	return nil
}

func (s *roomStore) directory(source, room string) string {
	return filepath.Join(s.home, "rooms", roomID(s.channel, source, room))
}

func (s *roomStore) append(source string, event Event) (Record, error) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	dir := s.directory(source, event.Data.RoomID)
	record := Record{ID: newID(), RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Event: event}
	if err := s.register(source, event.Data.RoomID, record.RecordedAt); err != nil {
		return Record{}, err
	}
	if event.Type != Received {
		return Record{}, errors.New("Room 日志只保存平台消息")
	}
	err := appendJSONL(filepath.Join(dir, "context", "messages.log"), func(offset int64) any {
		record.Offset = offset
		return event.Data
	})
	if err != nil {
		return Record{}, err
	}
	legacy := filepath.Join(dir, "messages.jsonl")
	if info, err := os.Lstat(legacy); err == nil && info.Mode().IsRegular() {
		if err = os.Remove(legacy); err != nil {
			return Record{}, err
		}
		if err = syncDir(dir); err != nil {
			return Record{}, err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Record{}, err
	}
	return record, nil
}
