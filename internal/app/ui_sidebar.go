//go:build !nogui

package app

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/traffic"
)

type domainNode struct {
	Name, Path string
	Count      int
	Children   map[string]*domainNode
}

func withinPath(path, prefix string) bool {
	return prefix == "" || prefix == "/" || path == prefix || strings.HasPrefix(path, prefix+"/")
}

func buildDomainTrees(items []traffic.Summary) map[string]*domainNode {
	roots := make(map[string]*domainNode)
	for _, r := range items {
		u, err := url.Parse(r.URL)
		if err != nil || u.Host == "" {
			continue
		}
		host := strings.ToLower(u.Host)
		root := roots[host]
		if root == nil {
			root = &domainNode{Name: host, Children: make(map[string]*domainNode)}
			roots[host] = root
		}
		root.Count++
		if r.Tunnel {
			continue
		}
		path := u.EscapedPath()
		if path == "" {
			path = "/"
		}
		node, prefix := root, ""
		for _, part := range strings.Split(path, "/")[1:] {
			prefix += "/" + part
			child := node.Children[part]
			if child == nil {
				child = &domainNode{Name: "/" + part, Path: prefix, Children: make(map[string]*domainNode)}
				node.Children[part] = child
			}
			child.Count++
			node = child
		}
	}
	return roots
}

func branchKey(host, path string) string { return host + "|" + path }

func (a *application) revealFilter() {
	if a.selectionSource == "app" {
		a.appsExpanded = true
		return
	}
	if a.host == "" {
		return
	}
	if a.selectionSource == "pin" {
		a.pinsExpanded = true
	} else {
		a.domainsExpanded = true
	}
	if a.path == "" {
		return
	}
	a.expanded[branchKey(a.host, "")] = true
	prefix := ""
	parts := strings.Split(a.path, "/")[1:]
	for i := 0; i+1 < len(parts); i++ {
		prefix += "/" + parts[i]
		a.expanded[branchKey(a.host, prefix)] = true
	}
}

func (a *application) sidebar(c *ui.Context, p palette) {
	ui.Column(c.Key("sidebar")).Role(ui.RoleNavigation).Label(locales.Sidebar()).Width(sidebarWidth).Background(p.bg).BorderWidth(0, 1, 0, 0).BorderColor(p.border).Children(func() {
		if a.extension != "" {
			a.extensionsSidebar(c, p)
			return
		}
		if a.settings {
			ui.Column(c).FillWidth().Padding(16, 10).Gap(14).Children(func() {
				for _, item := range []struct {
					page       settingsPage
					key, label string
				}{{settingsGeneral, "settings-general", "通用"}, {settingsProxy, "settings-proxy", "代理"}} {
					b := ui.Button(c.Key(item.key), tr(item.label)).FillWidth().Height(34).Padding(0, 8).Justify(ui.Start).Background(ui.Transparent).Border(0, ui.Transparent).Radius(6)
					if a.settingsPage == item.page {
						b.Background(p.brandSoft).TextColor(p.brandText)
					}
					if b.Clicked() {
						a.navigate(location{settings: true, page: item.page})
					}
				}
			})
			return
		}
		ui.Column(c).FillWidth().Padding(10, 10, 0).Children(func() {
			a.searchField(c, p, "domain-search", &a.sideQuery, locales.FindAppOrDomain(), locales.FindAppOrDomainPlaceholder(), 32, false)
		})
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(0, 10).Children(func() {
				a.appGroups(c, p)
				a.domainGroup(c, p, true)
				if a.pinsExpanded {
					hosts := make([]string, 0, len(a.pins))
					for host := range a.pins {
						hosts = append(hosts, host)
					}
					sort.Strings(hosts)
					for _, host := range hosts {
						a.domainTree(c, p, host, "pin")
					}
				}
				a.domainGroup(c, p, false)
				if a.domainsExpanded {
					hosts := make([]string, 0, len(a.domains))
					for host := range a.domains {
						hosts = append(hosts, host)
					}
					sort.Strings(hosts)
					for _, host := range hosts {
						a.domainTree(c, p, host, "domain")
					}
				}
			})
		})
		ui.Column(c).FillWidth().Padding(12, 18).Gap(3).Children(func() {
			if a.sidebarProblem != "" {
				ui.Text(c.Key("pin-error"), a.sidebarProblem).FontSize(textCaption).TextColor(p.danger)
			}
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(p.brand)
				ui.Text(c, tr("代理监听中")).FontSize(textCaption)
			})
			ui.Text(c, a.r.gateway.Status().Addr).FontSize(textCaption).TextColor(p.muted).Padding(0, 0, 0, 13)
		})
	})
}

func (a *application) domainGroup(c *ui.Context, p palette, pinned bool) {
	key, label, count, open := "all-traffic", locales.Domains(), len(a.domains), a.domainsExpanded
	if pinned {
		key, label, count, open = "pinned-group", locales.Pinned(), len(a.pins), a.pinsExpanded
	}
	toggle := func() {
		if pinned {
			a.pinsExpanded = !open
		} else {
			a.domainsExpanded = !open
		}
		if open && a.host != "" && ((pinned && a.selectionSource == "pin") || (!pinned && a.selectionSource != "pin")) {
			a.navigate(location{})
		}
	}
	choose := func() { a.navigate(location{}) }
	if pinned {
		choose = toggle
	}
	selected := !pinned && a.host == "" && a.selectionSource != "app"
	sidebarGroup(c, p, key, label, count, open, selected, toggle, choose)
}

func sidebarGroup(c *ui.Context, p palette, key, label string, count int, open, selected bool, toggle, choose func()) {
	ui.Box(c).Height(17)
	row := ui.Row(c).FillWidth().Height(30).Radius(5).AlignItems(ui.Center)
	if selected {
		row.Background(p.brandSoft).TextColor(p.brandText)
	} else if row.Hovered() {
		row.Background(p.soft)
	}
	row.Children(func() {
		chevron := "chevron-right"
		if open {
			chevron = "chevron-down"
		}
		if iconButton(c, key+"-toggle", chevron, tr("展开或收起")+" "+label).Size(21, 30).Padding(4).Clicked() {
			toggle()
		}
		b := ui.Button(c.Key(key), "").Label(label).Grow(1).MinWidth(0).Height(30).Padding(0, 7, 0, 2).Background(ui.Transparent).Border(0, ui.Transparent).Children(func() {
			ui.Row(c).Fill().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, label).FontSize(12).Grow(1)
				ui.Text(c, fmt.Sprint(count)).FontSize(11).TextColor(p.muted)
			})
		})
		if b.Clicked() {
			choose()
		}
	})
}

func (a *application) domainTree(c *ui.Context, p palette, host, source string) {
	if !strings.Contains(strings.ToLower(host), strings.ToLower(a.sideQuery)) {
		return
	}
	node := a.domains[host]
	if node == nil {
		node = &domainNode{Name: host}
	}
	a.treeNode(c, p, host, node, source, 0)
}

func (a *application) treeNode(c *ui.Context, p palette, host string, node *domainNode, source string, depth int) {
	key := "path-" + source + "-" + host + node.Path
	if depth == 0 {
		key = "domain-" + host
		if source == "pin" {
			key = "pin-" + host
		}
	}
	selected := a.host == host && a.path == node.Path && a.selectionSource == source
	open := a.expanded[branchKey(host, node.Path)]
	height := float32(28)
	if depth == 0 {
		height = 30
	}
	row := ui.Row(c).FillWidth().Height(height).Radius(5).Padding(0, 0, 0, float32(9+13*depth)).AlignItems(ui.Center)
	if selected {
		row.Background(p.brandSoft).TextColor(p.brandText)
	} else if row.Hovered() {
		row.Background(p.soft)
	}
	row.Children(func() {
		if len(node.Children) > 0 {
			chevron := "chevron-right"
			if open {
				chevron = "chevron-down"
			}
			if iconButton(c, key+"-toggle", chevron, tr("展开或收起")+" "+host+node.Path).Size(17, height).Padding(2).Clicked() {
				a.expanded[branchKey(host, node.Path)] = !open
				if open && a.host == host && a.path != node.Path && withinPath(a.path, node.Path) {
					a.navigate(location{host: host, path: node.Path, source: a.selectionSource})
				}
			}
		} else {
			ui.Box(c).Width(17)
		}
		b := ui.Button(c.Key(key), "").Label(host+node.Path).Tooltip(host+node.Path).Grow(1).MinWidth(0).Height(height).Padding(0, 7, 0, 2).Background(ui.Transparent).Border(0, ui.Transparent).Children(func() {
			ui.Row(c).Fill().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, node.Name).FontSize(12).SingleLine().Grow(1).MinWidth(0)
				ui.Text(c, fmt.Sprint(node.Count)).FontSize(11).TextColor(p.muted)
			})
		})
		if b.Clicked() {
			a.navigate(location{host: host, path: node.Path, source: source})
		}
		if depth == 0 {
			b.ContextMenu(func(m *ui.Menu) {
				label := locales.PinDomain()
				if a.pins[host] {
					label = locales.Unpin()
				}
				if m.Item(label).Chosen() && !a.pinSaving {
					a.setPinned(host, !a.pins[host])
				}
			})
		}
	})
	if open {
		keys := make([]string, 0, len(node.Children))
		for key := range node.Children {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			a.treeNode(c, p, host, node.Children[key], source, depth+1)
		}
	}
}

func (a *application) setPinned(host string, pinned bool) {
	if a.pinSaving {
		return
	}
	next := make(map[string]bool, len(a.pins)+1)
	for key := range a.pins {
		next[key] = true
	}
	if pinned {
		next[host] = true
	} else {
		delete(next, host)
	}
	hosts := make([]string, 0, len(next))
	for key := range next {
		hosts = append(hosts, key)
	}
	sort.Strings(hosts)
	a.pinSaving = true
	a.sidebarProblem = ""
	go func() {
		err := config.SetPinnedDomains(a.r.home, hosts)
		a.post(func() {
			a.pinSaving = false
			if err != nil {
				a.sidebarProblem = fmt.Sprintf(tr("无法保存固定域名：%v"), err)
				return
			}
			a.pins = next
			if !pinned {
				for i := range a.routes {
					if a.routes[i].host == host && a.routes[i].source == "pin" {
						a.routes[i].source = "domain"
					}
				}
			}
			if pinned {
				a.pinsExpanded = true
				a.expanded[branchKey(host, "")] = true
			}
			if !pinned && a.host == host && a.selectionSource == "pin" {
				a.selectionSource = "domain"
				a.revealFilter()
			}
		})
	}()
}
