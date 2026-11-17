package messages

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"
)

type Inbox struct {
	rooms     *roomStore
	ingressMu sync.Mutex
}

func NewInbox(home, channel string) *Inbox {
	return &Inbox{rooms: newRoomStore(home, channel)}
}

func (s *Inbox) Receive(source string, raw json.RawMessage) (Record, error) {
	var event Event
	if len(raw) > 16<<20 || json.Unmarshal(raw, &event) != nil || event.SpecVersion != "1.0" || event.ContentType != "application/json" || event.Type != Received || event.Source != source || event.ID == "" || event.Data.ID == "" || event.Data.RoomID == "" || event.Data.Delivery != nil || event.Data.Schedule != nil {
		return Record{}, errors.New("无效渠道消息")
	}
	if event.Time != "" {
		if _, err := time.Parse(time.RFC3339Nano, event.Time); err != nil {
			return Record{}, errors.New("无效消息时间")
		}
	}
	if event.Data.Time != nil && (*event.Data.Time < 0 || *event.Data.Time > 253402300799999) {
		return Record{}, errors.New("无效消息时间")
	}
	if len(event.Data.Summary) > 8<<10 || !utf8.ValidString(event.Data.Summary) {
		return Record{}, errors.New("消息摘要无效或过长")
	}
	return s.rooms.append(source, "", event, "")
}

func (s *Inbox) KnownRoom(source, room string) bool {
	info, err := os.Stat(filepath.Join(s.rooms.directory(source, room), "context", "messages.log"))
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}
