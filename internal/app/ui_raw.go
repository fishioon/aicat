//go:build !nogui

package app

import (
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/fishioon/aicat/internal/app/locales"
	"github.com/fishioon/aicat/internal/traffic"
)

// Notice is shown outside the document: truncated/binary/error information must
// not look like bytes sent by a client or server, or be copied as HTTP content.
type rawBody struct {
	Text, Notice string
	Info         traffic.BodyInfo
}

func (a *application) bodyText(info traffic.BodyInfo) rawBody {
	if info.ID == "" {
		return rawBody{Notice: "正文未采集"}
	}
	b, err := a.r.traffic.OpenBody(a.ctx, info.ID)
	if err != nil {
		return rawBody{Notice: err.Error()}
	}
	defer b.Reader.Close()
	data, err := io.ReadAll(b.Reader)
	if err != nil {
		return rawBody{Notice: err.Error()}
	}
	body := rawBody{Text: string(data), Info: b.Info}
	if !utf8.Valid(data) || strings.IndexByte(body.Text, 0) >= 0 {
		body.Text = hex.Dump(data)
		body.Notice = "二进制正文，以十六进制显示"
	}
	return body
}

func requestText(r traffic.Request) string {
	u, _ := url.Parse(r.URL)
	target := r.URL
	if u != nil {
		if r.Method == http.MethodConnect {
			target = u.Host
		} else {
			target = u.RequestURI()
		}
	}
	proto := r.Proto
	if proto == "" {
		proto = "HTTP"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s\n", r.Method, target, proto)
	if u != nil && u.Host != "" && r.Header.Get("Host") == "" {
		fmt.Fprintf(&b, "Host: %s\n", u.Host)
	}
	writeRawHeaders(&b, r.Header)
	b.WriteByte('\n')
	return b.String()
}

func responseText(r traffic.Response) string {
	var b strings.Builder
	proto := r.Proto
	if proto == "" {
		proto = "HTTP"
	}
	fmt.Fprintf(&b, "%s %d %s\n", proto, r.StatusCode, http.StatusText(r.StatusCode))
	writeRawHeaders(&b, r.Header)
	b.WriteByte('\n')
	return b.String()
}
func writeRawHeaders(b *strings.Builder, h http.Header) {
	var headers strings.Builder
	_ = h.Write(&headers)
	b.WriteString(strings.ReplaceAll(headers.String(), "\r\n", "\n"))
}

func (a *application) renderDetails() {
	st := &a.state
	if st.record == nil {
		return
	}
	req := requestText(st.record.RequestReceived)
	if !st.record.Tunnel {
		req += st.bodyRequest.Text
	}
	res := ""
	if st.record.ResponseSent != nil && !st.record.Tunnel {
		res = responseText(*st.record.ResponseSent) + st.bodyResponse.Text
	}
	if st.request.Buffer.String() != req {
		st.request.Buffer.Reset(req)
	}
	if st.response.Buffer.String() != res {
		st.response.Buffer.Reset(res)
	}
}

func (a *application) details(c *ui.Context, p palette) {
	st := &a.state
	if st.record == nil {
		label := locales.NoRequestSelected()
		if st.selectedID != 0 {
			label = locales.LoadingRequest()
			if st.problem != "" {
				label = locales.UnableToLoadRequest()
			}
		}
		ui.Column(c.Key("details-empty")).Fill().AlignItems(ui.Center).Justify(ui.Center).Children(func() { ui.Text(c, label).FontSize(12).TextColor(p.muted) })
		return
	}
	ui.Column(c.Key("raw-details")).Fill().Children(func() {
		a.rawSummary(c, p)
		ui.SplitPercent(c, &st.detailSplit, func() { a.rawPane(c, p, true) }, func() { a.rawPane(c, p, false) }).Grow(1).FillWidth()
	})
}

func (a *application) rawSummary(c *ui.Context, p palette) {
	r := a.state.record
	ui.Row(c.Key("raw-summary")).Role(ui.RoleGroup).Label("HTTP").Height(42).FillWidth().Padding(6, 12).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		ui.Text(c, r.Method).Font(fontCode).FontSize(11).Padding(2, 10).MinWidth(62).TextAlign(ui.Center).Radius(12).Background(p.brandSoft).Border(1, p.brand.Alpha(.35)).TextColor(p.brandText)
		status, color := recordStatus(r.Summary, p)
		if r.StatusCode != 0 {
			status = fmt.Sprintf("%d %s", r.StatusCode, http.StatusText(r.StatusCode))
		}
		ui.Text(c, status).Font(fontCode).FontSize(11).Padding(2, 10).MinWidth(62).TextAlign(ui.Center).Radius(12).Background(color.Mix(p.surface, .88)).Border(1, color.Alpha(.3)).TextColor(color)
		urlText(c, p, r.URL, true).Font(fontCode).FontSize(12).SingleLine().Grow(1).MinWidth(0).Tooltip(r.URL)
		if r.Tunnel {
			ui.Text(c, locales.PassthroughEncrypted()).FontSize(11).TextColor(p.muted)
		}
	})
}

func (a *application) rawPane(c *ui.Context, p palette, request bool) {
	label, key, ed, body := locales.Response(), "response-detail", a.state.response, a.state.bodyResponse
	view := &a.state.responseView
	var pairs []headerPair
	if a.state.record.ResponseSent != nil {
		pairs = headerPairs(a.state.record.ResponseSent.Header, "")
	}
	if request {
		label, key, ed, body = locales.Request(), "request-detail", a.state.request, a.state.bodyRequest
		view = &a.state.requestView
		pairs = headerPairs(a.state.record.RequestReceived.Header, a.state.record.RequestReceived.URL)
	}
	encrypted := a.state.record.Tunnel && !request
	ui.Column(c.Key(key + "-pane")).Fill().Children(func() {
		ui.Row(c.Key(key+"-heading")).Role(ui.RoleToolbar).Label(label).Height(34).FillWidth().Padding(0, 10).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
			ui.Text(c, label).FontSize(13).FontWeight(500)
			tabs := ui.TabsBase(c.Key(key+"-tabs"), view, 2)
			tabs.List.Label(label).AlignSelf(ui.Stretch).Gap(12).Margin(0, 0, 0, 4).Children(func() {
				for i, name := range []string{"Header", "Raw"} {
					c.Key(key + "-" + strings.ToLower(name) + "-tab")
					tab := tabs.Tab(i).Label(name).Padding(2, 0, 0).BorderWidth(0, 0, 2, 0).BorderColor(ui.Transparent).TextColor(p.muted)
					if *view == i {
						tab.TextColor(p.brandText).BorderColor(p.brand)
					}
					tab.Children(func() { ui.Text(c, name).FontSize(12) })
				}
			})
			if tabs.List.Changed() {
				c.CloseToast("clipboard")
			}
			ui.Box(c).Grow(1)
			copyKey := key + "-copy"
			button := iconButton(c, copyKey, "copy", locales.Copy()+" "+label).Size(26, 26).Disabled(encrypted)
			a.copyAnchors[copyKey] = button
			if button.Clicked() {
				text := ed.Buffer.String()
				if *view == 0 {
					text = headerText(pairs)
				}
				a.copyText(c, copyKey, text)
			}
		})
		if encrypted {
			a.tunnelContent(c, p)
			return
		}
		if *view == 0 {
			if !request && a.state.record.ResponseSent == nil {
				ui.Text(c, locales.ResponsePending()).FontSize(11).TextColor(p.muted).Padding(6, 12)
				return
			}
			headerTable(c, p, key, pairs)
			return
		}
		if !request && a.state.record.ResponseSent == nil {
			body.Notice = tr("响应尚未到达")
		}
		if a.state.record.Tunnel && request {
			body.Notice = ""
		}
		notice := tr(body.Notice)
		if body.Info.ID != "" && body.Info.State != "complete" {
			notice = fmt.Sprintf(tr("正文状态：%s · 已采集 %d / %d 字节"), body.Info.State, body.Info.Captured, body.Info.Observed)
		}
		if notice != "" {
			ui.Text(c.Key(key+"-notice"), notice).FontSize(11).TextColor(p.muted).Padding(6, 12)
		}
		theme := editor.DefaultTheme(c.Theme())
		theme.Background = p.surface
		theme.Text = p.text
		theme.LineNumber = p.muted.Alpha(.75)
		theme.ActiveLineNumber = theme.LineNumber
		theme.Tokens[editor.TokenKeyword].Color = p.brandText
		theme.Tokens[editor.TokenProperty].Color = p.info
		theme.Tokens[editor.TokenString].Color = p.brandText
		theme.Tokens[editor.TokenNumber].Color = p.warning
		ed.Theme = theme
		ui.Box(c).Grow(1).FillWidth().Padding(8, 0, 0).Children(func() { editor.View(c.Key(key), ed).Label(label + " Raw").Grow(1).FillWidth() })
	})
}

var rawLanguage = &editor.Language{Name: "HTTP", Tokenizer: editor.TokenizerFunc(func(line string, state uint64, tokens []editor.Token) ([]editor.Token, uint64) {
	if state == 0 {
		if line != "" {
			tokens = append(tokens, editor.Token{End: len(line), Kind: editor.TokenKeyword})
		}
		return tokens, 1
	}
	if state == 1 {
		if line == "" {
			return tokens, 2
		}
		if n := strings.IndexByte(line, ':'); n > 0 {
			tokens = append(tokens, editor.Token{End: n + 1, Kind: editor.TokenProperty}, editor.Token{End: len(line), Kind: editor.TokenText})
			return tokens, 1
		}
		return append(tokens, editor.Token{End: len(line), Kind: editor.TokenText}), 1
	}
	jsonLanguage := editor.LanguageNamed("json")
	if jsonLanguage != nil && jsonLanguage.Tokenizer != nil {
		next, st := jsonLanguage.Tokenizer.Tokenize(line, state-2, tokens)
		return next, st + 2
	}
	return append(tokens, editor.Token{End: len(line), Kind: editor.TokenText}), state
})}
