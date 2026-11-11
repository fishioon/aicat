//go:build !nogui

package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
	"github.com/fishioon/aicat/internal/config"
)

func (a *application) trafficExtensionControls(c *ui.Context, p palette) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		status := "已关闭"
		if a.trafficEnabled {
			status = "已开启"
		}
		ui.Text(c.Key("feature-traffic-status"), tr(status)).FontSize(12).TextColor(p.muted)
		enabled := a.trafficEnabled
		if ui.Switch(c.Key("feature-traffic"), &enabled).Label(locales.EnableTraffic()).Disabled(a.featureSaving).Changed() {
			a.setTrafficEnabled(enabled)
		}
		if a.featureSaving {
			ui.Text(c, locales.Saving()).FontSize(textCaption).TextColor(p.muted)
		}
	})
	if a.featureError != "" {
		ui.Text(c.Key("feature-error"), tr(a.featureError)).TextColor(p.danger)
	}
}

func (a *application) setTrafficEnabled(enabled bool) {
	if a.featureSaving || a.trafficEnabled == enabled {
		return
	}
	a.featureSaving = true
	a.featureError = ""
	go func() {
		err := config.SetTrafficEnabled(a.r.home, enabled)
		a.post(func() {
			a.featureSaving = false
			if err != nil {
				a.featureError = fmt.Sprintf("无法保存流量功能设置：%v", err)
				return
			}
			a.trafficEnabled = enabled
			a.r.capture.SetRecording(enabled && a.recording)
			a.updateRecordingMenu()
			if !enabled {
				a.clearSelection()
				if !a.settings && a.extension == "" {
					a.navigate(location{extension: "traffic"})
				}
			}
		})
	}()
}
