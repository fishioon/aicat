package messages

import (
	"os"
	"path/filepath"
	"strings"
)

type RoomIssue struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	MessageID string `json:"messageId"`
}

func deliveryIssues(home, room string) ([]RoomIssue, error) {
	outboundMu.Lock()
	defer outboundMu.Unlock()
	issues := []RoomIssue{}
	dir := filepath.Join(home, "rooms", room, "outbound")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return issues, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		file := filepath.Join(dir, entry.Name())
		record, err := readOutbound(file)
		if err != nil {
			return nil, err
		}
		delivery := record.Receipt.Delivery
		if delivery.Status == "pending" && !activeOutbound[file] {
			delivery.Status, delivery.Error = "unknown", "上次发送未取得持久确认，不会自动重发"
		}
		if delivery.Status == "failed" || delivery.Status == "unknown" {
			issues = append(issues, RoomIssue{ID: delivery.ID, Kind: "delivery", Status: delivery.Status, Error: delivery.Error, MessageID: record.MessageID})
		}
	}
	return issues, nil
}

func (r *RoomSummary) AddTaskIssues(tasks []Task) {
	for _, task := range tasks {
		if task.Source == r.Source && task.Room == r.Conversation && (task.Status == "failed" || task.Status == "unknown") {
			r.Issues = append(r.Issues, RoomIssue{ID: task.ID, Kind: "task", Status: task.Status, Error: task.Error, MessageID: task.Trigger.ID})
		}
	}
}
