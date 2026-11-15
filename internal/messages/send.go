package messages

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (c *Channel) Send(ctx context.Context, event Event) (Receipt, error) {
	if event.SpecVersion != "1.0" || event.Type != SendRequested || event.ContentType != "application/json" ||
		event.ID == "" || len(event.ID) > 256 || (event.Source != "aicat-ui://desktop" && event.Source != "aicat-agent://mcp" && event.Source != "aicat-agent://codex") ||
		event.Data.RoomID == "" || len(event.Data.RoomID) > 256 || strings.ContainsAny(event.Data.RoomID, "\r\n") ||
		event.Data.Action != "send" || event.Data.Type != "text" || strings.TrimSpace(event.Data.Text.Content) == "" || len(event.Data.Text.Content) > 256<<10 || !utf8.ValidString(event.Data.Text.Content) ||
		event.Data.ID != "" || event.Data.From != "" || event.Data.Time != nil {
		return Receipt{}, errors.New("无效的文本发送 CloudEvent")
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.mu.Lock()
	a := c.account
	phase := c.state.Phase
	c.mu.Unlock()
	if a == nil || phase != "connected" {
		return Receipt{}, errors.New("账号尚未连接")
	}
	result := Receipt{Delivery: Delivery{Source: event.Source, ID: event.ID, Status: "pending"}}

	parts := splitText(event.Data.Text.Content, 4000)
	for i, text := range parts {
		part := event
		part.Data.Text.Content = text
		if len(parts) > 1 {
			part.ID = "part-" + digest([]byte(event.ID+"\x00"+strconv.Itoa(i)))
		}
		receipt := Delivery{Source: event.Source, ID: part.ID, Status: "pending"}

		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		raw, sendErr := a.Send(sendCtx, part)
		cancel()
		var platformResult PlatformResult
		if sendErr == nil {
			sendErr = json.Unmarshal(raw, &platformResult)
			if sendErr == nil && (platformResult.ChatID != event.Data.RoomID || platformResult.Confirmation == "" && platformResult.MessageID == "") {
				sendErr = errors.New("渠道没有返回匹配会话的发送确认")
			}
		}
		if sendErr != nil {
			receipt.Status = "unknown"
			receipt.Error = "发送结果不明，请勿重新发送"
		} else {
			receipt.Status = "sent"
			receipt.Result = &platformResult
		}
		if len(parts) > 1 {
			result.Parts = append(result.Parts, receipt)
		}
		if receipt.Status == "unknown" || i == len(parts)-1 {
			result.Status, result.Error, result.Result = receipt.Status, receipt.Error, receipt.Result
		}
		c.mu.Lock()
		c.state.Records = append(c.state.Records, Record{ID: part.ID, Event: part, Status: receipt.Status, RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		if len(c.state.Records) > 50 {
			c.state.Records = c.state.Records[len(c.state.Records)-50:]
		}
		c.mu.Unlock()
		if receipt.Status == "sent" {
			slog.Info("message sent", "channel", c.id, "request", part.ID)
		} else {
			slog.Error("message send failed", "channel", c.id, "request", part.ID)
		}
		c.changed()
		if receipt.Status != "sent" {
			break
		}
	}
	return result, nil
}

func splitText(text string, limit int) []string {
	parts := []string{}
	start := 0
	for i, r := range text {
		if i-start+utf8.RuneLen(r) > limit {
			parts = append(parts, text[start:i])
			start = i
		}
	}
	return append(parts, text[start:])
}
