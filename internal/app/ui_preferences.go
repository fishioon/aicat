//go:build !nogui

package app

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
	"github.com/fishioon/aicat/internal/config"
)

func tr(source string) string { return locales.Text(source) }

func (a *application) applyLanguage() {
	tag := a.preferences.Language
	if tag == "system" {
		tag = ""
	}
	mygo.App.SetLocale(tag)
}

func (a *application) generalSettings(c *ui.Context, p palette) {
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(28).Gap(20).Children(func() {
			ui.Text(c, locales.General()).FontSize(textTitle).FontWeight(600)
			ui.Column(c).FillWidth().MaxWidth(680).Gap(0).Children(func() {
				ui.Row(c).FillWidth().Padding(18, 0).Gap(16).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					ui.Column(c).Grow(1).Gap(6).Children(func() {
						ui.Text(c, locales.Language()).FontSize(14).FontWeight(500)
						ui.Text(c, locales.ChooseTheInterfaceLanguageAppliesImmediately()).FontSize(12).TextColor(p.muted)
					})
					labels := []string{locales.FollowSystem(), "简体中文", "English"}
					values := []string{"system", "zh-Hans", "en"}
					selected := labels[0]
					for i, v := range values {
						if a.preferences.Language == v {
							selected = labels[i]
						}
					}
					if ui.Select(c.Key("preference-language"), &selected, labels).Label(locales.Language()).Width(170).Disabled(a.preferencesSaving).Changed() {
						next := a.preferences
						for i, label := range labels {
							if selected == label {
								next.Language = values[i]
							}
						}
						a.setPreferences(next)
					}
				})
				ui.Row(c).FillWidth().Padding(18, 0).Gap(16).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
					ui.Column(c).Grow(1).Gap(6).Children(func() {
						ui.Text(c, locales.Theme()).FontSize(14).FontWeight(500)
						ui.Text(c, locales.ChooseTheAppAppearanceAppliesImmediately()).FontSize(12).TextColor(p.muted)
					})
					ui.Row(c).Gap(6).Children(func() {
						for _, item := range []struct{ id, label string }{{"system", locales.FollowSystem()}, {"light", locales.Light()}, {"dark", locales.Dark()}} {
							b := ui.Button(c.Key("preference-theme-"+item.id), item.label).Label(item.label).Disabled(a.preferencesSaving).Padding(7, 12).Radius(6)
							if a.preferences.Theme == item.id {
								b.Background(p.brandSoft).TextColor(p.brandText).Border(1, p.brand)
							}
							if b.Clicked() {
								next := a.preferences
								next.Theme = item.id
								a.setPreferences(next)
							}
						}
					})
				})
				if a.preferencesSaving {
					ui.Text(c, locales.Saving()).Padding(12, 0).TextColor(p.muted)
				}
				if a.preferencesError != "" {
					ui.Text(c.Key("preferences-error"), a.preferencesError).Padding(12, 0).TextColor(p.danger)
				}
			})
		})
	})
}

func (a *application) setPreferences(next config.Preferences) {
	if a.preferencesSaving || next == a.preferences {
		return
	}
	a.preferencesSaving, a.preferencesError = true, ""
	go func() {
		err := config.SetPreferences(a.r.home, next)
		a.post(func() {
			a.preferencesSaving = false
			if err != nil {
				a.preferencesError = fmt.Sprintf(locales.UnableToSaveSettingsV(), err)
				return
			}
			a.preferences = next
			a.applyLanguage()
			mygo.Theme.SetSource(mygo.ThemeSource(next.Theme))
			a.renderDetails()
			a.refreshTrayLanguage()
		})
	}()
}
