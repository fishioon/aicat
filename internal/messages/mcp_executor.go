package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fishioon/aicat/internal/agent"
)

type MCPSender func(context.Context, string, Event) (Receipt, error)
type ScheduleCaller func(context.Context, string, json.RawMessage) (json.RawMessage, error)
type MCPExecutorOptions struct {
	Channel        string
	Send           MCPSender
	Target         func(Task) (Message, error)
	RequireReplyTo bool
	Schedule       ScheduleCaller
}

func MCPExecutor(options MCPExecutorOptions) func(context.Context, *agent.Runner, string, Task, string, chan<- struct{}) error {
	return func(ctx context.Context, r *agent.Runner, thread string, task Task, prompt string, started chan<- struct{}) error {
		if options.Send == nil {
			return errors.New("aicat MCP 发送能力不可用")
		}
		target := Message{RoomID: task.Room, SearchName: task.Settings.SendTarget}
		var err error
		if options.Target != nil {
			target, err = options.Target(task)
			if err != nil {
				return err
			}
		}
		if target.RoomID != task.Room {
			return errors.New("回复目标必须绑定当前 Room")
		}
		var deliveryErr error
		err = r.TurnWithModel(ctx, thread, task.Trigger.ID, prompt, task.Settings.Model, func(ctx context.Context, call agent.ToolCall) (any, error) {
			if deliveryErr != nil {
				return nil, deliveryErr
			}
			if strings.HasPrefix(call.Name, "schedules_") {
				if options.Schedule == nil {
					return nil, errors.New("定时任务 MCP 不可用")
				}
				var args map[string]json.RawMessage
				if json.Unmarshal(call.Arguments, &args) != nil || args == nil {
					return nil, errors.New("无效定时任务参数")
				}
				if _, exists := args["room_id"]; exists {
					return nil, errors.New("定时任务只能绑定当前 Room")
				}
				args["room_id"], _ = json.Marshal(roomID(options.Channel, task.Source, task.Room))
				raw, _ := json.Marshal(args)
				return options.Schedule(ctx, call.Name, raw)
			}
			var input struct {
				Event struct {
					Text        string  `json:"text"`
					ReplyTo     string  `json:"reply_to"`
					ReplyID     string  `json:"reply_id"`
					SpecVersion string  `json:"specversion"`
					ID          string  `json:"id"`
					Source      string  `json:"source"`
					Type        string  `json:"type"`
					ContentType string  `json:"datacontenttype"`
					Data        Message `json:"data"`
				} `json:"event"`
			}
			decoder := json.NewDecoder(bytes.NewReader(call.Arguments))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil || call.ID == "" {
				deliveryErr = errors.New("回复参数无效，请提供 {event:{text:正文}}")
				return nil, deliveryErr
			}
			data := input.Event.Data
			text := input.Event.Text
			if text == "" {
				text = data.Text.Content
			}
			if data.Delivery != nil || data.Outgoing || data.Schedule != nil {
				deliveryErr = errors.New("不能伪造回复或发送结果")
				return nil, deliveryErr
			}
			replyTo := input.Event.ReplyTo
			if replyTo == "" {
				replyTo = data.ReplyTo
			}
			if strings.TrimSpace(text) == "" || (data.Text.Content != "" && data.Text.Content != text) ||
				(data.RoomID != "" && data.RoomID != target.RoomID) || (data.SearchName != "" && data.SearchName != target.SearchName) ||
				(data.Action != "" && data.Action != "send") || (data.Type != "" && data.Type != "text") || data.ID != "" || data.From != "" || data.Time != nil || data.ContextKey != "" || data.AgentSnapshot != nil ||
				(input.Event.Source != "" && input.Event.Source != "aicat-agent://mcp") || (input.Event.SpecVersion != "" && input.Event.SpecVersion != "1.0") ||
				(input.Event.Type != "" && input.Event.Type != SendRequested) || (input.Event.ContentType != "" && input.Event.ContentType != "application/json") ||
				((options.RequireReplyTo || task.Settings.Mode == "snapshot") && replyTo == "") || (data.ReplyTo != "" && data.ReplyTo != replyTo) {
				deliveryErr = errors.New("回复正文无效或尝试改变绑定的 Room 与发送来源")
				return nil, deliveryErr
			}
			replyID := input.Event.ReplyID
			if replyID == "" {
				replyID = data.ReplyID
			}
			if data.ReplyID != "" && data.ReplyID != replyID {
				deliveryErr = errors.New("关联回复不一致")
				return nil, deliveryErr
			}
			publish := call.Name == "rooms_reply"
			if publish && replyID != "" {
				deliveryErr = errors.New("发布回复不能关联其他回复 ID")
				return nil, deliveryErr
			}
			if !publish && !task.Settings.AllowOutbound {
				deliveryErr = errors.New("当前 Room 未允许外发")
				return nil, deliveryErr
			}
			event := Event{Subject: roomID(options.Channel, task.Source, task.Room), SpecVersion: "1.0", ID: "agent-" + digest([]byte(task.ID+"\x00"+call.ID)), Source: "aicat-agent://mcp", Type: SendRequested, ContentType: "application/json", Data: Message{Action: "send", RoomID: target.RoomID, SearchName: target.SearchName, ReplyTo: replyTo, ReplyID: replyID, Type: "text", Text: Text{Content: text}}}
			expected := "sent"
			if publish {
				event.Type, event.Data.Action, event.Data.SearchName = ReplyPublished, "publish", ""
				expected = "saved"
			}
			receipt, e := options.Send(ctx, options.Channel, event)
			if e != nil || receipt.Status != expected {
				deliveryErr = fmt.Errorf("%w: MCP 回复未取得发送确认", agent.ErrUnknown)
				if e != nil {
					return nil, deliveryErr
				}
				return receipt, nil
			}
			return receipt, nil
		}, func(context.Context, string, string) error { return nil }, started, "rooms_reply,messages_send,schedules_set,schedules_list,schedules_delete")
		if deliveryErr != nil {
			return deliveryErr
		}
		return err
	}
}
