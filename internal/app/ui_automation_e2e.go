//go:build !nogui && e2e

package app

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

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

type trayFrameEvent struct {
	Frame      int    `json:"frame"`
	Generation uint64 `json:"generation"`
	AtMS       int64  `json:"atMs"`
}

type trayFrameTrace struct {
	mu     sync.Mutex
	frames []trayFrameEvent
}

var trayMotionTraces sync.Map

func trayMotionObserver(a *application) func(int, uint64) {
	trace := &trayFrameTrace{}
	trayMotionTraces.Store(a, trace)
	return func(frame int, generation uint64) {
		trace.mu.Lock()
		defer trace.mu.Unlock()
		if len(trace.frames) == 512 {
			trace.frames = append([]trayFrameEvent(nil), trace.frames[256:]...)
		}
		trace.frames = append(trace.frames, trayFrameEvent{frame, generation, time.Now().UnixMilli()})
	}
}

// Inspect the actual menu model without opening a native menu or window.
func (a *application) previewTray(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.ready.Load() || a.trayMenu == nil {
		http.Error(w, "not ready", 503)
		return
	}
	if r.Method == http.MethodPost {
		var in struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil {
			http.Error(w, "invalid action", 400)
			return
		}
		switch in.Action {
		case "refresh":
			a.updateTray()
		case "capture":
			if !a.trayToggle.IsEnabled() {
				http.Error(w, "disabled", 409)
				return
			}
			a.trayToggle.Click(a.trayToggle, nil)
		case "click":
			// The native tray callback uses this same handler with openMenu=true.
			// Headless verification exercises the animation without a popup.
			go a.clickTray(false)
		default:
			http.Error(w, "invalid action", 400)
			return
		}
	} else if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	type item struct {
		ID      string            `json:"id"`
		Label   string            `json:"label"`
		Type    mygo.MenuItemType `json:"type"`
		Checked bool              `json:"checked"`
		Enabled bool              `json:"enabled"`
		Submenu []item            `json:"submenu,omitempty"`
	}
	var items func([]*mygo.MenuItem) []item
	items = func(entries []*mygo.MenuItem) []item {
		result := make([]item, 0, len(entries))
		for _, entry := range entries {
			result = append(result, item{entry.ID, entry.Label, entry.Type, entry.IsChecked(), entry.IsEnabled(), items(entry.Submenu)})
		}
		return result
	}
	index := 0
	if a.trayState.Taken {
		index = 1
	}
	var trace []trayFrameEvent
	if value, ok := trayMotionTraces.Load(a); ok {
		log := value.(*trayFrameTrace)
		log.mu.Lock()
		trace = append(trace, log.frames...)
		log.mu.Unlock()
	}
	motion := a.trayMotion
	frame := int(motion.frame.Load())
	w.Header().Set("Content-Type", "application/json")
	_ = previewEncode(w, map[string]any{
		"items": items(a.trayMenu.Items()), "taken": a.trayState.Taken, "icon": a.trayIcons[index],
		"frame": frame, "currentIcon": motion.frames[frame], "frameCount": len(motion.frames),
		"animating": motion.running.Load() != 0, "generation": motion.generation.Load(),
		"durationMs": motion.duration.Milliseconds(), "trace": trace,
		"reduceMotion": mygo.Theme.Preferences().ReduceMotion,
	})
}
