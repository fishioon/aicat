package messages

import (
	"path/filepath"
	"sync"
	"time"
)

type roomStore struct {
	home, channel string
	logMu         sync.Mutex
	tasks         *taskStore
}

func newRoomStore(home, channel string) *roomStore {
	return &roomStore{home: home, channel: channel, tasks: newTaskStore(home, channel)}
}

func (s *roomStore) directory(source, room string) string {
	return filepath.Join(s.home, "rooms", digest([]byte(s.channel+"\x00"+source+"\x00"+room)))
}

func (s *roomStore) append(source, botID string, event Event, status string) (Record, error) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	dir := s.directory(source, event.Data.RoomID)
	var record Record
	err := appendJSONL(filepath.Join(dir, "messages.jsonl"), func(offset int64) any {
		record = Record{ID: newID(), RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Event: event, Status: status, Offset: offset}
		return record
	})
	if err != nil {
		return Record{}, err
	}
	if event.Type == Received || status == "sent" {
		message := event.Data
		if event.Type == SendRequested {
			message.ID, message.From, message.ToList = event.ID, botID, []string{message.RoomID}
		}
		err = appendJSONL(filepath.Join(dir, "context", "messages.log"), func(int64) any { return message })
	}
	return record, err
}
