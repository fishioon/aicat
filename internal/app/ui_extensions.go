//go:build !nogui

package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
)

// Extension browsing is presentation state; built-in traffic continues to use
// the existing capture service and features.traffic persistence.
type extensionsState struct {
	query                                                      string
	installedCollapsed, builtinCollapsed, recommendedCollapsed bool
}

func (a *application) extensionsSidebar(c *ui.Context, p palette) {
	ui.Row(c).FillWidth().Padding(10, 10, 0).Gap(3).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Grow(1).MinWidth(0).Children(func() {
			if a.searchField(c, p, "extension-search", &a.extensions.query, locales.SearchExtensions(), locales.SearchExtensionsPlaceholder(), 32, false) && strings.TrimSpace(a.extensions.query) != "" {
				a.extensions.builtinCollapsed = false
			}
		})
		if a.extensions.query != "" {
			if iconButton(c, "extension-search-clear", "close", locales.ClearExtensionSearch()).Size(24, 28).Padding(4).Clicked() {
				a.extensions.query = ""
			}
		}
	})
	ui.Scroll(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Padding(12, 6).Gap(4).Children(func() {
			if strings.TrimSpace(a.extensions.query) == "" {
				a.extensionGroup(c, p, "installed", "已安装", "0", &a.extensions.installedCollapsed, func() {
					ui.Text(c, locales.NoCommunityExtensionsInstalled()).FillWidth().Padding(8, 12, 12).FontSize(textCaption).TextColor(p.muted)
				})
			}
			query := strings.ToLower(strings.TrimSpace(a.extensions.query))
			matchesTraffic := strings.Contains(strings.ToLower(locales.Traffic()+" "+locales.InspectHttpAndHttpsRequestsAndResponses()+" traffic http https network 流量 请求 响应"), query)
			count := "0"
			if matchesTraffic {
				count = "1"
			}
			a.extensionGroup(c, p, "builtin", "内置", count, &a.extensions.builtinCollapsed, func() {
				if matchesTraffic {
					a.extensionItem(c, p, "traffic", "activity", "流量", locales.InspectHttpAndHttpsRequestsAndResponses(), "aicat · "+locales.BuiltIn())
				} else {
					ui.Text(c.Key("extensions-no-results"), locales.NoMatchingBuiltInExtensions()).FillWidth().Padding(8, 12, 12).FontSize(textCaption).TextColor(p.muted)
				}
			})
			a.extensionGroup(c, p, "recommended", "推荐", "", &a.extensions.recommendedCollapsed, func() {
				a.extensionItem(c, p, "community", "extensions", "社区扩展", "发现更多扩展能力", locales.ComingSoon())
			})
		})
	})
}

func (a *application) extensionGroup(c *ui.Context, p palette, key, label, count string, collapsed *bool, content func()) {
	chevron := "chevron-down"
	if *collapsed {
		chevron = "chevron-right"
	}
	if ui.Button(c.Key("extensions-"+key+"-toggle"), "").Label(tr("展开或收起")+" "+tr(label)).FillWidth().Height(30).Padding(0, 6).Background(ui.Transparent).Border(0, ui.Transparent).Radius(4).Children(func() {
		ui.Row(c).Fill().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Icon(c, icon(chevron)).Size(13, 13).TextColor(p.muted)
			ui.Text(c, tr(label)).FontSize(11).FontWeight(600).Grow(1)
			if count != "" {
				ui.Text(c, count).FontSize(10).TextColor(p.muted).Padding(0, 5).Radius(8).Background(p.soft)
			}
		})
	}).Clicked() {
		*collapsed = !*collapsed
	}
	if !*collapsed {
		content()
	}
}

func (a *application) extensionItem(c *ui.Context, p palette, id, symbol, title, description, source string) {
	b := ui.Button(c.Key("extension-"+id), "").Label(tr(title)).Tooltip(tr(description)).FillWidth().Padding(10, 8).Background(ui.Transparent).Border(0, ui.Transparent).Radius(5)
	if a.extension == id {
		b.Background(p.brandSoft)
	} else if b.Hovered() {
		b.Background(p.soft)
	}
	if b.Children(func() {
		ui.Row(c).FillWidth().Gap(9).AlignItems(ui.Start).Children(func() {
			ui.Box(c).Size(29, 29).Shrink(0).Radius(6).Background(p.surface).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Icon(c, icon(symbol)).Size(19, 19).TextColor(p.brandText)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(5).Children(func() {
					ui.Text(c, tr(title)).FontSize(12).FontWeight(600).Grow(1).MinWidth(0).SingleLine()
					if id == "traffic" {
						dot := p.muted
						if a.trafficEnabled {
							dot = p.brand
						}
						ui.Box(c).Size(5, 5).Radius(3).Background(dot)
					}
				})
				ui.Text(c, tr(description)).FillWidth().FontSize(11).SingleLine().TextColor(p.muted)
				ui.Text(c, source).FillWidth().FontSize(10).SingleLine().TextColor(p.muted)
			})
		})
	}).Clicked() {
		a.navigate(location{extension: id})
	}
}

func (a *application) extensionsView(c *ui.Context, p palette) {
	if a.sidebarHidden {
		ui.Row(c).FillWidth().Padding(8, 24).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			if ui.Button(c.Key("extensions-show-sidebar"), locales.BrowseExtensions()).FontSize(12).Background(ui.Transparent).Border(0, ui.Transparent).Clicked() {
				a.sidebarHidden = false
			}
		})
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(32).Gap(24).Children(func() {
			if a.extension == "community" {
				ui.Column(c.Key("community-extension-placeholder")).Role(ui.RoleGroup).Label(locales.CommunityExtensions()).FillWidth().MaxWidth(760).Gap(18).Children(func() {
					ui.Icon(c, icon("extensions")).Size(44, 44).TextColor(p.muted)
					ui.Text(c, locales.CommunityExtensions()).FontSize(26).FontWeight(600)
					ui.Text(c, locales.DiscoverMoreCapabilities()).FontSize(14).TextColor(p.muted)
					ui.Text(c, locales.ComingSoon()).AlignSelf(ui.Start).FontSize(11).TextColor(p.muted).Padding(4, 9).Background(p.soft).Radius(4)
					ui.Box(c).FillWidth().Height(1).Background(p.border)
					ui.Text(c, locales.CommunityExtensionsPlaceholder()).FillWidth().TextColor(p.muted)
				})
				return
			}
			ui.Column(c).FillWidth().MaxWidth(760).Gap(24).Children(func() {
				ui.Row(c).FillWidth().Gap(20).AlignItems(ui.Start).Children(func() {
					ui.Box(c).Size(64, 64).Shrink(0).Radius(12).Background(p.brandSoft).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Icon(c, icon("activity")).Size(36, 36).TextColor(p.brandText)
					})
					ui.Column(c).Grow(1).MinWidth(0).Gap(8).Children(func() {
						ui.Text(c, locales.Traffic()).FontSize(26).FontWeight(600)
						ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
							ui.Text(c, "aicat").FontSize(12).TextColor(p.muted)
							ui.Text(c, locales.BuiltInExtension()).FontSize(10).TextColor(p.brandText).Padding(2, 6).Background(p.brandSoft).Radius(4)
						})
						ui.Text(c, locales.InspectHttpAndHttpsRequestsAndResponses()).FillWidth().FontSize(13).TextColor(p.muted)
					})
				})
				a.trafficExtensionControls(c, p)
				ui.Row(c).Gap(8).Children(func() {
					if ui.Button(c.Key("extension-open-traffic"), locales.OpenTraffic()).Disabled(!a.trafficEnabled).Padding(7, 12).Radius(5).Background(p.brandSoft).TextColor(p.brandText).Border(1, p.brandSoft).Clicked() {
						a.navigate(location{})
					}
					if ui.Button(c.Key("extension-proxy-settings"), locales.ProxySettings()).Padding(7, 12).Radius(5).Background(ui.Transparent).Border(1, p.border).Clicked() {
						a.navigate(location{settings: true, page: settingsProxy})
					}
				})
				ui.Box(c).FillWidth().Height(1).Background(p.border)
				ui.Text(c, locales.ExtensionDescription()).FontSize(14).FontWeight(600)
				ui.Text(c, locales.CaptureAndInspectTraffic()).FillWidth().TextColor(p.muted)
				ui.Text(c, locales.DisablingHidesTheEntryAndStopsCapturingNewRequestsProxyForwardingContinuesRetain()).FillWidth().TextColor(p.muted)
				ui.Text(c, locales.TheProxyIsAlwaysAvailableOnlyClientsConfiguredToUseItSendTrafficThroughIt()).FillWidth().FontSize(12).TextColor(p.muted)
			})
		})
	})
}
