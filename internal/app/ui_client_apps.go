//go:build !nogui

package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
	"github.com/fishioon/aicat/internal/traffic"
)

type appGroup struct {
	key, name         string
	requests, tunnels int
}

func (g appGroup) label() string {
	if g.key == "" {
		return locales.UnknownApplication()
	}
	return g.name
}

func (a *application) clientAppTitle() string {
	for _, group := range a.apps {
		if group.key == a.clientApp {
			return group.label()
		}
	}
	return locales.TrafficApplications()
}

func buildAppGroups(items []traffic.Summary) []appGroup {
	byApp := make(map[string]appGroup)
	for _, item := range items {
		key := item.ClientApp.Key()
		group := byApp[key]
		group.key = key
		group.name = item.ClientApp.Name
		if item.Tunnel {
			group.tunnels++
		} else {
			group.requests++
		}
		byApp[key] = group
	}
	groups := make([]appGroup, 0, len(byApp))
	for _, group := range byApp {
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if (a.key == "") != (b.key == "") {
			return b.key == ""
		}
		aName, bName := strings.ToLower(a.name), strings.ToLower(b.name)
		if aName != bName {
			return aName < bName
		}
		return a.key < b.key
	})
	return groups
}

func (a *application) appGroups(c *ui.Context, p palette) {
	toggle := func() {
		a.appsExpanded = !a.appsExpanded
		if !a.appsExpanded && a.selectionSource == "app" {
			a.navigate(location{query: a.state.query})
		}
	}
	sidebarGroup(c, p, "apps-group", locales.TrafficApplications(), len(a.apps), a.appsExpanded, false, toggle, toggle)
	if !a.appsExpanded {
		return
	}
	query := strings.ToLower(a.sideQuery)
	for _, group := range a.apps {
		name := group.label()
		if !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		a.appRow(c, p, group, name)
	}
}

func (a *application) appRow(c *ui.Context, p palette, group appGroup, name string) {
	key := "client-app-" + group.key
	if group.key == "" {
		key += "unknown"
	}
	count := fmt.Sprint(group.requests)
	if group.tunnels > 0 {
		count += fmt.Sprintf(" · %d %s", group.tunnels, locales.TunnelCountUnit())
	}
	row := ui.Row(c).FillWidth().Height(30).Radius(5).Padding(0, 0, 0, 26).AlignItems(ui.Center)
	if a.selectionSource == "app" && a.clientApp == group.key {
		row.Background(p.brandSoft).TextColor(p.brandText)
	} else if row.Hovered() {
		row.Background(p.soft)
	}
	row.Children(func() {
		button := ui.Button(c.Key(key), "").Label(name).Value(count).Tooltip(name).Grow(1).MinWidth(0).Height(30).Padding(0, 7, 0, 2).Background(ui.Transparent).Border(0, ui.Transparent).Children(func() {
			ui.Row(c).Fill().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).FontSize(12).SingleLine().Grow(1).MinWidth(0)
				ui.Text(c, count).FontSize(11).TextColor(p.muted)
			})
		})
		if button.Clicked() {
			a.navigate(location{source: "app", clientApp: group.key, query: a.state.query})
		}
	})
}
