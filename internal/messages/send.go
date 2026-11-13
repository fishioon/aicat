package messages

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	credentials, err := c.credentials()
	if err != nil {
		return Receipt{}, errors.New("账号未配置")
	}
	source := c.source(credentials.AccountID)
	payload, _ := json.Marshal(event)
	key := digest([]byte(c.id + "\x00" + credentials.AccountID + "\x00" + event.Source + "\x00" + event.ID))
	file := filepath.Join(c.home, "messages", "receipts", key+".json")
	var saved struct {
		Receipt Receipt `json:"receipt"`
		Digest  string  `json:"digest"`
	}
	b, err := os.ReadFile(file)
	if err == nil {
		if json.Unmarshal(b, &saved) != nil {
			return Receipt{}, errors.New("发送记录损坏")
		}
		if saved.Digest != digest(payload) {
			return Receipt{}, errors.New("同一发送标识不能使用不同内容")
		}
		if saved.Receipt.Status == "pending" {
			saved.Receipt.Status = "unknown"
			for i := range saved.Receipt.Parts {
				if saved.Receipt.Parts[i].Status == "pending" {
					saved.Receipt.Parts[i].Status = "unknown"
				}
			}
			if err = writeJSON(file, saved); err != nil {
				return Receipt{}, err
			}
		}
		return saved.Receipt, nil
	}
	if !os.IsNotExist(err) {
		return Receipt{}, err
	}
	c.mu.Lock()
	a := c.account
	phase := c.state.Phase
	c.mu.Unlock()
	if a == nil || phase != "connected" {
		return Receipt{}, errors.New("账号尚未连接")
	}
	saved.Receipt = Receipt{Delivery: Delivery{Source: event.Source, ID: event.ID, Status: "pending"}}
	saved.Digest = digest(payload)
	if err = writeJSON(file, saved); err != nil {
		return Receipt{}, err
	}
	if _, err = c.appendRecord(source, credentials.AccountID, event, "pending"); err != nil {
		return Receipt{}, err
	}

	parts := splitText(event.Data.Text.Content, 4000)
	for i, text := range parts {
		part := event
		part.Data.Text.Content = text
		if len(parts) > 1 {
			part.ID = "part-" + digest([]byte(event.ID+"\x00"+strconv.Itoa(i)))
		}
		receipt := Delivery{Source: event.Source, ID: part.ID, Status: "pending"}
		if len(parts) > 1 {
			saved.Receipt.Parts = append(saved.Receipt.Parts, receipt)
			if err = writeJSON(file, saved); err != nil {
				return saved.Receipt, err
			}
			if _, err = c.appendRecord(source, credentials.AccountID, part, "pending"); err != nil {
				return saved.Receipt, err
			}
		}
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		result, sendErr := a.Send(sendCtx, part)
		cancel()
		var platformResult PlatformResult
		if sendErr == nil {
			sendErr = json.Unmarshal(result, &platformResult)
		}
		if sendErr != nil {
			receipt.Status = "unknown"
			receipt.Error = "发送结果不明，请勿重新发送"
		} else {
			receipt.Status = "sent"
			receipt.Result = &platformResult
		}
		if len(parts) > 1 {
			saved.Receipt.Parts[i] = receipt
		}
		if receipt.Status == "unknown" || i == len(parts)-1 {
			saved.Receipt.Status, saved.Receipt.Error, saved.Receipt.Result = receipt.Status, receipt.Error, receipt.Result
		}
		if err = writeJSON(file, saved); err != nil {
			return saved.Receipt, err
		}
		if _, err = c.appendRecord(source, credentials.AccountID, part, receipt.Status); err != nil {
			return saved.Receipt, err
		}
		c.changed()
		if receipt.Status != "sent" {
			break
		}
	}
	return saved.Receipt, nil
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
