package messages

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func (s *Inbox) findMessage(ctx context.Context, source, room, id string, outgoing bool) (Message, bool, error) {
	s.rooms.logMu.Lock()
	defer s.rooms.logMu.Unlock()
	f, err := os.Open(filepath.Join(s.rooms.directory(source, room), "context", "messages.log"))
	if err != nil {
		return Message{}, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Message{}, false, err
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 16<<20)
	var found Message
	exists := false
	var offset int64
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return Message{}, false, err
		}
		offset += int64(len(scan.Bytes()) + 1)
		if offset > info.Size() {
			continue
		}
		var message Message
		if json.Unmarshal(scan.Bytes(), &message) != nil || message.ID != id || message.Outgoing != outgoing {
			continue
		}
		if exists {
			before, _ := json.Marshal(found)
			after, _ := json.Marshal(message)
			if string(before) != string(after) {
				return Message{}, false, errors.New("Room 中存在内容冲突的消息标识")
			}
		}
		found, exists = message, true
	}
	return found, exists, scan.Err()
}
