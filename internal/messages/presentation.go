package messages

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const PresentationUpdated = "org.aicat.room.presentation.updated.v1"

type PersonPresentation struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

type RoomPresentation struct {
	RoomID  string                        `json:"roomid"`
	Name    string                        `json:"name"`
	Avatar  string                        `json:"avatar"`
	Kind    string                        `json:"kind"`
	Members map[string]PersonPresentation `json:"members"`
}

func validDisplayText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func validAvatar(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && len(value) <= 4096 && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil
}

func (s *Inbox) updatePresentation(source string, raw json.RawMessage) error {
	var event struct {
		Event
		Data RoomPresentation `json:"data"`
	}
	if json.Unmarshal(raw, &event) != nil || event.SpecVersion != "1.0" || event.Type != PresentationUpdated || event.Source != source || event.ContentType != "application/json" || event.ID == "" {
		return errors.New("无效会话展示信息")
	}
	value := event.Data
	if value.RoomID == "" || !validDisplayText(value.RoomID, 256) || !validDisplayText(value.Name, 1024) || !validAvatar(value.Avatar) || len(value.Members) > 512 || (value.Kind != "" && value.Kind != "direct" && value.Kind != "group" && value.Kind != "system") {
		return errors.New("无效会话展示信息")
	}
	for id, person := range value.Members {
		if id == "" || !validDisplayText(id, 256) || !validDisplayText(person.Name, 1024) || !validAvatar(person.Avatar) {
			return errors.New("无效会话成员信息")
		}
	}
	s.rooms.logMu.Lock()
	defer s.rooms.logMu.Unlock()
	return writeJSON(filepath.Join(s.rooms.directory(source, value.RoomID), "presentation.json"), value)
}

func readPresentation(home, id string) (RoomPresentation, error) {
	var value RoomPresentation
	file := filepath.Join(home, "rooms", id, "presentation.json")
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<10 {
		return value, errors.New("会话展示信息无效")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return value, err
	}
	err = json.Unmarshal(raw, &value)
	return value, err
}
