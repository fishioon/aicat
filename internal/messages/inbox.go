package messages

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Inbox struct {
	rooms    *roomStore
	mu       sync.Mutex
	loaded   map[string]bool
	events   map[string]map[string]inboxEntry
	contexts map[string]map[string]string
}

type inboxEntry struct {
	record Record
	digest string
}

func NewInbox(home, channel string) *Inbox {
	return &Inbox{rooms: newRoomStore(home, channel), loaded: map[string]bool{}, events: map[string]map[string]inboxEntry{}, contexts: map[string]map[string]string{}}
}

func readLines(file string, consume func([]byte) error) error {
	f, err := os.Open(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > 16<<20 {
			return errors.New("Room 消息超过大小限制")
		}
		line = append(line, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := consume(line); err != nil {
			return err
		}
		line = line[:0]
	}

}

func (s *Inbox) Receive(source string, raw json.RawMessage) (Record, error) {
	var event Event
	if len(raw) > 16<<20 || json.Unmarshal(raw, &event) != nil || event.SpecVersion != "1.0" || event.ContentType != "application/json" || event.Type != Received || event.Source != source || event.ID == "" || event.Data.ID == "" || event.Data.RoomID == "" {
		return Record{}, errors.New("无效渠道消息")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.rooms.directory(source, event.Data.RoomID)
	if !s.loaded[dir] {
		events := map[string]inboxEntry{}
		contexts := map[string]string{}
		if err := readLines(filepath.Join(dir, "messages.jsonl"), func(line []byte) error {
			var record Record
			if err := json.Unmarshal(line, &record); err != nil {
				return err
			}
			id, signature := record.Event.ID, jsonDigest(record.Event)
			record.Event = Event{}
			events[id] = inboxEntry{record: record, digest: signature}
			return nil
		}); err != nil {
			return Record{}, err
		}
		if err := readLines(filepath.Join(dir, "context", "messages.log"), func(line []byte) error {
			var message Message
			if err := json.Unmarshal(line, &message); err != nil {
				return err
			}
			contexts[message.ID] = jsonDigest(message)
			return nil
		}); err != nil {
			return Record{}, err
		}
		s.events[dir], s.contexts[dir], s.loaded[dir] = events, contexts, true
	}
	entry, exists := s.events[dir][event.ID]
	record := entry.record
	record.Event = event
	if exists && entry.digest != jsonDigest(event) {
		return Record{}, errors.New("同一入站消息标识不能使用不同内容")
	}
	if !exists {
		if err := appendJSONL(filepath.Join(dir, "messages.jsonl"), func(offset int64) any {
			record = Record{ID: newID(), RecordedAt: time.Now().UTC().Format(time.RFC3339Nano), Offset: offset, Event: event}
			return record
		}); err != nil {
			return Record{}, err
		}
		saved := record
		saved.Event = Event{}
		s.events[dir][event.ID] = inboxEntry{record: saved, digest: jsonDigest(event)}
	}
	if prior, exists := s.contexts[dir][event.Data.ID]; exists {
		if prior != jsonDigest(event.Data) {
			return Record{}, errors.New("同一消息标识不能使用不同内容")
		}
	} else {
		if err := appendJSONL(filepath.Join(dir, "context", "messages.log"), func(int64) any { return event.Data }); err != nil {
			return Record{}, err
		}
		s.contexts[dir][event.Data.ID] = jsonDigest(event.Data)
	}
	return record, nil
}

func jsonDigest(value any) string {
	raw, _ := json.Marshal(value)
	return digest(raw)
}
