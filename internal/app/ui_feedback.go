//go:build !nogui

package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/traffic"
)

type copyTarget struct {
	key       string
	requestID traffic.ID
}

func (a *application) copyText(c *ui.Context, key, text string) {
	message, failed := tr("已复制"), false
	if err := mygo.Clipboard.Write(transfer.TextData(text)); err != nil {
		message, failed = tr("复制失败，请重试"), true
	}
	a.copyNotice(c, key, message, failed)
}

func (a *application) copyNotice(c *ui.Context, key, message string, failed bool) {
	kind := "success"
	if failed {
		kind = "error"
	}
	target := copyTarget{key: key, requestID: a.state.selectedID}
	c.AddToast(ui.Toast{ID: "clipboard", Title: message, Type: kind, Data: target, Timeout: 2400 * time.Millisecond})
}

func (a *application) feedback(c *ui.Context, p palette) {
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		for _, t := range toasts {
			target, ok := t.Data.(copyTarget)
			anchor, visible := a.copyAnchors[target.key]
			if !ok || !visible || target.requestID != 0 && target.requestID != a.state.selectedID {
				continue
			}
			toast := ui.ToastBase(c, t)
			if strings.HasPrefix(target.key, "request-row-") {
				toast.Root.AttachTo(anchor, ui.AnchorRight, ui.AnchorRight).Margin(0, 10, 0, 0)
			} else {
				toast.Root.AttachTo(anchor, ui.AnchorLeft, ui.AnchorRight).Margin(0, 6, 0, 0)
			}
			toast.Root.Row().Label(t.Title).
				PassThrough().AlignItems(ui.Center).Gap(5).Padding(4, 8).Radius(6).
				MaxWidth(260).Background(p.surface).Border(1, p.border).Shadow(0, 4, 20, 0, p.text.Alpha(.12)).Children(func() {
				name, color := "check", p.brandText
				if t.Type == "error" {
					name, color = "circle-alert", p.danger
				}
				ui.Icon(c, icon(name)).Size(14, 14).TextColor(color)
				ui.Text(c.Key("feedback-message"), t.Title).FontSize(12).TextColor(p.text)
			})
		}
	})
}
