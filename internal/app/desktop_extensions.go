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

func (d *Desktop) InstallExtensionSource(ctx context.Context, request channelplugin.SourceRequest) (string, error) {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return "", errors.New("应用尚未就绪或正在退出")
	}
	defer d.app.notifyState()
	return d.app.r.extensions.InstallSource(ctx, request)
}

func (d *Desktop) ParseExtensionSource(ctx context.Context, request channelplugin.SourceRequest) (channelplugin.Source, error) {
	return channelplugin.ParseSource(request.Source, request.Version)
}

func (d *Desktop) CancelExtensionSourceInstallation(ctx context.Context) error {
	if !d.app.ready.Load() || d.app.quitting.Load() {
		return errors.New("应用尚未就绪或正在退出")
	}
	defer d.app.notifyState()
	return d.app.r.extensions.CancelSourceInstallation()
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
