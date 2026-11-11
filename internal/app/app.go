//go:build !nogui

package app

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/traffic"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type application struct {
	sidebarHidden, profileOpen, recording bool
	trafficEnabled, featureSaving         bool
	featureError                          string
	settingsPage                          settingsPage
	extension                             string
	extensions                            extensionsState
	preferences                           config.Preferences
	navigation                            navigationState
	preferencesSaving                     bool
	preferencesError                      string
	host, path, sideQuery                 string
	clientApp                             string
	apps                                  []appGroup
	appsExpanded                          bool
	pins                                  map[string]bool
	domains                               map[string]*domainNode
	expanded                              map[string]bool
	domainsExpanded, pinsExpanded         bool
	selectionSource                       string
	pinSaving                             bool
	sidebarProblem                        string
	searchHandle                          ui.Handle
	copyAnchors                           map[string]ui.Element
	routes                                []location
	routeAt                               int

	win         *mygo.Window
	tray        *mygo.Tray
	trayToggle  *mygo.MenuItem
	trayMenu    *mygo.Menu
	r           *runtime
	ctx         context.Context
	state       trafficState
	settings    bool
	quitting    bool
	decrypting  string
	security    securityState
	quitPending bool
}
type trafficState struct {
	bodyRequest, bodyResponse rawBody

	items                     []traffic.Summary
	visible                   []traffic.Summary
	query                     string
	selected                  int
	selectedID                traffic.ID
	generation                uint64
	revision                  uint64
	record                    *traffic.Record
	request, response         *editor.State
	problem                   string
	table                     ui.ListState
	split, detailSplit        float32
	requestView, responseView int
}

func Run(o Options) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := start(ctx, o)
	if err != nil {
		return err
	}
	initial := location{source: "domain"}
	if !r.initialTrafficEnabled {
		initial = location{extension: "traffic"}
	}
	a := &application{r: r, ctx: ctx, recording: true, trafficEnabled: r.initialTrafficEnabled, settings: initial.settings, settingsPage: initial.page, pins: make(map[string]bool), expanded: make(map[string]bool), appsExpanded: true, domainsExpanded: true, pinsExpanded: true, selectionSource: "domain", routes: []location{initial}, state: trafficState{selected: -1, split: 48, detailSplit: 50}}
	a.extension = initial.extension
	a.preferences = r.initialPreferences
	a.navigation.pinned = r.initialNavigationPins
	for _, host := range r.initialPins {
		a.pins[strings.ToLower(host)] = true
	}
	// MYGO_LOCALE makes SetLocale process-local. aicat.yaml owns persistence;
	// do not also write AppleLanguages or MyGo's separate locale file.
	if err := os.Setenv("MYGO_LOCALE", "zh-Hans"); err != nil {
		return errors.Join(err, r.close())
	}
	mygo.App.SetLanguages("zh-Hans", "en")
	mygo.Theme.SetSource(mygo.ThemeSource(a.preferences.Theme))
	a.state.request = &editor.State{Buffer: editor.NewBuffer(""), ReadOnly: true, Font: ui.Font{Family: fontCode, Size: textCode, Weight: 400, Lang: "zh-Hans"}, LineHeight: 1.65, TabSize: 2, NoActiveLine: true, NoFolding: true, LineNumbers: editor.LineNumbersOn, NoIndentGuides: true, NoScrollPastEnd: true, SoftWrap: true}
	a.state.response = &editor.State{Buffer: editor.NewBuffer(""), ReadOnly: true, Font: ui.Font{Family: fontCode, Size: textCode, Weight: 400, Lang: "zh-Hans"}, LineHeight: 1.65, TabSize: 2, NoActiveLine: true, NoFolding: true, LineNumbers: editor.LineNumbersOn, NoIndentGuides: true, NoScrollPastEnd: true, SoftWrap: true}
	a.state.request.Buffer.SetLanguage(rawLanguage)
	a.state.response.Buffer.SetLanguage(rawLanguage)
	mygo.App.SetName("aicat")
	menuItems := []*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}, {Role: mygo.RoleWindowMenu}}
	mygo.App.SetMenu(mygo.NewMenu(append(menuItems, automationMenus(a)...)))
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if a.quitting {
			return
		}
		e.PreventDefault()
		if a.quitPending || a.certificateBusy() || a.security.listenerBusy {
			return
		}
		a.quitPending = true
		go func() {
			err := r.connection.prepareQuit()
			a.post(func() {
				a.quitPending = false
				if err != nil {
					a.security.problem = err.Error()
					a.navigate(location{settings: true})
					return
				}
				a.quitting = true
				mygo.App.Quit()
			})
		}()
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	mygo.App.OnActivate(func(bool) { a.showWindow() })
	var startupErr error
	mygo.App.WhenReady(func() {
		a.applyLanguage()
		mygo.App.OnLocaleChanged(func(string) {
			a.refreshTrayLanguage()
			if a.win != nil {
				a.renderDetails()
				a.win.Invalidate()
			}
		})
		a.win = mygo.NewWindow(mygo.WindowOptions{Title: "aicat", Width: 1180, Height: 900, MinWidth: 900, MinHeight: 600, TitleBarStyle: mygo.TitleBarHidden, Content: ui.View(a.view)})
		a.win.OnFocus(func() { a.win.Update(func() { a.refreshSecurity() }) })
		a.refreshSecurity()
		if startupErr = a.createTray(); startupErr != nil {
			mygo.App.Quit()
			return
		}
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if !a.quitting {
				e.PreventDefault()
				a.win.Hide()
			}
		})
		go a.watch()
	})
	err = mygo.App.Run()
	cancel()
	return errors.Join(startupErr, err, r.close())
}

// Only Window.Update callbacks and view handlers touch UI state.
func (a *application) post(fn func()) {
	if a.ctx.Err() == nil {
		a.win.Update(fn)
	}
}
func (a *application) watch() {
	for a.ctx.Err() == nil {
		page, sub, err := a.r.traffic.Watch(a.ctx, traffic.Query{Limit: 2000})
		if err != nil {
			a.post(func() { a.state.problem = err.Error() })
			return
		}
		a.accept(page)
		ticker := time.NewTicker(50 * time.Millisecond)
		dirty := false
		live := true
		for live {
			select {
			case <-a.ctx.Done():
				sub.Close()
				ticker.Stop()
				return
			case _, ok := <-sub.Changes:
				if !ok {
					live = false
				} else {
					dirty = true
				}
			case <-ticker.C:
				if dirty {
					page, err = a.r.traffic.List(a.ctx, traffic.Query{Limit: 2000})
					if err == nil {
						a.accept(page)
					}
					dirty = false
				}
			}
		}
		ticker.Stop()
		err = sub.Err()
		sub.Close()
		if !errors.Is(err, traffic.ErrSlowSubscriber) {
			return
		}
	}
}
func (a *application) accept(p traffic.Page) {
	sort.Slice(p.Items, func(i, j int) bool { return p.Items[i].ID < p.Items[j].ID })
	a.post(func() {
		a.state.items = p.Items
		a.domains = buildDomainTrees(p.Items)
		a.apps = buildAppGroups(p.Items)
		a.filter()
		found := false
		for _, r := range p.Items {
			if r.ID == a.state.selectedID {
				found = true
				if r.Revision > a.state.revision {
					a.selectID(r.ID)
				}
				break
			}
		}
		if !found && a.state.selectedID != 0 {
			a.clearSelection()
		}
	})
}
func (a *application) filter() {
	st := &a.state
	st.visible = nil
	st.selected = -1
	matcher := newURLMatcher(st.query)
	for _, r := range st.items {
		if a.selectionSource == "app" && r.ClientApp.Key() != a.clientApp {
			continue
		}
		if a.host != "" {
			u, _ := url.Parse(r.URL)
			if u == nil || !strings.EqualFold(u.Host, a.host) || !withinPath(u.EscapedPath(), a.path) {
				continue
			}
		}
		if matcher.matches(r.URL) {
			if r.ID == st.selectedID {
				st.selected = len(st.visible)
			}
			st.visible = append(st.visible, r)
		}
	}
	if st.selectedID != 0 && st.selected < 0 {
		a.clearSelection()
	}
}

func (a *application) clearSelection() {
	st := &a.state
	st.selectedID, st.selected, st.revision = 0, -1, 0
	st.generation++
	st.record = nil
	st.problem = ""
	st.bodyRequest, st.bodyResponse = rawBody{}, rawBody{}
	st.request.Buffer.Reset("")
	st.response.Buffer.Reset("")
}
func (a *application) selectID(id traffic.ID) {
	st := &a.state
	if st.selectedID != id {
		st.record = nil
		st.revision = 0
		st.problem = ""
	}
	st.selectedID = id
	st.generation++
	generation := st.generation
	go func() {
		rec, err := a.r.traffic.Get(a.ctx, id)
		if err != nil {
			a.post(func() {
				if st.generation == generation {
					if errors.Is(err, traffic.ErrNotFound) {
						a.clearSelection()
					} else {
						st.problem = err.Error()
					}
				}
			})
			return
		}
		request := a.bodyText(rec.RequestReceived.Body)
		response := rawBody{Notice: "响应尚未到达"}
		if rec.ResponseSent != nil {
			response = a.bodyText(rec.ResponseSent.Body)
		}
		a.post(func() {
			if st.selectedID != id || st.generation != generation {
				return
			}
			st.record = &rec
			st.revision = rec.Revision
			st.problem = ""
			st.bodyRequest = request
			st.bodyResponse = response
			a.renderDetails()
		})
	}()
}

type location struct {
	settings       bool
	extension      string
	extensionQuery string
	host           string
	path           string
	clientApp      string
	query          string
	sideQuery      string
	request        traffic.ID
	source         string
	page           settingsPage
}

func (a *application) navigate(next location) {
	if !next.settings && next.extension == "" && !a.trafficEnabled {
		next = location{extension: "traffic"}
	}
	if next.source == "" {
		next.source = "domain"
	}
	if next.request == 0 {
		next.request = a.state.selectedID
	}
	next.sideQuery = a.sideQuery
	if next.extension != "" {
		next.extensionQuery = a.extensions.query
	}
	current := a.currentLocation()
	a.routes[a.routeAt] = current
	if current == next {
		return
	}
	a.routes = a.routes[:a.routeAt+1]
	a.routes = append(a.routes, next)
	a.routeAt = len(a.routes) - 1
	a.settings = next.settings
	a.settingsPage = next.page
	a.extension = next.extension
	a.extensions.query = next.extensionQuery
	if next.settings {
		a.refreshSecurity()
	}
	a.host, a.path, a.selectionSource = next.host, next.path, next.source
	a.clientApp = next.clientApp
	a.state.query = next.query
	a.sideQuery = next.sideQuery
	a.revealFilter()
	a.filter()
	a.restoreRequest(next.request)
}
func (a *application) currentLocation() location {
	return location{settings: a.settings, extension: a.extension, extensionQuery: a.extensions.query, host: a.host, path: a.path, clientApp: a.clientApp, query: a.state.query, sideQuery: a.sideQuery, request: a.state.selectedID, source: a.selectionSource, page: a.settingsPage}
}

func (a *application) travelIndex(delta int) int {
	for n := a.routeAt + delta; n >= 0 && n < len(a.routes); n += delta {
		if a.routes[n].settings || a.routes[n].extension != "" || a.trafficEnabled {
			return n
		}
	}
	return -1
}
func (a *application) travel(delta int) {
	n := a.travelIndex(delta)
	if n < 0 {
		return
	}
	a.routes[a.routeAt] = a.currentLocation()
	a.routeAt = n
	v := a.routes[n]
	a.settings = v.settings
	a.settingsPage = v.page
	a.extension = v.extension
	a.extensions.query = v.extensionQuery
	if v.settings {
		a.refreshSecurity()
	}
	a.host, a.path, a.selectionSource = v.host, v.path, v.source
	a.clientApp = v.clientApp
	a.state.query = v.query
	a.sideQuery = v.sideQuery
	a.revealFilter()
	a.filter()
	a.restoreRequest(v.request)
}

// Only user selections enter navigation history; asynchronous record revisions
// refresh the current detail without creating history entries.
func (a *application) chooseRequest(id traffic.ID) {
	next := a.currentLocation()
	next.request = id
	a.navigate(next)
}
func (a *application) restoreRequest(id traffic.ID) {
	for i, r := range a.state.visible {
		if r.ID == id {
			a.state.selected = i
			a.state.table.ScrollIntoView(i)
			if a.state.selectedID != id {
				a.selectID(id)
			}
			return
		}
	}
	a.clearSelection()
}
