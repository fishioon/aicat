//go:build !nogui

package app

import (
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
	"github.com/fishioon/aicat/internal/config"
)

type navigationState struct {
	pinned  []string
	open    bool
	saving  bool
	problem string
}

type navigationEntry struct {
	id, symbol, label string
	destination       location
	selected          bool
}

func (a *application) navigationEntries(c *ui.Context, p palette) {
	var entries []navigationEntry
	if a.trafficEnabled {
		entries = append(entries, navigationEntry{
			id: "traffic", symbol: "activity", label: locales.Traffic(),
			selected: !a.settings && a.extension == "",
		})
	}
	selectedInMore := false
	for _, entry := range entries {
		if slices.Contains(a.navigation.pinned, entry.id) {
			a.navigationButton(c, p, entry)
		} else if entry.selected {
			selectedInMore = true
		}
	}
	a.navigationButton(c, p, navigationEntry{
		id: "extensions", symbol: "extensions", label: locales.Extensions(),
		destination: location{extension: "traffic"}, selected: a.extension != "",
	})
	a.navigationMore(c, p, entries, selectedInMore)
}

func (a *application) navigationButton(c *ui.Context, p palette, entry navigationEntry) {
	b := iconButton(c, "nav-"+entry.id, entry.symbol, entry.label).Size(35, 35).Padding(8).Radius(8)
	if entry.selected {
		b.Background(p.brandSoft).TextColor(p.brandText)
	}
	if b.Clicked() {
		a.navigate(entry.destination)
	}
}

func (a *application) navigationMore(c *ui.Context, p palette, entries []navigationEntry, selected bool) {
	st := &a.navigation
	label := locales.MoreNavigation()
	anchor := iconButton(c, "nav-more", "more", label).Size(35, 35).Padding(8).Radius(8)
	anchor.Expanded(st.open)
	if selected {
		anchor.Background(p.brandSoft).TextColor(p.brandText)
	} else if st.open {
		anchor.Background(p.soft)
	}
	if anchor.Clicked() {
		st.open = !st.open
		if st.open {
			a.profileOpen = false
		}
	}
	ui.PopoverBase(c.Key("nav-more-menu"), anchor, &st.open, func(panel ui.Element) {
		panel.Role(ui.RoleMenu).Label(label).AttachTo(anchor, ui.AnchorTopRight, ui.AnchorTopLeft).
			Margin(0, 0, 0, 8).Width(220).Padding(5).Gap(3).Radius(12).Background(p.surface).
			Border(1, p.border).Shadow(0, 6, 24, 0, p.text.Alpha(.15)).Children(func() {
			if len(entries) == 0 {
				ui.Text(c.Key("nav-more-empty"), locales.NoAdditionalNavigation()).Padding(7, 9).TextColor(p.muted)
				return
			}
			ui.Scroll(c).FillWidth().MaxHeight(320).Children(func() {
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					for _, entry := range entries {
						a.navigationMenuRow(c, p, entry)
					}
				})
			})
			if st.problem != "" {
				ui.Text(c.Key("nav-pin-error"), fmt.Sprintf(locales.SaveNavigationError(), st.problem)).
					FillWidth().Padding(6, 8).FontSize(textCaption).TextColor(p.danger)
			}
		})
	})
}

func (a *application) navigationMenuRow(c *ui.Context, p palette, entry navigationEntry) {
	st := &a.navigation
	row := ui.Row(c).FillWidth().Height(36).Gap(3).Padding(0, 3, 0, 0).Radius(7).AlignItems(ui.Center)
	if entry.selected {
		row.Background(p.brandSoft)
	} else if row.Hovered() {
		row.Background(p.soft)
	}
	row.Children(func() {
		open := ui.Button(c.Key("nav-more-"+entry.id), "").Role(ui.RoleMenuItem).Label(entry.label).
			Grow(1).MinWidth(0).Height(36).Padding(0, 9).Background(ui.Transparent).Border(0, ui.Transparent)
		if open.Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(9).Children(func() {
				ui.Icon(c, icon(entry.symbol)).Size(16, 16)
				ui.Text(c, entry.label).Grow(1).MinWidth(0).SingleLine()
			})
		}).Clicked() {
			st.open = false
			if !entry.selected {
				a.navigate(entry.destination)
			}
		}
		pinned := slices.Contains(st.pinned, entry.id)
		pinLabel := locales.PinToNavigation()
		if pinned {
			pinLabel = locales.UnpinFromNavigation()
		}
		pin := iconButton(c, "nav-pin-"+entry.id, "pin", pinLabel).Size(28, 28).Radius(6).
			Label(pinLabel + " · " + entry.label).Disabled(st.saving)
		if pinned {
			pin.Background(p.brandSoft).TextColor(p.brandText)
		} else {
			pin.TextColor(p.muted)
		}
		if pin.Clicked() {
			a.setNavigationPinned(entry.id, !pinned)
		}
	})
}

func (a *application) setNavigationPinned(id string, pin bool) {
	st := &a.navigation
	if st.saving || slices.Contains(st.pinned, id) == pin {
		return
	}
	next := slices.Clone(st.pinned)
	if pin {
		next = append(next, id)
	} else {
		next = slices.DeleteFunc(next, func(pinned string) bool { return pinned == id })
	}
	st.saving, st.problem = true, ""
	go func() {
		err := config.SetPinnedNavigation(a.r.home, next)
		a.post(func() {
			st.saving = false
			if err != nil {
				st.problem = err.Error()
				return
			}
			st.pinned = next
		})
	}()
}
