//go:build !nogui

package app

import (
	"context"
	"errors"

	"github.com/fishioon/aicat/internal/agent"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/messages"
)

func (d *Desktop) ListRoomModels(ctx context.Context) ([]agent.Model, error) {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return nil, errors.New("应用尚未就绪或正在退出")
	}
	cfg, err := config.Load(d.app.r.home)
	if err != nil {
		return nil, err
	}
	runner := agent.New()
	defer runner.Close()
	return runner.Models(ctx, cfg.MessageExecution.CodexBinary, d.app.r.home)
}

func (d *Desktop) ListRooms(ctx context.Context) ([]messages.RoomSummary, error) {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return nil, errors.New("应用尚未就绪或正在退出")
	}
	return d.app.r.extensions.ListRooms(ctx)
}

func (d *Desktop) ReadRoomByID(ctx context.Context, id string) (messages.RoomSummary, error) {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return messages.RoomSummary{}, errors.New("应用尚未就绪或正在退出")
	}
	return d.app.r.extensions.ReadRoomByID(ctx, id)
}

func (d *Desktop) UpdateRoomSettings(ctx context.Context, id string, settings config.RoomSettings) error {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return errors.New("应用尚未就绪或正在退出")
	}
	if err := messages.UpdateRoomSettings(ctx, d.app.r.home, id, settings); err != nil {
		return err
	}
	d.app.notifyState()
	return nil
}
