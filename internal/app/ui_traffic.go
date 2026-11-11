//go:build !nogui

package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
	"github.com/fishioon/aicat/internal/traffic"
)

// searchField owns a stable native input. Live results never replace its key,
// buffer, focus or selection. The outer frame alone draws the focus feedback.
func (a *application) searchField(c *ui.Context, p palette, key string, value *string, label, placeholder string, height float32, shortcut bool) bool {
	var input ui.Element
	frame := ui.Row(c.Key(key+"-frame")).FillWidth().Height(height).Padding(4, 8).Gap(7).AlignItems(ui.Center).Radius(5).Border(1, p.border).Background(p.bg)
	if !shortcut {
		frame.Background(p.surface).Radius(7)
	}
	frame.Children(func() {
		ui.Icon(c, icon("search")).Size(14, 14).TextColor(p.muted)
		input = ui.TextInputBase(c.Key(key), value).Label(label).Placeholder(placeholder).Grow(1).MinWidth(0).FontSize(12).Padding(0).Background(ui.Transparent).Border(0, ui.Transparent).FocusRing(false)
		if shortcut {
			input.Ref(&a.searchHandle)
			ui.Text(c, "⌘ K").Font(fontCode).FontSize(10).Padding(1, 4).Border(1, p.border).Radius(3).TextColor(p.muted)
		}
	})
	if input.Focused() {
		frame.BorderColor(p.brand.Mix(p.border, 0.55))
	}
	return input.Changed()
}

func (a *application) trafficView(c *ui.Context, p palette) {
	st := &a.state
	c.OnShortcut(0, ui.KeyEscape, a.clearSelection)
	ui.Row(c.Key("traffic-toolbar")).Role(ui.RoleToolbar).Label(tr("搜索 URL")).Height(44).FillWidth().Padding(7, 14).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Grow(1).MinWidth(0).Children(func() {
			if a.searchField(c, p, "traffic-search", &st.query, tr("搜索 URL"), tr("搜索 URL…"), 28, true) {
				a.filter()
			}
		})
		if ui.Button(c.Key("clear-traffic"), "").Label(locales.Clear()).Height(28).Padding(4, 9).Radius(5).FontSize(11).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() { ui.Icon(c, icon("trash")).Size(14, 14); ui.Text(c, locales.Clear()) })
		}).Clicked() {
			a.clearSelection()
			a.host, a.path, a.clientApp, a.state.query = "", "", "", ""
			a.selectionSource = "domain"
			go func() { _ = a.r.traffic.Clear() }()
		}
	})
	if st.problem != "" {
		ui.Text(c, st.problem).FontSize(11).TextColor(p.danger).Padding(6, 14)
	}
	ui.SplitVerticalPercent(c, &st.split, func() {
		ui.Column(c.Key("traffic-table")).Role(ui.RoleTable).Label(locales.TrafficList()).Fill().Children(func() {
			a.trafficHeader(c, p)
			st.table.Selected = &st.selected
			st.table.Key = func(i int) any { return st.visible[i].ID }
			st.table.Label = func(i int) string { r := st.visible[i]; return fmt.Sprintf("#%d %s %s", r.ID, r.Method, r.URL) }
			t := *c.Theme()
			t.Accent = p.brandSoft
			t.InactiveAccent = p.brandSoft
			t.AccentText = p.text
			t.Radius = 0
			ui.Themed(c, &t).Grow(1).FillWidth().Children(func() {
				if len(st.visible) == 0 {
					label := tr("没有匹配的请求")
					if len(st.items) == 0 {
						label = tr("等待流量 · 请在客户端配置代理 ") + a.r.gateway.Status().Addr
					}
					ui.Column(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Padding(20, 14).Children(func() { ui.Text(c, label).FontSize(12).TextColor(p.muted) })
					return
				}
				list := ui.List(c.Key("traffic-rows"), &st.table, len(st.visible)).Grow(1).FillWidth().FocusRing(false).Rows(func(row ui.ListRow) {
					cc := row.Context
					r := st.visible[row.Index]
					bg := p.surface
					if row.Index%2 == 1 {
						bg = p.stripe
					}
					if row.Hovered() {
						bg = p.soft
					}
					if row.Selected() {
						bg = p.brandSoft
					}
					e := ui.Row(cc).Role(ui.RoleRow).Label(fmt.Sprintf("#%d %s %s", r.ID, r.Method, r.URL)).Height(30).FillWidth().AlignItems(ui.Stretch).Background(bg).BorderWidth(0, 0, 0, 2).BorderColor(ui.Transparent)
					if row.Selected() {
						e.BorderColor(p.brand)
					}
					e.Children(func() {
						a.trafficCell(cc, p, 0, false, func() { ui.Text(cc, fmt.Sprint(r.ID)).Font(fontCode).TextColor(p.muted).FontSize(11) })
						a.trafficCell(cc, p, 1, false, func() { urlText(cc, p, r.URL, false).FontSize(12).SingleLine().Tooltip(r.URL) })
						a.trafficCell(cc, p, 2, false, func() { ui.Text(cc, r.Method).Font(fontCode).FontSize(11).TextColor(p.muted).SingleLine() })
						a.trafficCell(cc, p, 3, false, func() {
							value, color := recordStatus(r, p)
							ui.Text(cc, value).Font(fontCode).FontSize(11).TextColor(color).SingleLine()
						})
						a.trafficCell(cc, p, 4, false, func() { ui.Text(cc, formatDuration(r.Duration)).FontSize(11).TextColor(p.muted).SingleLine() })
						a.trafficCell(cc, p, 5, false, func() {
							value := formatBytes(r.ResponseBytes)
							if r.Tunnel {
								value = "—"
							}
							ui.Text(cc, value).FontSize(11).TextColor(p.muted).SingleLine()
						})
					})
					a.requestMenu(cc, e, r)
				})
				if list.Changed() && st.selected >= 0 && st.selected < len(st.visible) {
					a.chooseRequest(st.visible[st.selected].ID)
				}

			})
		})
	}, func() { a.details(c, p) }).Grow(1).FillWidth()
	ui.Row(c.Key("traffic-footer")).Role(ui.RoleStatus).Height(27).Padding(0, 14).FillWidth().AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(p.border).Gap(8).Children(func() {
		ui.Text(c, fmt.Sprintf(locales.DRequests(), len(st.visible))).FontSize(11).TextColor(p.muted)
		ui.Text(c, "·").TextColor(p.border)
		n := 0
		if st.selectedID != 0 {
			n = 1
		}
		ui.Text(c, fmt.Sprintf(tr("已选择 %d 项"), n)).FontSize(11).TextColor(p.muted)
	})
}

func (a *application) trafficHeader(c *ui.Context, p palette) {
	labels := []string{"#", "URL", locales.Method(), locales.Status(), locales.Time(), locales.Size()}
	ui.Row(c.Key("traffic-header")).Role(ui.RoleRow).FillWidth().Height(27).Padding(0, 0, 0, 2).AlignItems(ui.Stretch).BorderWidth(1, 0, 1, 0).BorderColor(p.border).Children(func() {
		for i, label := range labels {
			a.trafficCell(c, p, i, true, func() { ui.Text(c, label).FontSize(11).TextColor(p.muted).SingleLine() })
		}
	})
}

func (a *application) trafficCell(c *ui.Context, p palette, col int, header bool, fn func()) {
	widths := []float32{36, 0, 74, 56, 68, 64}
	e := ui.Row(c).AlignItems(ui.Center).Padding(0, 8).MinWidth(0)
	if col == 0 {
		e.Padding(0, 8, 0, 12)
	}
	if col == 5 {
		e.Padding(0, 14, 0, 8)
	}
	if widths[col] == 0 {
		e.Grow(1).Shrink(1)
	} else {
		e.Width(widths[col]).Shrink(0)
	}
	if col >= 4 {
		e.Justify(ui.End)
	}
	if header && col > 0 {
		e.BorderWidth(0, 0, 0, 1).BorderColor(p.border.Alpha(0.55))
	}
	e.Children(fn)
}

func urlText(c *ui.Context, p palette, raw string, summary bool) ui.Element {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ui.Text(c, raw)
	}
	if u.User != nil {
		return ui.Text(c, raw)
	}
	hostColor := p.text
	if summary {
		hostColor = p.brandText
	}
	prefix := u.Scheme + "://"
	if !strings.HasPrefix(raw, prefix+u.Host) {
		return ui.Text(c, raw)
	}
	tail := strings.TrimPrefix(raw, prefix+u.Host)
	return ui.RichText(c, ui.Span{Text: prefix, Color: p.muted}, ui.Span{Text: u.Host, Color: hostColor}, ui.Span{Text: tail, Color: p.muted})
}

func recordStatus(r traffic.Summary, p palette) (string, ui.Color) {
	if r.State == traffic.Requesting || r.State == traffic.Responding {
		return locales.InProgress(), p.muted
	}
	if r.StatusCode != 0 {
		if r.StatusCode >= 400 {
			return fmt.Sprint(r.StatusCode), p.danger
		}
		return fmt.Sprint(r.StatusCode), p.success
	}
	if r.Error != "" {
		return locales.Failed(), p.danger
	}
	return "…", p.muted
}
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	if d >= time.Second {
		return fmt.Sprintf("%.2f s", d.Seconds())
	}
	return fmt.Sprintf("%.0f ms", float64(d.Microseconds())/1000)
}
func formatBytes(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	if n >= 1<<10 {
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func (a *application) exportTraffic() {
	records := a.state.visible
	if records == nil {
		records = []traffic.Summary{}
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		a.state.problem = err.Error()
		return
	}
	path, err := chooseTrafficExport(a.win)
	if err != nil {
		a.state.problem = err.Error()
		return
	}
	if path == "" {
		return
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		a.state.problem = fmt.Sprintf(tr("无法导出记录：%v"), err)
	}
}
