//go:build !nogui

package app

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

type headerPair struct{ name, value string }

func headerPairs(h http.Header, requestURL string) []headerPair {
	pairs := []headerPair{}
	if u, err := url.Parse(requestURL); err == nil && u.Host != "" && h.Get("Host") == "" {
		pairs = append(pairs, headerPair{"Host", u.Host})
	}
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range h[key] {
			pairs = append(pairs, headerPair{key, value})
		}
	}
	return pairs
}

func headerText(pairs []headerPair) string {
	var text strings.Builder
	for i, pair := range pairs {
		if i > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(pair.name + ": " + pair.value)
	}
	return text.String()
}

func headerTable(c *ui.Context, p palette, key string, pairs []headerPair) {
	ui.Column(c.Key(key + "-headers")).Role(ui.RoleTable).Label(tr("Header")).Fill().Children(func() {
		ui.Row(c).FillWidth().Height(27).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			ui.Text(c, tr("名称")).WidthPercent(35).Padding(4, 10).FontSize(11).TextColor(p.muted)
			ui.Text(c, tr("值")).Grow(1).Padding(4, 10).BorderWidth(0, 0, 0, 1).BorderColor(p.border).FontSize(11).TextColor(p.muted)
		})
		ui.Scroll(c.Key(key + "-headers-scroll")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for i, pair := range pairs {
					ui.Row(c).Role(ui.RoleRow).FillWidth().AlignItems(ui.Start).Padding(5, 0).Children(func() {
						ui.Text(c.Key(key+"-header-name-"+strconv.Itoa(i)), pair.name).WidthPercent(35).Shrink(0).Padding(0, 10).Font(fontCode).FontSize(12).LineHeight(1.65).Selectable()
						ui.Text(c.Key(key+"-header-value-"+strconv.Itoa(i)), pair.value).Grow(1).MinWidth(0).Padding(0, 10).Font(fontCode).FontSize(12).LineHeight(1.65).Selectable()
					})
				}
			})
		})
	})
}
