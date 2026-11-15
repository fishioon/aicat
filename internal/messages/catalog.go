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

type RoomSummary struct {
	RoomIdentity
	LastMessage string              `json:"lastMessage"`
	UpdatedAt   time.Time           `json:"updatedAt"`
	Settings    config.RoomSettings `json:"settings"`
	Issues      []RoomIssue         `json:"issues"`
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
	if !config.ValidRoomID(id) {
		return RoomIdentity{}, errors.New("无效 Room ID")
	}
	file := filepath.Join(home, "rooms", id, "room.json")
	info, err := os.Lstat(file)
	if err != nil {
		return RoomIdentity{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return RoomIdentity{}, errors.New("Room 来源记录无效")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return RoomIdentity{}, err
	}
	var identity RoomIdentity
	if json.Unmarshal(raw, &identity) != nil || identity.ID != id || identity.Channel == "" || identity.Source == "" || identity.Conversation == "" || roomID(identity.Channel, identity.Source, identity.Conversation) != id {
		return RoomIdentity{}, errors.New("Room 来源绑定无效")
	}
	return identity, nil
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
	if err = config.EnsureRoomSettings(s.home, id, s.defaults); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.home, "rooms", id, "room.json"), RoomIdentity{ID: id, Channel: s.channel, Source: source, Conversation: conversation, CreatedAt: timestamp})
}

func ListRooms(ctx context.Context, home string) ([]RoomSummary, error) {
	entries, err := os.ReadDir(filepath.Join(home, "rooms"))
	if os.IsNotExist(err) {
		return []RoomSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(home)
	if err != nil {
		return nil, err
	}
	result := []RoomSummary{}
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
		settings, exists := cfg.Rooms[identity.ID]
		if !exists {
			settings = config.DefaultRoomSettings()
		}
		if err = settings.Validate(); err != nil {
			return nil, fmt.Errorf("Room %s: %w", identity.ID, err)
		}
		rows, err := NewInbox(home, identity.Channel).Read(identity.Source, identity.Conversation, 1)
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
	settings, err := config.ReadRoomSettings(home, id)
	if err != nil {
		return RoomView{}, err
	}
	rows, err := NewInbox(home, identity.Channel).Read(identity.Source, identity.Conversation, 200)
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
	result := RoomSummary{RoomIdentity: identity, Settings: settings, UpdatedAt: updated, Issues: issues}
	if len(rows) == 0 {
		return result, nil
	}
	last := rows[len(rows)-1]
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
	if _, err := readRoomIdentity(home, id); err != nil {
		return err
	}
	return config.SetRoomSettings(home, id, settings)
}
