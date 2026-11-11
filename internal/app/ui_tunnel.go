//go:build !nogui

package app

import (
	"net/url"

	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
)

func (a *application) tunnelContent(c *ui.Context, p palette) {
	record := a.state.record
	target, _ := url.Parse(record.URL)
	host := ""
	if target != nil {
		host = target.Hostname()
	}
	ui.Column(c.Key("tunnel-encrypted")).Fill().Padding(24).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Children(func() {
		ui.Icon(c, icon("lock")).Size(24, 24).TextColor(p.muted)
		ui.Text(c, locales.HttpsContentIsEncrypted()).FontSize(13).TextColor(p.muted)
		ui.Text(c, locales.EnableHttpsDecryptionForThisDomainToInspectFutureRequestsAndResponses()).FontSize(textCaption).TextColor(p.muted)
		enabled := a.r.connection.authority.Available() && a.r.interception.enabled(host)
		if enabled {
			ui.Text(c, locales.DecryptionEnabledWaitingForANewClientConnection()).FontSize(textUI).TextColor(p.brandText)
		} else {
			label := "解密此域名"
			if a.decrypting == host {
				label = "正在启用…"
			}
			if ui.Button(c.Key("enable-domain-decryption"), tr(label)).Label(tr(label)).Disabled(host == "" || a.decrypting != "" || a.certificateBusy()).Clicked() {
				a.requestDecryption(host)
			}
		}
		ui.Text(c, locales.TheClientMustTrustTheAicatCaThisTunnelRemainsInPassthroughMode()).FontSize(textCaption).TextColor(p.muted)
		if ui.Button(c.Key("decryption-settings"), locales.CertificateSettings()).Clicked() {
			a.navigate(location{settings: true, page: settingsProxy})
		}
	})
}
