package messages

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/fishioon/aicat/internal/config"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

type outboundRecord struct {
	Fingerprint string  `json:"fingerprint"`
	MessageID   string  `json:"messageId"`
	Receipt     Receipt `json:"receipt"`
}

type deliveryLink struct {
	RequestID string `json:"requestId"`
}

type MCPSender func(context.Context, string, Event) (Receipt, error)

var outboundMu sync.Mutex
var activeOutbound = map[string]bool{}

func CheckRoomBinding(channel, source string, event Event) error {
	if event.Subject != "" && event.Subject != roomID(channel, source, event.Data.RoomID) {
		return errors.New("发送绑定的 Room 或账号已发生变化")
	}
	return nil
}

func readOutbound(file string) (outboundRecord, error) {
	var record outboundRecord
	raw, err := os.ReadFile(file)
	if err != nil {
		return record, err
	}
	if json.Unmarshal(raw, &record) != nil || record.Fingerprint == "" || record.MessageID == "" || record.Receipt.ID == "" {
		return record, errors.New("外发记录损坏")
	}
	return record, nil
}

func Deliver(ctx context.Context, home, channel, source string, event Event, send MCPSender) (Receipt, error) {
	m := event.Data
	if event.SpecVersion != "1.0" || event.Type != SendRequested || event.ContentType != "application/json" || (event.Source != "aicat-agent://mcp" && event.Source != "aicat-ui://desktop") || event.ID == "" || len(event.ID) > 256 || m.Action != "send" || m.Type != "text" || strings.TrimSpace(m.Text.Content) == "" || len(m.Text.Content) > 256<<10 || !utf8.ValidString(m.Text.Content) || m.ID != "" || m.From != "" || m.Time != nil || m.AgentSnapshot != nil || m.Schedule != nil || m.ContextKey != "" || m.Outgoing || m.Delivery != nil || len(m.ReplyTo) > 256 || m.RoomID == "" || len(m.RoomID) > 256 {
		return Receipt{}, errors.New("无效外发请求")
	}
	dir := filepath.Join(home, "rooms", roomID(channel, source, m.RoomID))
	file := filepath.Join(dir, "outbound", digest([]byte(event.ID))+".json")
	raw, _ := json.Marshal(event)
	fingerprint := digest(raw)
	outboundMu.Lock()
	record, err := readOutbound(file)
	if err == nil {
		defer outboundMu.Unlock()
		if record.Fingerprint != fingerprint {
			return Receipt{}, errors.New("同一外发 ID 的内容不能改变")
		}
		if record.Receipt.Status == "pending" && !activeOutbound[file] {
			record.Receipt.Status = "unknown"
			record.Receipt.Error = "上次发送未取得持久确认，不会自动重发"
			if err = writeJSON(file, record); err != nil {
				return Receipt{}, err
			}
		}
		return record.Receipt, nil
	}
	if !os.IsNotExist(err) {
		outboundMu.Unlock()
		return Receipt{}, err
	}
	settings, err := config.ReadRoomSettings(home, roomID(channel, source, m.RoomID))
	if err != nil || (m.SearchName != "" && m.SearchName != settings.SendTarget) {
		outboundMu.Unlock()
		return Receipt{}, errors.New("外发寻址必须与 Room 配置一致")
	}
	event.Data.SearchName = settings.SendTarget
	inbox := NewInbox(home, channel)
	rows, err := inbox.Read(source, m.RoomID, 200)
	if err != nil {
		outboundMu.Unlock()
		return Receipt{}, err
	}
	foundReference := m.ReplyTo == ""
	for _, row := range rows {
		if row.ID == m.ReplyTo && !row.Outgoing {
			event.Data.ContextKey = row.ContextKey
			foundReference = true
		}
	}
	if !foundReference {
		reference, exists, err := inbox.findMessage(ctx, source, m.RoomID, m.ReplyTo, false)
		if err != nil || !exists {
			outboundMu.Unlock()
			return Receipt{}, errors.New("reply_to 必须属于目标 Room")
		}
		event.Data.ContextKey = reference.ContextKey
	}
	index := filepath.Join(dir, "deliveries", digest([]byte(event.ID))+".json")
	if _, err = os.Stat(index); err == nil {
		outboundMu.Unlock()
		return Receipt{}, errors.New("该回复已经请求外发，请查看原发送记录")
	} else if !os.IsNotExist(err) {
		outboundMu.Unlock()
		return Receipt{}, err
	}
	record = outboundRecord{Fingerprint: fingerprint, MessageID: event.ID, Receipt: Receipt{Delivery: Delivery{ID: event.ID, Source: event.Source, Status: "pending"}}}
	if err = writeJSON(file, record); err == nil {
		_, err = inbox.rooms.append(source, "codex", event, "pending")
	}
	if err == nil {
		err = writeJSON(index, deliveryLink{RequestID: event.ID})
	}
	if err != nil {
		record.Receipt.Status, record.Receipt.Error = "failed", err.Error()
		saveErr := writeJSON(file, record)
		outboundMu.Unlock()
		return record.Receipt, errors.Join(err, saveErr)
	}
	activeOutbound[file] = true
	outboundMu.Unlock()
	defer func() { outboundMu.Lock(); delete(activeOutbound, file); outboundMu.Unlock() }()
	platformEvent := event
	platformEvent.Data.ContextKey = ""
	receipt, sendErr := send(ctx, channel, platformEvent)
	if sendErr != nil || receipt.Status != "sent" {
		if receipt.Status != "failed" {
			receipt.Status = "unknown"
		}
		if sendErr != nil {
			receipt.Error = sendErr.Error()
		}
	}
	receipt.ID, receipt.Source = event.ID, event.Source
	record.Receipt = receipt
	outboundMu.Lock()
	err = writeJSON(file, record)
	outboundMu.Unlock()
	if err != nil {
		return Receipt{Delivery: Delivery{ID: event.ID, Source: event.Source, Status: "unknown", Error: "发送后无法保存确认，请勿重发"}}, err
	}
	return receipt, nil
}

func attachDeliveries(home, room string, rows []Message) error {
	outboundMu.Lock()
	defer outboundMu.Unlock()
	dir := filepath.Join(home, "rooms", room)
	for i := range rows {
		if !rows[i].Outgoing {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "deliveries", digest([]byte(rows[i].ID))+".json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var link deliveryLink
		if json.Unmarshal(raw, &link) != nil || link.RequestID == "" {
			return errors.New("外发关联记录损坏")
		}
		file := filepath.Join(dir, "outbound", digest([]byte(link.RequestID))+".json")
		record, err := readOutbound(file)
		if err != nil {
			return err
		}
		if record.MessageID != rows[i].ID || record.Receipt.ID != link.RequestID {
			return errors.New("外发记录关联不一致")
		}
		if record.Receipt.Status == "pending" && !activeOutbound[file] {
			record.Receipt.Status, record.Receipt.Error = "unknown", "上次发送未取得持久确认，不会自动重发"
			if err = writeJSON(file, record); err != nil {
				return err
			}
		}
		delivery := record.Receipt.Delivery
		rows[i].Delivery = &delivery
	}
	return nil
}
