package extension

import (
	"context"
	"errors"

	"github.com/fishioon/aicat/internal/channelhost"
	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/fishioon/aicat/internal/messages"
)

func packageExtension(descriptor channelplugin.Descriptor) Extension {
	return Extension{PackageSource: "registry", PackageFormat: descriptor.Format, ID: descriptor.ID, Name: descriptor.Name, Summary: descriptor.Summary, Lede: descriptor.Summary, Categories: []string{"消息渠道"}, Registry: descriptor.Registry, Package: descriptor.Package, Version: descriptor.Version, Publisher: descriptor.Publisher,
		Build: func(env Env) (Parts, error) {
			channel := messages.New(env.Home, descriptor.ID, platformEndpoint(), channelhost.New(env.Home))
			channel.SetMCPSender(env.MCPSend)
			parts := Parts{Messages: channel, Configuration: channel, Run: channel.Run}
			if descriptor.Format == "aicat" {
				parts.ReadData = channel.ReadNativeData
				parts.Prepare = channel.PrepareNative
			}
			return parts, nil
		}}
}

func (h *Host) registered() []*item {
	h.itemsMu.RLock()
	defer h.itemsMu.RUnlock()
	return append([]*item{}, h.items...)
}

func (h *Host) InstallChannelPackage(ctx context.Context, descriptor channelplugin.Descriptor) error {
	if descriptor.Format != "aicat" {
		return errors.New("手动安装仅接受声明 aicat 渠道 API 的 NPM 包")
	}
	if !h.packageMu.TryLock() {
		return errors.New("正在登记其他渠道包")
	}
	defer h.packageMu.Unlock()
	if it := h.find(descriptor.ID); it != nil {
		if it.on.Load() {
			return errors.New("请先停用渠道插件")
		}
		if err := h.plugins.Register(descriptor); err != nil {
			return err
		}
		if !it.parts.Messages.Installed() {
			return h.Install(ctx, descriptor.ID)
		}
		return h.Update(ctx, descriptor.ID)
	}
	if err := h.plugins.Register(descriptor); err != nil {
		return err
	}
	e := packageExtension(descriptor)
	parts, err := e.Build(h.env)
	if err != nil {
		return err
	}
	if h.changed != nil {
		parts.Messages.OnChange(h.changed)
	}
	h.itemsMu.Lock()
	h.items = append(h.items, &item{Extension: e, parts: parts})
	h.itemsMu.Unlock()
	if h.changed != nil {
		h.changed()
	}
	return h.Install(ctx, descriptor.ID)
}
