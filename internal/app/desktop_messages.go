//go:build !nogui

package app

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/fishioon/aicat/internal/messages"
)

func (d *Desktop) ConfigureMessageChannel(ctx context.Context, id string, account messages.AccountConfig) error {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return errors.New("应用尚未就绪或正在退出")
	}
	defer d.app.notifyState()
	return d.app.r.extensions.ConfigureMessageChannel(ctx, id, account)
}

func (d *Desktop) SendMessage(ctx context.Context, channel string, event messages.Event) (messages.Receipt, error) {
	a := d.app
	if !a.ready.Load() || a.pending.Load() || a.quitting.Load() {
		return messages.Receipt{}, errors.New("应用尚未就绪或正在退出")
	}
	if event.Source != "aicat-ui://desktop" {
		return messages.Receipt{}, errors.New("无效发送来源")
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	return a.r.extensions.SendMessage(ctx, channel, event)
}

func (d *Desktop) ReadExtensionConfig(ctx context.Context, id string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return nil, errors.New("应用尚未就绪或正在退出")
	}
	return d.app.r.extensions.ReadConfig(id)
}

func (d *Desktop) UpdateExtensionConfig(ctx context.Context, id string, patch map[string]any) error {
	return d.mutate(ctx, func() error { return d.app.r.extensions.UpdateConfig(id, patch) })
}

func (d *Desktop) OpenMessageLogs(ctx context.Context, id string) error {
	return d.mutate(ctx, func() error {
		state, ok := d.app.r.extensions.MessageStates()[id]
		if !ok {
			return errors.New("没有这个消息渠道")
		}
		info, err := os.Stat(state.RoomDir)
		if os.IsNotExist(err) {
			return errors.New("尚无消息日志")
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("消息日志目录不可用")
		}
		return openMessageLogs(state.RoomDir)
	})
}

func (d *Desktop) InstallExtension(ctx context.Context, id string) error {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return errors.New("应用尚未就绪或正在退出")
	}
	defer d.app.notifyState()
	return d.app.r.extensions.Install(ctx, id)
}

func (d *Desktop) UpdateExtension(ctx context.Context, id string) error {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return errors.New("应用尚未就绪或正在退出")
	}
	defer d.app.notifyState()
	return d.app.r.extensions.Update(ctx, id)
}

func (d *Desktop) UninstallExtension(ctx context.Context, id string) error {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return errors.New("应用尚未就绪或正在退出")
	}
	defer d.app.notifyState()
	return d.app.r.extensions.Uninstall(ctx, id)
}
