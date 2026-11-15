package messages

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

func (s *Inbox) ReceiveAndDispatch(source string, raw json.RawMessage, d *Dispatcher) (Record, error) {
	s.ingressMu.Lock()
	defer s.ingressMu.Unlock()
	var incoming Event
	if len(raw) > 16<<20 || json.Unmarshal(raw, &incoming) != nil {
		return Record{}, errors.New("无效入站消息")
	}
	if incoming.Source != source || incoming.Type != Received || incoming.SpecVersion != "1.0" || incoming.ContentType != "application/json" || incoming.ID == "" || incoming.Data.ID == "" || incoming.Data.RoomID == "" {
		return Record{}, errors.New("无效入站消息")
	}
	if incoming.Time != "" {
		if _, err := time.Parse(time.RFC3339Nano, incoming.Time); err != nil {
			return Record{}, errors.New("无效消息时间")
		}
	}
	if incoming.Data.Time != nil && (*incoming.Data.Time < 0 || *incoming.Data.Time > 253402300799999) {
		return Record{}, errors.New("无效消息时间")
	}
	rows, err := s.Read(source, incoming.Data.RoomID, 200)
	if err != nil {
		return Record{}, err
	}
	for _, row := range rows {
		if row.ID == incoming.Data.ID {
			if incoming.Data.Time == nil {
				incoming.Data.Time = row.Time
			}
			candidateData := incoming.Data
			candidateData.Historical = row.Historical
			if row.Time == nil {
				candidateData.Time = nil
			}
			if row.RoomType == "" || row.RoomType == "unknown" {
				candidateData.RoomType = row.RoomType
			}
			previous, _ := json.Marshal(row)
			candidate, _ := json.Marshal(candidateData)
			if string(previous) != string(candidate) {
				return Record{}, errors.New("入站消息 ID 内容冲突")
			}
			return Record{ID: incoming.ID, Event: incoming, Status: "duplicate"}, nil
		}
	}
	if incoming.Data.Time == nil {
		stamp := time.Now().UTC()
		if incoming.Time != "" {
			var err error
			stamp, err = time.Parse(time.RFC3339Nano, incoming.Time)
			if err != nil {
				return Record{}, errors.New("无效消息时间")
			}
		}
		value := stamp.UnixMilli()
		incoming.Data.Time = &value
		incoming.Time = stamp.Format(time.RFC3339Nano)
	}
	raw, err = json.Marshal(incoming)
	if err != nil {
		return Record{}, err
	}
	record, err := s.Receive(source, raw)
	if err == nil && d != nil {
		d.Enqueue(source, record)
	}
	return record, err
}

func (s *Inbox) Read(source, room string, limit int) ([]Message, error) {
	if room == "" || len(room) > 256 {
		return nil, errors.New("无效 Room")
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	s.rooms.logMu.Lock()
	defer s.rooms.logMu.Unlock()
	f, err := os.Open(filepath.Join(s.rooms.directory(source, room), "context", "messages.log"))
	if os.IsNotExist(err) {
		return []Message{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	offset := max(int64(0), info.Size()-(2<<20))
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 2<<20)
	if offset > 0 {
		scan.Scan()
		offset += int64(len(scan.Bytes()) + 1)
	}
	result := []Message{}
	for scan.Scan() {
		offset += int64(len(scan.Bytes()) + 1)
		if offset > info.Size() {
			continue
		}
		var m Message
		if json.Unmarshal(scan.Bytes(), &m) != nil {
			continue
		}
		result = append(result, m)
		if len(result) > limit {
			result = result[1:]
		}
	}
	return result, scan.Err()
}

func (s *Inbox) Room(source, room string) (map[string]any, error) {
	rows, err := s.Read(source, room, 100)
	if err != nil {
		return nil, err
	}
	tasks := []Task{}
	for _, t := range s.Tasks(source) {
		if t.Room == room {
			tasks = append(tasks, t)
		}
	}
	return map[string]any{"roomId": room, "messages": rows, "tasks": tasks}, nil
}
