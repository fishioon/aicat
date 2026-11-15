package messages

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/fishioon/aicat/internal/config"
)

var publishMu sync.Mutex

func CheckOutbound(ctx context.Context, home, channel, source, conversation string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := roomID(channel, source, conversation)
	if _, err := readRoomIdentity(home, id); err != nil {
		return err
	}
	settings, err := config.ReadRoomSettings(home, id)
	if err != nil {
		return err
	}
	if !settings.AllowOutbound {
		return errors.New("当前 Room 未允许外部发送")
	}
	return nil
}

func CheckRoomBinding(channel, source string, event Event) error {
	if event.Subject != "" && event.Subject != roomID(channel, source, event.Data.RoomID) {
		return errors.New("回复绑定的 Room 或账号已发生变化")
	}
	return nil
}

func PublishReply(ctx context.Context, home, channel, source string, event Event) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	m := event.Data
	if event.SpecVersion != "1.0" || event.Type != ReplyPublished || event.Source != "aicat-agent://mcp" || event.ContentType != "application/json" || event.ID == "" || len(event.ID) > 256 || m.Action != "publish" || m.Type != "text" || strings.TrimSpace(m.Text.Content) == "" || len(m.Text.Content) > 64<<10 || !utf8.ValidString(m.Text.Content) || m.ID != "" || m.From != "" || m.Time != nil || m.ContextKey != "" || m.AgentSnapshot != nil || m.Outgoing || m.RoomID == "" || len(m.RoomID) > 256 || len(m.ReplyTo) > 256 || m.SearchName != "" || m.ReplyID != "" || m.Delivery != nil {
		return Receipt{}, errors.New("无效 Room 回复")
	}
	publishMu.Lock()
	defer publishMu.Unlock()
	identity, err := readRoomIdentity(home, roomID(channel, source, m.RoomID))
	if err != nil {
		return Receipt{}, err
	}
	inbox := NewInbox(home, identity.Channel)
	rows, err := inbox.Read(source, m.RoomID, 200)
	if err != nil {
		return Receipt{}, err
	}
	found := m.ReplyTo == ""
	for _, row := range rows {
		if row.Outgoing && row.ID == event.ID {
			if row.Action != "publish" || row.Text.Content != m.Text.Content || row.ReplyTo != m.ReplyTo {
				return Receipt{}, errors.New("回复 ID 内容冲突")
			}
			return savedReply(event, channel), nil
		}
		if !row.Outgoing && row.ID == m.ReplyTo {
			m.ContextKey = row.ContextKey
			found = true
		}
	}
	if !found {
		reference, exists, err := inbox.findMessage(ctx, source, m.RoomID, m.ReplyTo, false)
		if err != nil {
			return Receipt{}, err
		}
		if !exists {
			return Receipt{}, errors.New("reply_to 必须引用当前 Room 已收到的消息")
		}
		m.ContextKey = reference.ContextKey
	}
	event.Data = m
	event.Time = time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = inbox.rooms.append(source, "codex", event, "saved"); err != nil {
		return Receipt{}, err
	}
	return savedReply(event, channel), nil
}

func savedReply(event Event, channel string) Receipt {
	return Receipt{Delivery: Delivery{Source: event.Source, ID: event.ID, Status: "saved", Result: &PlatformResult{ChatID: event.Data.RoomID, Channel: channel, Confirmation: "room_storage"}}}
}
