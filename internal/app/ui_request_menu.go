//go:build !nogui

package app

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
	"github.com/fishioon/aicat/internal/traffic"
)

func (a *application) requestMenu(c *ui.Context, row ui.Element, r traffic.Summary) {
	key := fmt.Sprintf("request-row-%d", r.ID)
	a.copyAnchors[key] = row
	row.ContextMenu(func(m *ui.Menu) {
		if a.state.selectedID != r.ID {
			a.chooseRequest(r.ID)
		}
		if m.Item(tr("复制 URL")).ID("request-copy-url").Chosen() {
			a.copyText(c, key, r.URL)
		}
		record, err := a.r.traffic.Get(a.ctx, r.ID)
		canCopy := err == nil && !record.Tunnel && record.RequestReceived.Body.State == "complete"
		if m.Item(tr("复制 cURL")).ID("request-copy-curl").Disabled(!canCopy).Tooltip(tr("仅支持正文采集完整的 HTTP 请求")).Chosen() {
			body := a.bodyText(record.RequestReceived.Body)
			if body.Notice != "" || body.Info.State != "complete" || strings.IndexByte(body.Text, 0) >= 0 {
				a.copyNotice(c, key, tr("请求正文不可用，无法复制 cURL"), true)
			} else {
				a.copyText(c, key, requestCURL(record.RequestReceived, body.Text))
			}
		}
		m.Separator()
		u, err := url.Parse(r.URL)
		host := ""
		if err == nil {
			host = strings.ToLower(u.Host)
		}
		label := tr("固定")
		if a.pins[host] {
			label = locales.Unpin()
		}
		if m.Item(label).ID("request-pin").Disabled(host == "" || a.pinSaving).Chosen() {
			a.setPinned(host, !a.pins[host])
		}
	})
}

func shellQuote(text string) string { return "'" + strings.ReplaceAll(text, "'", "'\\''") + "'" }

func requestCURL(r traffic.Request, body string) string {
	parts := []string{"curl --request " + shellQuote(r.Method), "--url " + shellQuote(r.URL), "--compressed"}
	skip := map[string]bool{"content-length": true, "transfer-encoding": true, "connection": true, "proxy-connection": true, "keep-alive": true, "te": true, "trailer": true, "upgrade": true, "accept-encoding": true}
	for _, value := range r.Header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			skip[strings.ToLower(strings.TrimSpace(token))] = true
		}
	}
	for _, pair := range headerPairs(r.Header, "") {
		if !skip[strings.ToLower(pair.name)] {
			parts = append(parts, "--header "+shellQuote(pair.name+": "+pair.value))
		}
	}
	if body != "" {
		parts = append(parts, "--data-raw "+shellQuote(body))
	}
	return strings.Join(parts, " \\\n  ")
}
