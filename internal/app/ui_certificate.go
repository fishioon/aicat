//go:build !nogui

package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
)

func (a *application) certificateBusy() bool {
	return a.security.trusting || a.security.certificateAction != "" || a.security.listenerBusy
}

func (a *application) certificateMenu(c *ui.Context) {
	s := &a.security
	ui.PopoverMenuButton(c.Key("certificate-manage"), locales.ManageCertificate(), func(m *ui.Menu) {
		if m.Item(locales.ViewCertificate()).ID("certificate-details").Chosen() {
			s.details = true
		}
		if m.Item(locales.ExportCertificate()).ID("certificate-export").Disabled(s.certificate.PendingDeletion).Chosen() {
			a.exportCertificate()
		}
		if m.Item(locales.ManageInKeychain()).ID("certificate-keychain").Disabled(!s.certificate.Supported).Chosen() {
			a.certificateTask("正在打开钥匙串", func(context.Context) error { return openCertificateKeychain() })
		}
		if m.Item(locales.CheckTrustAgain()).ID("certificate-menu-recheck").Chosen() {
			a.refreshSecurity()
		}
		m.Separator()
		label := "删除证书…"
		if s.certificate.PendingDeletion {
			label = "重试删除…"
		}
		if m.Item(tr(label)).ID("certificate-delete").Danger().Disabled(!s.certificate.Supported).Chosen() {
			s.deleteConfirm = true
		}
	}).Disabled(a.certificateBusy() || s.loading || a.decrypting != "")
}

func (a *application) certificateTask(label string, fn func(context.Context) error) {
	s := &a.security
	if a.certificateBusy() || a.quitPending {
		return
	}
	s.certificateAction = label
	s.generation++
	s.loading = false
	s.problem, s.note = "", ""
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		err := fn(ctx)
		cancel()
		status, checkErr := a.r.connection.inspect(a.ctx)
		a.post(func() {
			s.certificateAction = ""
			s.certificate = status
			s.certificateProblem = errorText(checkErr)
			s.problem = errorText(err)
		})
	}()
}

func (a *application) exportCertificate() {
	a.certificateTask("正在导出", func(context.Context) error {
		path, err := chooseCertificateExport(a.win)
		if err != nil || path == "" {
			return err
		}
		if err = a.r.connection.exportCertificate(path); err != nil {
			return err
		}
		a.post(func() { a.security.note = "证书已导出" })
		return nil
	})
}

func (a *application) certificateDeleteDialog(c *ui.Context, p palette) {
	s := &a.security
	ui.Modal(c.Key("certificate-delete-dialog"), &s.deleteConfirm, func() {
		ui.Column(c).Width(420).Gap(16).Children(func() {
			ui.Text(c, locales.DeleteTheLocalRootCertificate()).FontSize(textBody).FontWeight(500)
			ui.Text(c, locales.RevokeTrustForTheCurrentUserAndDeleteTheRootCertificateAndPrivateKeyExistingProx())
			ui.Text(c, locales.RemoveCertificateCopiesInstalledOnOtherDevicesSeparately()).FontSize(textCaption).TextColor(p.muted)
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c.Key("certificate-delete-cancel"), locales.Cancel()).Clicked() {
					s.deleteConfirm = false
				}
				if ui.Button(c.Key("certificate-delete-confirm"), locales.DeleteCertificate2()).TextColor(p.danger).Clicked() {
					s.deleteConfirm, s.details = false, false
					a.certificateTask("正在删除", a.r.connection.deleteCertificate)
				}
			})
		})
	})
}
