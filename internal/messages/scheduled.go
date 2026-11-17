package messages

import (
	"context"
	"errors"
	"time"

	"github.com/fishioon/aicat/internal/scheduler"
)

func RoomIdentityByID(home, id string) (RoomIdentity, error) {
	return readRoomIdentity(home, id)
}

func (c *Channel) CheckScheduledRoom(ctx context.Context, room RoomIdentity) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := c.BoundSource()
	if err != nil {
		return err
	}
	if room.Channel != c.id || room.Source != source {
		return errors.New("定时任务绑定的 Room 不属于当前渠道账号")
	}
	settings, err := c.rooms.readSettings(room.ID)
	if err != nil {
		return err
	}
	if !settings.Automatic {
		return errors.New("Room 已关闭自动处理，跳过本次定时消息")
	}
	c.mu.Lock()
	ready := c.dispatcher != nil && c.account != nil && c.state.Phase == "connected"
	c.mu.Unlock()
	if !ready {
		return errors.New("消息渠道未连接，跳过本次定时消息")
	}
	return nil
}

func (c *Channel) ReceiveScheduled(ctx context.Context, room RoomIdentity, occurrence scheduler.Occurrence) error {
	if err := c.CheckScheduledRoom(ctx, room); err != nil {
		return err
	}
	c.inbox.ingressMu.Lock()
	defer c.inbox.ingressMu.Unlock()
	now := time.Now().UTC()
	stamp := now.UnixMilli()
	id := "schedule-" + newID()
	event := Event{Subject: room.ID, SpecVersion: "1.0", ID: id, Source: "aicat://scheduler", Type: Received, Time: now.Format(time.RFC3339Nano), ContentType: "application/json", Data: Message{ID: id, Action: "receive", From: "aicat://scheduler", RoomID: room.Conversation, Time: &stamp, Type: "text", SenderKind: "system", Schedule: &occurrence, Text: Text{Content: "定时任务：" + occurrence.Name + "\n" + occurrence.Prompt}}}
	record, err := c.rooms.append(room.Source, "", event, "")
	if err != nil {
		return err
	}
	c.mu.Lock()
	d := c.dispatcher
	c.mu.Unlock()
	if d == nil {
		return errors.New("定时消息已保存，Agent 调度已停止")
	}
	d.Enqueue(room.Source, record)
	c.changed()
	return nil
}
