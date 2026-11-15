package extension

import (
	"context"
	"errors"

	"github.com/fishioon/aicat/internal/messages"
	"github.com/fishioon/aicat/internal/scheduler"
)

type scheduledReceiver interface {
	CheckScheduledRoom(context.Context, messages.RoomIdentity) error
	ReceiveScheduled(context.Context, messages.RoomIdentity, scheduler.Occurrence) error
}

func (h *Host) scheduledRoom(ctx context.Context, id string) (messages.RoomIdentity, scheduledReceiver, error) {
	room, err := messages.RoomIdentityByID(h.home, id)
	if err != nil {
		return room, nil, err
	}
	it := h.find(room.Channel)
	if it == nil || !it.on.Load() {
		return room, nil, errors.New("消息渠道未启用，跳过本次定时消息")
	}
	receiver, ok := it.parts.Messages.(scheduledReceiver)
	if !ok {
		return room, nil, errors.New("消息渠道不支持定时消息")
	}
	return room, receiver, nil
}

func (h *Host) CheckScheduledRoom(ctx context.Context, id string) error {
	room, receiver, err := h.scheduledRoom(ctx, id)
	if err != nil {
		return err
	}
	return receiver.CheckScheduledRoom(ctx, room)
}

func (h *Host) DeliverScheduled(ctx context.Context, id string, occurrence scheduler.Occurrence) error {
	room, receiver, err := h.scheduledRoom(ctx, id)
	if err != nil {
		return err
	}
	return receiver.ReceiveScheduled(ctx, room, occurrence)
}
