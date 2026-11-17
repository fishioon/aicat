package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/config"
)

type RoomIdentity struct {
	ID           string `json:"id"`
	Channel      string `json:"channel"`
	Source       string `json:"source"`
	Conversation string `json:"conversation"`
	CreatedAt    string `json:"createdAt"`
}

const roomRecordVersion = 1

type RoomRecord struct {
	Version      int               `json:"version"`
	ID           string            `json:"id"`
	Channel      string            `json:"channel"`
	Source       string            `json:"source"`
	Conversation string            `json:"conversation"`
	CreatedAt    string            `json:"createdAt"`
	Settings     *roomOverrides    `json:"settings,omitempty"`
	Runtime      *roomRuntimeState `json:"runtime,omitempty"`
}

type roomRuntimeState struct {
	ThreadID         string `json:"threadId,omitempty"`
	InstructionsHash string `json:"instructionsHash,omitempty"`
	Model            string `json:"model,omitempty"`
}

type RoomSummary struct {
	RoomIdentity
	Presentation RoomPresentation    `json:"presentation"`
	LastSender   string              `json:"lastSender"`
	LastType     string              `json:"lastType"`
	LastOutgoing bool                `json:"lastOutgoing"`
	LastAgent    bool                `json:"lastAgent"`
	LastMessage  string              `json:"lastMessage"`
	UpdatedAt    time.Time           `json:"updatedAt"`
	Settings     config.RoomSettings `json:"settings"`
	Issues       []RoomIssue         `json:"issues"`
}

type RoomView struct {
	RoomSummary
	Messages []Message `json:"messages"`
	Tasks    []Task    `json:"tasks"`
}

var roomCatalogMu sync.Mutex

func roomID(channel, source, conversation string) string {
	return digest([]byte(channel + "\x00" + source + "\x00" + conversation))
}

func readRoomIdentity(home, id string) (RoomIdentity, error) {
	record, err := readRoomRecord(home, id)
	if err != nil {
		return RoomIdentity{}, err
	}
	return RoomIdentity{ID: record.ID, Channel: record.Channel, Source: record.Source, Conversation: record.Conversation, CreatedAt: record.CreatedAt}, nil
}

func (r RoomRecord) identity() RoomIdentity {
	return RoomIdentity{ID: r.ID, Channel: r.Channel, Source: r.Source, Conversation: r.Conversation, CreatedAt: r.CreatedAt}
}

func readRoomRecord(home, id string) (RoomRecord, error) {
	if !config.ValidRoomID(id) {
		return RoomRecord{}, errors.New("无效 Room ID")
	}
	file := filepath.Join(home, "rooms", id, "room.json")
	info, err := os.Lstat(file)
	if err != nil {
		return RoomRecord{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<10 {
		return RoomRecord{}, errors.New("Room 记录无效")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return RoomRecord{}, err
	}
	var record RoomRecord
	if json.Unmarshal(raw, &record) != nil || record.ID != id || record.Channel == "" || record.Source == "" || record.Conversation == "" || roomID(record.Channel, record.Source, record.Conversation) != id || record.Version < 0 || record.Version > roomRecordVersion {
		return RoomRecord{}, errors.New("Room 记录无效")
	}
	if record.Version == 0 {
		if err = readLegacyRoomParts(home, id, &record); err != nil {
			return RoomRecord{}, err
		}
	}
	return record, nil
}

func (s *roomStore) register(source, conversation, timestamp string) error {
	roomCatalogMu.Lock()
	defer roomCatalogMu.Unlock()
	id := roomID(s.channel, source, conversation)
	_, err := readRoomIdentity(s.home, id)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return s.writeRecord(RoomRecord{Version: roomRecordVersion, ID: id, Channel: s.channel, Source: source, Conversation: conversation, CreatedAt: timestamp})
}

func ListRooms(ctx context.Context, home string) ([]RoomSummary, error) {
	entries, err := os.ReadDir(filepath.Join(home, "rooms"))
	if os.IsNotExist(err) {
		return []RoomSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []RoomSummary{}
	stores := map[string]*roomStore{}
	for _, entry := range entries {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || !config.ValidRoomID(entry.Name()) {
			continue
		}
		identity, err := readRoomIdentity(home, entry.Name())
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("Room %s: %w", entry.Name(), err)
		}
		info, err := os.Stat(filepath.Join(home, "rooms", identity.ID, "context", "messages.log"))
		if os.IsNotExist(err) {
			info, err = os.Stat(filepath.Join(home, "rooms", identity.ID, "room.json"))
		}
		if err != nil {
			return nil, err
		}
		store := stores[identity.Channel]
		if store == nil {
			store = newRoomStore(home, identity.Channel)
			stores[identity.Channel] = store
		}
		settings, err := store.readSettings(identity.ID)
		if err != nil {
			return nil, fmt.Errorf("Room %s: %w", identity.ID, err)
		}
		rows, err := (&Inbox{rooms: store}).Read(identity.Source, identity.Conversation, 1)
		if err != nil {
			return nil, err
		}
		summary, err := summarizeRoom(home, identity, settings, info.ModTime().UTC(), rows)
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

func ReadRoom(ctx context.Context, home, id string) (RoomView, error) {
	if err := ctx.Err(); err != nil {
		return RoomView{}, err
	}
	identity, err := readRoomIdentity(home, id)
	if err != nil {
		return RoomView{}, err
	}
	store := newRoomStore(home, identity.Channel)
	settings, err := store.readSettings(id)
	if err != nil {
		return RoomView{}, err
	}
	rows, err := (&Inbox{rooms: store}).Read(identity.Source, identity.Conversation, 200)
	if err != nil {
		return RoomView{}, err
	}
	if err = attachDeliveries(home, id, rows); err != nil {
		return RoomView{}, err
	}
	info, err := os.Stat(filepath.Join(home, "rooms", id, "context", "messages.log"))
	if os.IsNotExist(err) {
		info, err = os.Stat(filepath.Join(home, "rooms", id, "room.json"))
	}
	if err != nil {
		return RoomView{}, err
	}
	summary, err := summarizeRoom(home, identity, settings, info.ModTime().UTC(), rows)
	if err != nil {
		return RoomView{}, err
	}
	return RoomView{RoomSummary: summary, Messages: rows, Tasks: []Task{}}, nil
}

func summarizeRoom(home string, identity RoomIdentity, settings config.RoomSettings, updated time.Time, rows []Message) (RoomSummary, error) {
	issues, err := deliveryIssues(home, identity.ID)
	if err != nil {
		return RoomSummary{}, err
	}
	presentation, err := readPresentation(home, identity.ID)
	if err != nil {
		return RoomSummary{}, err
	}
	result := RoomSummary{Presentation: presentation, RoomIdentity: identity, Settings: settings, UpdatedAt: updated, Issues: issues}
	if len(rows) == 0 {
		return result, nil
	}
	last := rows[len(rows)-1]
	result.LastSender, result.LastType, result.LastOutgoing = last.From, last.Type, last.Outgoing
	_, known := presentation.Members[last.From]
	result.LastAgent = last.Outgoing && (last.Action != "receive" && !known)
	if last.Time != nil {
		result.UpdatedAt = time.UnixMilli(*last.Time)
	}
	preview := last.Summary
	if preview == "" {
		preview = last.Text.Content
	}
	runes := []rune(preview)
	if len(runes) > 160 {
		preview = string(runes[:160]) + "…"
	}
	result.LastMessage = preview
	return result, nil
}

func UpdateRoomSettings(ctx context.Context, home, id string, settings config.RoomSettings) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	identity, err := readRoomIdentity(home, id)
	if err != nil {
		return err
	}
	return newRoomStore(home, identity.Channel).writeSettings(id, settings)
}
