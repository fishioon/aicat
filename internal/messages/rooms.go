package messages

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/config"
)

type roomStore struct {
	home, channel string
	logMu         *sync.Mutex
	tasks         *taskStore
	defaults      config.RoomSettings
}

var roomLogLocks sync.Map

func newRoomStore(home, channel string) *roomStore {
	lock, _ := roomLogLocks.LoadOrStore(filepath.Clean(home), &sync.Mutex{})
	return &roomStore{home: home, channel: channel, logMu: lock.(*sync.Mutex), tasks: newTaskStore(), defaults: config.DefaultRoomSettings()}
}

func (s *roomStore) directory(source, room string) string {
	return filepath.Join(s.home, "rooms", roomID(s.channel, source, room))
}

func (s *roomStore) append(source, botID string, event Event, status string) (Record, error) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	dir := s.directory(source, event.Data.RoomID)
	record := Record{ID: newID(), RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Event: event, Status: status}
	if err := s.register(source, event.Data.RoomID, record.RecordedAt); err != nil {
		return Record{}, err
	}
	if event.Type == Received || event.Type == ReplyPublished || event.Type == SendRequested {
		message := event.Data
		if event.Type == SendRequested || event.Type == ReplyPublished {
			message.ID, message.From, message.ToList = event.ID, botID, []string{message.RoomID}
			message.Outgoing = true
			stamp := time.Now().UnixMilli()
			message.Time = &stamp
		}
		err := appendJSONL(filepath.Join(dir, "context", "messages.log"), func(offset int64) any {
			record.Offset = offset
			return message
		})
		if err != nil {
			return Record{}, err
		}
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
