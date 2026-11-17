package messages

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"
)

type MCPSender func(context.Context, string, Event) (Receipt, error)

func CheckRoomBinding(channel, source string, event Event) error {
	if event.Subject != "" && event.Subject != roomID(channel, source, event.Data.RoomID) {
		return errors.New("发送绑定的 Room 或账号已发生变化")
	}
	return nil
}

func Deliver(ctx context.Context, home, channel, source string, event Event, send MCPSender) (Receipt, error) {
	m := event.Data
	if event.SpecVersion != "1.0" || event.Type != SendRequested || event.ContentType != "application/json" || (event.Source != "aicat-agent://mcp" && event.Source != "aicat-ui://desktop") || event.ID == "" || len(event.ID) > 256 || m.Action != "send" || m.Type != "text" || strings.TrimSpace(m.Text.Content) == "" || len(m.Text.Content) > 256<<10 || !utf8.ValidString(m.Text.Content) || m.ID != "" || m.From != "" || m.Time != nil || m.Schedule != nil || m.ContextKey != "" || m.Self || len(m.ReplyTo) > 256 || m.RoomID == "" || len(m.RoomID) > 256 {
		return Receipt{}, errors.New("无效外发请求")
	}
	room := roomID(channel, source, m.RoomID)
	store := newRoomStore(home, channel)
	if _, err := store.readSettings(room); err != nil {
		return Receipt{}, err
	}
	if m.ReplyTo != "" {
		if _, exists, err := (&Inbox{rooms: store}).findMessage(ctx, source, m.RoomID, m.ReplyTo); err != nil || !exists {
			slog.Info("room message reply target ignored", "channel", channel, "room", room, "request", event.ID, "replyTo", m.ReplyTo)
			event.Data.ReplyTo = ""
		}
	}
	receipt, sendErr := send(ctx, channel, event)
	if sendErr != nil || receipt.Status != "sent" {
		if receipt.Status != "failed" {
			receipt.Status = "unknown"
		}
		if sendErr != nil {
			receipt.Error = sendErr.Error()
		}
	}
	receipt.ID, receipt.Source = event.ID, event.Source
	attrs := []any{"channel", channel, "room", room, "request", event.ID, "source", event.Source, "replyTo", m.ReplyTo, "status", receipt.Status}
	if receipt.Status == "sent" {
		slog.Info("room message sent", attrs...)
	} else {
		slog.Warn("room message not confirmed", append(attrs, "error", receipt.Error)...)
	}
	return receipt, nil
}
