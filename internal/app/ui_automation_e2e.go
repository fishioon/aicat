//go:build !nogui && e2e

package app

import (
	"os"

	"github.com/egoist/mygo"
)

// The official automation plugin can choose application menu items, but does
// not expose status-bar items. This test-build-only entry opens the real native
// tray menu inside the window so accessibility can inspect and choose its items.
func automationMenus(a *application) []*mygo.MenuItem {
	return []*mygo.MenuItem{{Label: "E2E", Submenu: []*mygo.MenuItem{
		{Label: "关闭窗口", Click: func(*mygo.MenuItem, *mygo.Window) { a.win.Close() }},
		{Label: "打开窗口", Click: func(*mygo.MenuItem, *mygo.Window) { a.showWindow() }},
		{Label: "打开托盘菜单", Click: func(*mygo.MenuItem, *mygo.Window) { a.trayMenu.PopupAt(a.win, 180, 160) }},
		{Label: "暂停或继续", Click: func(*mygo.MenuItem, *mygo.Window) { go a.toggleRecording() }},
		{Label: "退出应用", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	}}}
}

// Scenarios driven only through the loopback preview never look at the native
// window. They set AICAT_E2E_BACKGROUND so a run neither shows a window, takes
// focus nor adds a Dock icon while the developer works in other apps.
func backgroundRun() bool { return os.Getenv("AICAT_E2E_BACKGROUND") != "" }
