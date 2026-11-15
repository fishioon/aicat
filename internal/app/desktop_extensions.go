//go:build !nogui

package app

import (
	"context"
	"errors"

	"github.com/fishioon/aicat/internal/channelplugin"
)

func (d *Desktop) InstallChannelPackage(ctx context.Context, descriptor channelplugin.Descriptor) error {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return errors.New("应用尚未就绪或正在退出")
	}
	defer d.app.notifyState()
	return d.app.r.extensions.InstallChannelPackage(ctx, descriptor)
}

func (d *Desktop) ReadExtensionData(ctx context.Context, id string, query map[string]any) (map[string]any, error) {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return nil, errors.New("应用尚未就绪或正在退出")
	}
	return d.app.r.extensions.ReadData(ctx, id, query)
}

func (d *Desktop) ReadRoom(ctx context.Context, id, room string) (map[string]any, error) {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return nil, errors.New("应用尚未就绪或正在退出")
	}
	return d.app.r.extensions.ReadRoom(ctx, id, room)
}
