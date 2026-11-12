//go:build !nogui

package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/fishioon/aicat/assets"
	"github.com/fishioon/aicat/internal/app/locales"
	"github.com/fishioon/aicat/internal/certificate"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/ui"
)

// application is only the desktop adapter. Business services never import MyGo.
type application struct {
	service                          *Desktop
	ready                            atomic.Bool
	revision                         atomic.Uint64
	previewNotify                    func(StateChange)
	trayStatus                       *mygo.MenuItem
	r                                *runtime
	win                              *mygo.Window
	tray                             *mygo.Tray
	trayMenu                         *mygo.Menu
	trayToggle                       *mygo.MenuItem
	trayOpen, traySettings, trayQuit *mygo.MenuItem
	mu                               sync.Mutex
	recording, enabled               bool
	quitting                         atomic.Bool
	pending                          atomic.Bool
	certificate                      certificate.Status
	certificateError                 error
	checkedAt                        time.Time
}

func tr(s string) string { return locales.Text(s) }
func Run(o Options) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &application{recording: true}
	a.service = &Desktop{app: a}
	if err := os.Setenv("MYGO_LOCALE", "zh-Hans"); err != nil {
		return err
	}
	mygo.App.SetLanguages("zh-Hans", "en")
	mygo.App.SetName("aicat")
	mygo.Bind(a.service)
	mygo.SetFrontend(ui.Files())
	mygo.App.SetMenu(mygo.NewMenu(append([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}, {Role: mygo.RoleWindowMenu}}, automationMenus(a)...)))
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if a.quitting.Load() {
			return
		}
		if !a.ready.Load() {
			a.quitting.Store(true)
			cancel()
			return
		}
		e.PreventDefault()
		if !a.pending.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer a.pending.Store(false)
			a.mu.Lock()
			defer a.mu.Unlock()
			if err := a.r.connection.prepareQuit(); err != nil {
				a.showWindow()
				a.evaluate("window.desktop?.problem(" + quote(err.Error()) + ")")
				return
			}
			a.quitting.Store(true)
			mygo.App.Quit()
		}()
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	background := backgroundRun()
	if background {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	} else {
		mygo.App.OnActivate(func(bool) { a.showWindow() })
	}
	type startupResult struct {
		runtime     *runtime
		stopPreview func()
		err         error
	}
	result := make(chan startupResult, 1)
	var starting atomic.Bool
	var windowErr error
	// Binding generation runs App.Run without invoking WhenReady: no config,
	// listener, certificate, OS operation or preview is touched by generation.
	mygo.App.WhenReady(func() {
		starting.Store(true)
		go func() {
			r, err := start(ctx, o)
			if err != nil {
				result <- startupResult{err: err}
				a.quitting.Store(true)
				mygo.App.Quit()
				return
			}
			a.r = r
			a.enabled = r.initialTrafficEnabled
			// model list syncs end on their own time
			r.providers.OnChange(a.notifyState)
			stop, err := desktopPreview(a.service)
			result <- startupResult{runtime: r, stopPreview: stop, err: err}
			if err != nil {
				a.quitting.Store(true)
				mygo.App.Quit()
				return
			}
			if ctx.Err() != nil {
				return
			}
			mygo.RunOnMain(func() {
				a.applyPreferences(r.initialPreferences)
				if windowErr = a.createTray(); windowErr != nil {
					a.quitting.Store(true)
					mygo.App.Quit()
					return
				}
				a.ready.Store(true)
				a.win = mygo.NewWindow(mygo.WindowOptions{
					Title: "aicat", Width: 1180, Height: 900, MinWidth: 900, MinHeight: 600,
					TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: 44,
					TrafficLightPosition: &mygo.Point{X: 14, Y: 15},
					URL:                  "/",
					Hidden:               background,
				})
				if !background {
					a.win.OnReadyToShow(func() { a.win.Focus(); mygo.App.Focus() })
				}
				a.win.OnClose(func(e *mygo.CloseEvent) {
					if !a.quitting.Load() {
						e.PreventDefault()
						a.win.Hide()
					}
				})
			})
		}()
	})
	err := mygo.App.Run()
	cancel()
	if !starting.Load() {
		return err
	}
	started := <-result
	if started.stopPreview != nil {
		started.stopPreview()
	}
	if started.runtime != nil {
		err = errors.Join(err, started.runtime.close())
	}
	return errors.Join(err, started.err, windowErr)
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }
func (a *application) evaluate(js string) {
	mygo.RunOnMain(func() {
		if a.win != nil {
			page := a.win.Page()
			go func() { _, _ = page.Eval(js) }()
		}
	})
}
func (a *application) showWindow() {
	mygo.RunOnMain(func() {
		if a.win != nil {
			a.win.Show()
			if a.win.IsMinimized() {
				a.win.Restore()
			}
			a.win.Focus()
			mygo.App.Focus()
		}
	})
}
func (a *application) applyPreferences(p config.Preferences) {
	language := p.Language
	if language == "system" {
		language = ""
	}
	mygo.App.SetLocale(language)
	mygo.Theme.SetSource(mygo.ThemeSource(p.Theme))
}
func (a *application) createTray() error {
	a.trayOpen = &mygo.MenuItem{Label: tr("打开 aicat"), Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }}
	a.trayToggle = &mygo.MenuItem{Label: tr("暂停采集"), Click: func(*mygo.MenuItem, *mygo.Window) { go a.toggleRecording() }}
	a.traySettings = &mygo.MenuItem{Label: tr("设置"), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow(); a.evaluate("window.desktop?.navigate('proxy')") }}
	a.trayQuit = &mygo.MenuItem{Label: tr("退出"), Role: mygo.RoleQuit, Accelerator: "CmdOrCtrl+Q"}
	a.trayStatus = &mygo.MenuItem{Label: "aicat", Subtitle: a.r.gateway.Status().Addr, Disabled: true}
	a.trayMenu = mygo.NewMenu([]*mygo.MenuItem{a.trayStatus, mygo.Separator(), a.trayOpen, a.trayToggle, a.traySettings, mygo.Separator(), a.trayQuit})
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "ai.netcat.aicat.tray", Icon: assets.TrayIcon, IconIsTemplate: true, ToolTip: "aicat", Menu: a.trayMenu})
	a.tray = tray
	a.updateTray()
	return err
}
func (a *application) toggleRecording() {
	defer a.notifyState()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.enabled && !a.pending.Load() {
		a.recording = !a.recording
		a.r.capture.SetRecording(a.recording)
		a.updateTray()
	}
}
func (a *application) updateTray() {
	if a.trayToggle == nil {
		return
	}
	label := "暂停采集"
	if !a.recording {
		label = "继续采集"
	}
	if !a.enabled {
		label = "流量功能已关闭"
	}
	a.trayStatus.SetSubtitle(a.r.gateway.Status().Addr)
	a.trayToggle.SetLabel(tr(label))
	a.trayToggle.SetEnabled(a.enabled)
	a.trayOpen.SetLabel(tr("打开 aicat"))
	a.traySettings.SetLabel(tr("设置"))
	a.trayQuit.SetLabel(tr("退出"))
}
