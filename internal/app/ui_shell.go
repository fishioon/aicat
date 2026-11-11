//go:build !nogui

package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
)

const railWidth float32 = 53
const sidebarWidth float32 = 216

type settingsPage uint8

const (
	settingsProxy settingsPage = iota
	settingsGeneral
)

func (a *application) view(c *ui.Context) {
	p := colors(c)
	a.copyAnchors = make(map[string]ui.Element, 3)
	// Use MyGo native themed popups so menus follow the same palette as the shell.
	c.SetMenuPresentation(ui.PopoverMenus)
	c.OnShortcut(ui.Cmd, ui.KeyComma, func() { a.navigate(location{settings: true, page: settingsGeneral}) })
	c.OnShortcut(ui.Cmd, ui.KeyK, func() {
		if a.trafficEnabled && !a.settings && a.extension == "" {
			a.searchHandle.Focus(c)
		}
	})
	c.OnShortcut(ui.Cmd, ui.KeyBracketLeft, func() { a.travel(-1) })
	c.OnShortcut(ui.Cmd, ui.KeyBracketRight, func() { a.travel(1) })
	ui.Column(c).Fill().Gap(0).Background(p.surface).Font(fontUI).Lang(c.Locale()).FontSize(textUI).FontWeight(400).LineHeight(1.5).Children(func() {
		a.titlebar(c, p)
		ui.Row(c).Grow(1).FillWidth().AlignItems(ui.Stretch).Children(func() {
			ui.Column(c.Key("nav-rail")).Role(ui.RoleNavigation).Label("Navigation").Width(railWidth).Padding(14, 9, 12).Gap(8).Background(p.bg).BorderWidth(0, 1, 0, 0).BorderColor(p.border).Children(func() {
				a.navigationEntries(c, p)
				ui.Box(c).Grow(1)
				anchor := ui.Button(c.Key("account-menu"), "").Label(locales.AccountMenu()).Tooltip(locales.AccountMenu()).Size(35, 35).AlignSelf(ui.Center).Padding(4).Radius(8).Background(ui.Transparent).Border(0, ui.Transparent).Children(func() {
					ui.Box(c).Size(27, 27).Radius(14).Background(p.brand).AlignItems(ui.Center).Justify(ui.Center).Children(func() { ui.Text(c, "AC").FontSize(10).TextColor(ui.Hex("#111827")) })
				})
				if anchor.Clicked() {
					a.profileOpen = !a.profileOpen
				}
				ui.PopoverBase(c, anchor, &a.profileOpen, func(panel ui.Element) {
					panel.AttachTo(anchor, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, 10).Width(238).Padding(8).Radius(12).Background(p.surface).Border(1, p.border).Shadow(0, 6, 24, 0, p.text.Alpha(0.12)).Children(func() {
						ui.Row(c).FillWidth().Padding(10, 9, 13).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
							ui.Box(c).Size(27, 27).Radius(14).Background(p.brand).AlignItems(ui.Center).Justify(ui.Center).Children(func() { ui.Text(c, "AC").FontSize(10).TextColor(ui.Hex("#111827")) })
							ui.Column(c).Gap(1).Children(func() {
								ui.Text(c, "AICat").FontSize(13).FontWeight(500)
								ui.Text(c, locales.NotSignedInToPrivateCloud()).FontSize(11).TextColor(p.muted)
							})
						})
						ui.Text(c, tr("私云账号尚未接入")).FillWidth().Padding(9).FontSize(12).TextColor(p.muted)
						if ui.Button(c.Key("nav-settings"), locales.Settings()).FillWidth().Padding(9).FontSize(12).Justify(ui.Start).Background(ui.Transparent).Border(0, ui.Transparent).Clicked() {
							a.profileOpen = false
							a.navigate(location{settings: true, page: settingsGeneral})
						}
					})
				})
			})
			if !a.sidebarHidden {
				a.sidebar(c, p)
			}
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				if a.extension != "" {
					a.extensionsView(c, p)
				} else if a.settings {
					a.settingsView(c, p)
				} else {
					a.trafficView(c, p)
				}
			})
		})
	})
	a.securityDialogs(c, p)
	a.feedback(c, p)
}
func (a *application) titlebar(c *ui.Context, p palette) {
	offset := railWidth
	if !a.sidebarHidden {
		offset += sidebarWidth
	}
	// 工具区始终容纳窗口按钮与三个操作按钮。侧边栏收起时不能缩成
	// railWidth，否则操作按钮会溢出并被右侧拖拽区域覆盖。
	windowInset := max(c.TitleBar().Left+8, 92)
	offset = max(offset, windowInset+3*iconTarget+2*4+12)
	ui.Row(c.Key("titlebar")).Role(ui.RoleToolbar).Label("Titlebar").Height(48).FillWidth().Background(p.bg).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		left := ui.Row(c).Width(offset).Shrink(0).Height(48).AlignItems(ui.Center).Gap(4)
		if !a.sidebarHidden {
			left.BorderWidth(0, 1, 0, 0).BorderColor(p.border)
		}
		left.Children(func() {
			ui.Box(c).Width(windowInset - 4).Height(48).Shrink(0).DragWindow()
			if iconButton(c, "history-back", "back", locales.Back()).Disabled(a.travelIndex(-1) < 0).Clicked() {
				a.travel(-1)
			}
			if iconButton(c, "history-forward", "forward", locales.Forward()).Disabled(a.travelIndex(1) < 0).Clicked() {
				a.travel(1)
			}
			if iconButton(c, "toggle-sidebar", "sidebar", locales.ToggleSidebar()).Clicked() {
				a.sidebarHidden = !a.sidebarHidden
			}
			ui.Box(c).Grow(1).Height(48).DragWindow()
		})
		ui.Row(c).Grow(1).MinWidth(0).Height(48).Padding(0, 21).FontSize(12).AlignItems(ui.Center).Gap(8).Children(func() {
			title := locales.AllDomains()
			if a.selectionSource == "app" {
				title = a.clientAppTitle()
			} else if a.host != "" {
				title = a.host + a.path
			}
			section := "流量"
			if a.settings {
				section = "设置"
				title = tr("代理")
				if a.settingsPage == settingsGeneral {
					title = tr("通用")
				}
			}
			if a.extension != "" {
				section, title = "扩展", locales.Traffic()
				if a.extension == "community" {
					title = tr("社区扩展")
				}
			}
			ui.Row(c).Grow(1).MinWidth(0).Height(48).AlignItems(ui.Center).Gap(10).DragWindow().Children(func() {
				ui.Text(c.Key("breadcrumb-section"), tr(section)).TextColor(p.muted)
				ui.Text(c, "›").TextColor(p.muted)
				ui.Text(c.Key("breadcrumb-title"), title).FontWeight(500).SingleLine().Shrink(1)
			})
			if a.trafficEnabled && !a.settings && a.extension == "" {
				label := tr("正在录制")
				dot := p.brand
				if !a.recording {
					label = tr("继续录制")
					dot = p.muted
				}
				if ui.Button(c.Key("record-toggle"), "").Label(label).Padding(5, 7).Radius(6).Background(ui.Transparent).Border(0, ui.Transparent).Tooltip(locales.PauseOrResumeCaptureProxyForwardingContinues()).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(6, 6).Radius(3).Background(dot)
						ui.Text(c, label).FontSize(11).TextColor(p.muted)
					})
				}).Clicked() {
					a.toggleRecording()
				}
				if iconButton(c, "export", "download", tr("导出记录")).Clicked() {
					a.exportTraffic()
				}
			}
			if iconButton(c, "theme-toggle", "sun", locales.ToggleLightAndDarkMode()).Clicked() {
				next := a.preferences
				if mygo.Theme.IsDark() {
					next.Theme = "light"
				} else {
					next.Theme = "dark"
				}
				a.setPreferences(next)
			}
		})
	})
}
func (a *application) settingsView(c *ui.Context, p palette) {
	if a.sidebarHidden {
		ui.Row(c).FillWidth().Padding(10, 21).Gap(6).Children(func() {
			for _, item := range []struct {
				page       settingsPage
				key, label string
			}{{settingsGeneral, "settings-general", "通用"}, {settingsProxy, "settings-proxy", "代理"}} {
				b := ui.Button(c.Key(item.key), tr(item.label)).Background(ui.Transparent).Border(0, ui.Transparent).Radius(6)
				if a.settingsPage == item.page {
					b.Background(p.brandSoft).TextColor(p.brandText)
				}
				if b.Clicked() {
					a.navigate(location{settings: true, page: item.page})
				}
			}
		})
	}
	switch a.settingsPage {
	case settingsGeneral:
		a.generalSettings(c, p)
	default:
		a.proxySettings(c, p)
	}
}
