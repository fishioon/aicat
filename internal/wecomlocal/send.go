package wecomlocal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fishioon/aicat/internal/messages"
)

type savedSend struct {
	Receipt messages.Receipt `json:"receipt"`
	Digest  string           `json:"digest"`
}

func (c *channel) Send(ctx context.Context, event messages.Event) (messages.Receipt, error) {
	if event.SpecVersion != "1.0" || event.Type != messages.SendRequested || event.ContentType != "application/json" || event.ID == "" || len(event.ID) > 256 ||
		(event.Source != "aicat-agent://mcp" && event.Source != "aicat-ui://desktop") || event.Data.RoomID == "" || len(event.Data.RoomID) > 256 || strings.ContainsAny(event.Data.RoomID, "\r\n\x00") ||
		strings.TrimSpace(event.Data.SearchName) == "" || len(event.Data.SearchName) > 256 || strings.ContainsAny(event.Data.SearchName, "\r\n\x00") ||
		event.Data.Action != "send" || event.Data.Type != "text" || strings.TrimSpace(event.Data.Text.Content) == "" || len(event.Data.Text.Content) > 4000 || !utf8.ValidString(event.Data.Text.Content) || strings.ContainsRune(event.Data.Text.Content, 0) ||
		event.Data.ID != "" || event.Data.From != "" || event.Data.Time != nil {
		return messages.Receipt{}, errors.New("客户端发送需要有效的文本 CloudEvent、roomid 和明确的 search_name，单条文本最多 4000 字节")
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if err := ctx.Err(); err != nil {
		return messages.Receipt{}, err
	}
	local, err := c.settings()
	if err != nil {
		return messages.Receipt{}, err
	}
	source := "aicat-channel://" + ID + "/" + hash(local.AccountID+"\x00"+filepath.Clean(local.DataDir))
	raw, _ := json.Marshal(event)
	file := filepath.Join(c.home, "messages", "receipts", hash(source+"\x00"+event.Source+"\x00"+event.ID)+".json")
	var saved savedSend
	prior, err := os.ReadFile(file)
	if err == nil {
		if json.Unmarshal(prior, &saved) != nil {
			return messages.Receipt{}, errors.New("发送记录损坏")
		}
		if saved.Digest != hash(string(raw)) {
			return messages.Receipt{}, errors.New("同一发送标识不能使用不同内容")
		}
		if saved.Receipt.Status == "pending" {
			saved.Receipt.Status = "unknown"
			saved.Receipt.Error = "发送结果不明，请勿重新发送"
			if err = writePrivate(file, saved); err != nil {
				return messages.Receipt{}, err
			}
			if _, err = c.inbox.RecordSend(source, event, "unknown"); err != nil {
				return saved.Receipt, err
			}
		}
		return saved.Receipt, nil
	}
	if !os.IsNotExist(err) {
		return messages.Receipt{}, err
	}
	c.mu.Lock()
	account, phase := c.account, c.state.Phase
	c.mu.Unlock()
	if account == nil || phase != "connected" {
		return messages.Receipt{}, errors.New("客户端读取宿主尚未连接")
	}
	if !c.inbox.KnownRoom(source, event.Data.RoomID) {
		return messages.Receipt{}, errors.New("发送目标必须是当前客户端已读入 Room 的会话")
	}
	saved = savedSend{Receipt: messages.Receipt{Delivery: messages.Delivery{Source: event.Source, ID: event.ID, Status: "pending"}}, Digest: hash(string(raw))}
	if err = writePrivate(file, saved); err != nil {
		return messages.Receipt{}, err
	}
	if _, err = c.inbox.RecordSend(source, event, "pending"); err != nil {
		return messages.Receipt{}, err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	result, sendErr := account.Send(sendCtx, event)
	cancel()
	var confirmation messages.PlatformResult
	if sendErr == nil {
		sendErr = json.Unmarshal(result, &confirmation)
		if sendErr == nil && (confirmation.Channel != ID || confirmation.ChatID != event.Data.RoomID || confirmation.Confirmation != "client_script") {
			sendErr = errors.New("客户端确认无效")
		}
	}
	if sendErr != nil {
		saved.Receipt.Status = "unknown"
		saved.Receipt.Error = "发送结果不明，请检查企业微信客户端和辅助功能权限；同一请求不会自动重发"
		saved.Receipt.Error += "：" + sendErr.Error()
	} else {
		saved.Receipt.Status = "sent"
		saved.Receipt.Result = &confirmation
	}
	if err = writePrivate(file, saved); err != nil {
		return saved.Receipt, err
	}
	if _, err = c.inbox.RecordSend(source, event, saved.Receipt.Status); err != nil {
		return saved.Receipt, err
	}
	c.notify()
	return saved.Receipt, nil
}
