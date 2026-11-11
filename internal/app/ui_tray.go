//go:build !nogui

package app

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/fishioon/aicat/assets"
	"github.com/fishioon/aicat/internal/app/locales"
)

func (a *application) createTray() error {
	if err := a.buildTrayMenu(); err != nil {
		return err
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID: "ai.netcat.aicat.tray", Icon: assets.TrayIcon, IconIsTemplate: true,
		ToolTip: tr("aicat · 正在采集"), Menu: a.trayMenu,
	})
	if err != nil {
		return fmt.Errorf("create aicat tray: %w", err)
	}
	a.tray = tray
	a.updateRecordingMenu()
	return nil
}

func (a *application) buildTrayMenu() error {
	brand, err := mygo.NewMenuIcon(assets.TrayIcon, true)
	if err != nil {
		return fmt.Errorf("create aicat menu icon: %w", err)
	}
	status := &mygo.MenuItem{
		Label:    "aicat",
		Subtitle: locales.ProxyRunning() + a.r.gateway.Status().Addr,
		Icon:     brand,
		Disabled: true,
	}
	a.trayToggle = &mygo.MenuItem{
		Label:    locales.PauseCapture(),
		Subtitle: locales.RecordingRequestsAndResponses(),
		Click:    func(*mygo.MenuItem, *mygo.Window) { a.toggleRecording() },
	}
	a.trayMenu = mygo.NewMenu([]*mygo.MenuItem{
		status,
		mygo.Separator(),
		a.trayToggle,
		mygo.Separator(),
		{Label: locales.OpenAicat(), Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }},
		{Label: locales.Settings2(), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.showWindow()
			a.win.Update(func() { a.navigate(location{settings: true, page: settingsGeneral}) })
		}},
		mygo.Separator(),
		{Label: locales.QuitAicat(), Role: mygo.RoleQuit, Accelerator: "CmdOrCtrl+Q"},
	})
	return nil
}

func (a *application) refreshTrayLanguage() {
	if a.tray == nil {
		return
	}
	if err := a.buildTrayMenu(); err != nil {
		a.preferencesError = err.Error()
		return
	}
	a.tray.SetMenu(a.trayMenu)
	a.updateRecordingMenu()
}

func (a *application) showWindow() {
	if a.win == nil {
		return
	}
	a.win.Show()
	if a.win.IsMinimized() {
		a.win.Restore()
	}
	a.win.Focus()
}

// Both entry points run on the UI thread. The window and tray reflect the
// same capture switch; pausing does not stop the proxy or in-flight records.
func (a *application) toggleRecording() {
	if !a.trafficEnabled {
		return
	}
	a.recording = !a.recording
	a.r.capture.SetRecording(a.recording)
	a.updateRecordingMenu()
	a.win.Invalidate()
}

func (a *application) updateRecordingMenu() {
	label, tip, subtitle := "暂停采集", "aicat · 正在采集", "正在记录请求与响应"
	if !a.recording {
		label, tip = "继续采集", "aicat · 已暂停采集"
		subtitle = "采集已暂停，代理继续转发"
	}
	if !a.trafficEnabled {
		label, tip, subtitle = "流量功能已关闭", "aicat · 代理运行中", "可在扩展的内置列表中重新开启"
	}
	if a.trayToggle != nil {
		a.trayToggle.SetEnabled(a.trafficEnabled)
		a.trayToggle.SetLabel(tr(label))
		a.trayToggle.SetSubtitle(tr(subtitle))
	}
	if a.tray != nil {
		a.tray.SetToolTip(tr(tip))
	}
}
