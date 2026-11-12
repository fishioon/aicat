//go:build !nogui

package app

import (
	"context"

	"github.com/egoist/mygo"
	"github.com/fishioon/aicat/assets"
	"github.com/fishioon/aicat/internal/listenaddr"
)

type trayState struct {
	Recording  bool
	Enabled    bool
	Taken      bool
	ProxyLabel string
	Problem    string
	Recovery   bool
	Supported  bool
	Addr       string
	Language   string
}

func (a *application) openTrayPage(route string, proxyAction bool) {
	a.showWindow()
	js := "window.desktop?.navigate(" + quote(route) + ")"
	if proxyAction {
		js += ";window.desktop?.toggleSystemProxy()"
	}
	a.evaluate(js)
}

func (a *application) createTray() error {
	frames, duration, err := assets.LoadTrayAnimation()
	if err != nil {
		return err
	}
	a.trayIcons = [2][]byte{frames[0], frames[len(frames)-1]}
	icon, err := mygo.NewMenuIcon(assets.TrayIcon, true)
	if err != nil {
		return err
	}
	page := func(route string) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { a.openTrayPage(route, false) }
	}
	link := func(url string) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { go func() { _ = mygo.Shell.OpenExternal(url) }() }
	}
	a.trayToggle = &mygo.MenuItem{ID: "capture", Label: tr("采集流量"), Type: mygo.MenuItemCheckbox, Click: func(*mygo.MenuItem, *mygo.Window) { go a.toggleRecording() }}
	a.trayMenu = mygo.NewMenu([]*mygo.MenuItem{
		{ID: "open", Icon: icon, Label: tr("打开 aicat"), Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }},
		mygo.Separator(),
		a.trayToggle,
		{ID: "proxy", Submenu: []*mygo.MenuItem{
			{ID: "proxy-toggle", Click: func(*mygo.MenuItem, *mygo.Window) { a.openTrayPage("proxy", true) }},
			{ID: "proxy-settings", Label: tr("代理设置…"), Click: page("proxy")},
		}},
		{ID: "listener", Submenu: []*mygo.MenuItem{
			{ID: "copy-address", Label: tr("复制代理地址"), Click: func(*mygo.MenuItem, *mygo.Window) {
				go func() {
					_ = a.service.CopyText(context.Background(), "http://"+listenaddr.ClientAddress(a.r.gateway.Status().Addr))
				}()
			}},
			{ID: "listener-settings", Label: tr("代理设置…"), Click: page("proxy")},
		}},
		mygo.Separator(),
		{ID: "connect", Label: tr("接入 Agent…"), Click: page("connect")},
		{ID: "extensions", Label: tr("扩展管理…"), Click: page("extensions")},
		{ID: "settings", Label: tr("设置…"), Accelerator: "CmdOrCtrl+,", Click: page("general")},
		mygo.Separator(),
		{ID: "help", Label: tr("帮助"), Submenu: []*mygo.MenuItem{
			{ID: "manual", Label: tr("使用文档…"), Click: link("https://github.com/fishioon/aicat#readme")},
			{ID: "feedback", Label: tr("反馈问题…"), Click: link("https://github.com/fishioon/aicat/issues")},
		}},
		{ID: "quit", Label: tr("退出 aicat"), Role: mygo.RoleQuit, Accelerator: "CmdOrCtrl+Q"},
	})
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "ai.netcat.aicat.tray", Icon: a.trayIcons[0], IconIsTemplate: true, ToolTip: "aicat"})
	a.tray = tray
	if err == nil {
		a.trayMotion = &trayIconMotion{tray: tray, frames: frames, duration: duration, observe: trayMotionObserver(a)}
		a.trayMotion.frame.Store(0)
		a.updateTray()
		open := func() { go a.clickTray(true) }
		a.tray.OnClick(open)
		a.tray.OnRightClick(open)
	}
	return err
}

func (a *application) clickTray(openMenu bool) {
	a.mu.Lock()
	if a.quitting.Load() {
		a.mu.Unlock()
		return
	}
	a.updateTray()
	if mygo.Theme.Preferences().ReduceMotion {
		a.trayMotion.rest(a.trayState.Taken)
	} else {
		a.trayMotion.play(a.trayState.Taken)
	}
	a.mu.Unlock()
	if openMenu {
		a.trayMenu.Popup(nil)
	}
}

func (a *application) updateTray() {
	if a.tray == nil {
		return
	}
	proxy := a.r.connection.status()
	taken := proxy.Enabled && proxy.Problem == "" && a.r.gateway.Status().State == "ready"
	label := "系统代理：未接管"
	if taken {
		label = "系统代理：已接管"
	}
	next := trayState{a.recording, a.enabled, taken, label, proxy.Problem, proxy.Recovery, proxy.Supported, a.r.gateway.Status().Addr, mygo.App.Locale()}
	a.trayToggle.SetChecked(a.enabled && a.recording)
	a.trayToggle.SetEnabled(a.enabled && !a.pending.Load())
	if a.trayState != nil && *a.trayState == next {
		return
	}
	previous := a.trayState
	a.trayState = &next
	a.trayToggle.SetLabel(tr("采集流量"))
	proxyItem := a.trayMenu.ItemByID("proxy")
	proxyItem.SetLabel(tr(label))
	proxyItem.SetSubtitle(tr(proxy.Problem))
	toggle := a.trayMenu.ItemByID("proxy-toggle")
	action := "启用系统代理…"
	if proxy.Enabled && proxy.Problem == "" {
		action = "关闭系统代理"
	} else if proxy.Recovery {
		action = "处理代理设置…"
	}
	toggle.SetLabel(tr(action))
	toggle.SetEnabled(proxy.Supported && !proxy.OwnedElsewhere)
	a.trayMenu.ItemByID("listener").SetLabel(tr("监听于 ") + next.Addr)
	for id, label := range map[string]string{"open": "打开 aicat", "proxy-settings": "代理设置…", "copy-address": "复制代理地址", "listener-settings": "代理设置…", "connect": "接入 Agent…", "extensions": "扩展管理…", "settings": "设置…", "help": "帮助", "manual": "使用文档…", "feedback": "反馈问题…", "quit": "退出 aicat"} {
		a.trayMenu.ItemByID(id).SetLabel(tr(label))
	}
	if previous == nil || previous.Taken != taken {
		a.trayMotion.rest(taken)
	}
	a.tray.SetToolTip("aicat · " + tr(label))
}
