//go:build !nogui

package app

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *application) listenerControls(c *ui.Context, p palette) {
	s := &a.security
	if s.listenerHost == "" {
		s.listenerHost, s.listenerPort, _ = net.SplitHostPort(a.r.gateway.Status().Addr)
	}
	addresses := s.listenerAddresses
	if len(addresses) == 0 {
		addresses = []string{"127.0.0.1", "0.0.0.0"}
	}
	if !slices.Contains(addresses, s.listenerHost) {
		addresses = append(slices.Clone(addresses), s.listenerHost)
	}
	port, err := strconv.Atoi(s.listenerPort)
	valid := err == nil && port >= 1 && port <= 65535
	changed := net.JoinHostPort(s.listenerHost, strconv.Itoa(port)) != a.r.gateway.Status().Addr
	busy := s.listenerBusy || s.loading || s.proxyBusy || a.certificateBusy()
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("监听地址")).Grow(1).TextColor(p.muted)
		if ui.Select(c.Key("listener-address"), &s.listenerHost, addresses).Label(tr("监听地址")).Width(190).Font(fontCode).FontSize(13).Disabled(busy).Changed() {
			s.listenerProblem = ""
		}
		ui.Text(c, ":").TextColor(p.muted)
		if ui.TextInput(c.Key("listener-port"), &s.listenerPort).Label(tr("端口")).Width(78).Font(fontCode).FontSize(13).Disabled(busy).Changed() {
			s.listenerProblem = ""
		}
		label := tr("应用")
		if s.listenerBusy {
			label = tr("正在应用…")
		}
		if ui.Button(c.Key("listener-apply"), label).Disabled(busy || !valid || !changed).Clicked() {
			a.applyListener()
		}
	})
	if !valid {
		ui.Text(c.Key("listener-error"), tr("端口须为 1–65535 的整数")).FontSize(11).TextColor(p.danger)
	} else if s.listenerProblem != "" {
		ui.Text(c.Key("listener-error"), s.listenerProblem).FontSize(11).TextColor(p.danger)
	}
}

func (a *application) applyListener() {
	s := &a.security
	if s.listenerBusy {
		return
	}
	port, err := strconv.Atoi(s.listenerPort)
	if err != nil || port < 1 || port > 65535 {
		return
	}
	address := net.JoinHostPort(s.listenerHost, strconv.Itoa(port))
	s.listenerBusy = true
	s.listenerProblem = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
		defer cancel()
		err := a.r.connection.rebind(ctx, a.r.gateway, address)
		status := a.r.connection.status()
		a.post(func() {
			s.listenerBusy = false
			s.proxy = status
			if err != nil {
				s.listenerProblem = fmt.Sprintf(tr("应用监听设置失败：%v"), err)
				return
			}
			s.listenerHost, s.listenerPort, _ = net.SplitHostPort(a.r.gateway.Status().Addr)
			a.refreshTrayLanguage()
		})
	}()
}
