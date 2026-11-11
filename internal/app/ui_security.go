//go:build !nogui

package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/fishioon/aicat/internal/app/locales"
	"github.com/fishioon/aicat/internal/certificate"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/listenaddr"
	"github.com/fishioon/aicat/internal/systemproxy"
)

type securityState struct {
	certificate                                    certificate.Status
	proxy                                          systemproxy.Status
	loading, trusting, checking, proxyBusy         bool
	generation                                     uint64
	problem, certificateProblem, note              string
	details, domainOpen, pendingOpen, proxyConfirm bool
	deleteConfirm                                  bool
	certificateAction                              string
	domain, domainError, pendingDomain             string
	listenerHost, listenerPort, listenerProblem    string
	listenerAddresses                              []string
	listenerBusy                                   bool
}

func (a *application) refreshSecurity() {
	s := &a.security
	if s.loading || a.certificateBusy() || s.proxyBusy || s.listenerBusy {
		return
	}
	s.loading = true
	s.generation++
	generation := s.generation
	go func() {
		addresses, _ := listenaddr.Addresses()
		cert, err := a.r.connection.inspect(a.ctx)
		proxy := a.r.connection.status()
		a.post(func() {
			if s.generation != generation {
				return
			}
			s.loading = false
			s.certificate = cert
			s.certificateProblem = errorText(err)
			s.proxy = proxy
			if len(addresses) > 0 {
				s.listenerAddresses = addresses
			}
		})
	}()
}

func (a *application) installTrust() {
	s := &a.security
	if a.certificateBusy() || s.certificate.Expired || s.certificate.PendingDeletion {
		return
	}
	s.trusting = true
	s.checking = false
	s.generation++
	s.loading = false
	s.problem = ""
	s.note = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		defer cancel()
		err := a.r.connection.install(ctx)
		a.post(func() { s.checking = true })
		status, checkErr := a.r.connection.inspect(ctx)
		a.post(func() {
			s.trusting = false
			s.checking = false
			s.certificate = status
			s.certificateProblem = errorText(checkErr)
			if err != nil {
				s.problem = err.Error()
				return
			}
			if checkErr != nil {
				s.problem = "无法确认系统信任，请重试。"
				return
			}
			if !status.Trusted {
				s.problem = "证书尚未受信任，请重试。"
				return
			}
			s.problem = ""
			s.note = "证书已就绪"
			if s.pendingOpen && s.pendingDomain != "" {
				a.changeDomain(s.pendingDomain, true)
			}
		})
	}()
}

func (a *application) certificateCard(c *ui.Context, p palette) {
	s := &a.security
	ui.Column(c).FillWidth().Padding(18).Gap(16).Border(1, p.border).Radius(12).Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("lock")).Size(iconSize, iconSize)
			ui.Text(c, locales.LocalCertificate()).FontWeight(500).Grow(1)
			label := "未信任"
			switch {
			case s.certificateAction != "":
				label = s.certificateAction
			case s.trusting && s.checking:
				label = "正在验证"
			case s.trusting:
				label = "等待授权"
			case s.loading:
				label = "正在检测"
			case s.certificateProblem != "":
				label = "状态未知"
			case s.certificate.PendingDeletion:
				label = "删除未完成"
			case !s.certificate.Exists:
				label = "未安装"
			case s.certificate.Expired:
				label = "已过期"
			case s.certificate.Trusted:
				label = "已信任"
			}
			color := p.muted
			if s.certificate.Trusted && !s.certificate.PendingDeletion {
				color = p.success
			}
			ui.Text(c.Key("certificate-status"), tr(label)).FontSize(textCaption).TextColor(color)
		})
		ui.Row(c).Gap(8).Children(func() {
			if !s.certificate.PendingDeletion && (!s.certificate.Trusted || s.trusting) {
				label := "安装证书"
				if s.trusting {
					label = "正在安装…"
					if s.checking {
						label = "正在验证…"
					}
				}
				if ui.PrimaryButton(c.Key("certificate-install"), tr(label)).Disabled(a.certificateBusy() || s.loading || s.certificate.Expired || !s.certificate.Supported).Clicked() {
					a.installTrust()
				}
			}
			if s.certificate.Exists {
				a.certificateMenu(c)
			}
		})
		if s.certificateAction == "正在删除" {
			ui.Text(c, locales.CompleteMacosAuthorizationHttpsDecryptionIsDisabled()).FontSize(textCaption).TextColor(p.muted)
		}
		if s.trusting {
			label := "请在 macOS 弹出的窗口中确认。"
			if s.checking {
				label = "正在检查证书是否已受信任…"
			}
			ui.Text(c, tr(label)).FontSize(textCaption).TextColor(p.muted)
		}
		if s.certificateProblem != "" {
			ui.Text(c, locales.UnableToReadSystemTrustStatus()).TextColor(p.danger)
		}
		if s.certificate.PendingDeletion {
			ui.Text(c, locales.HttpsRemainsInPassthroughModeRetryDeletion()).FontSize(textCaption).TextColor(p.muted)
		}
		if s.certificate.Expired {
			ui.Text(c, locales.TheLocalCertificateHasExpiredAndCannotDecryptHttps()).TextColor(p.danger)
		}
	})
}

func (a *application) decryptionDomainsCard(c *ui.Context, p palette) {
	s := &a.security
	ui.Column(c).FillWidth().Padding(18).Gap(14).Border(1, p.border).Radius(12).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.DecryptionDomains()).FontWeight(500).Grow(1)
			if ui.Button(c.Key("add-decryption-domain"), locales.AddDomain()).Disabled(a.decrypting != "" || a.certificateBusy()).Clicked() {
				s.domain = ""
				s.domainError = ""
				s.domainOpen = true
			}
		})
		ui.Text(c, locales.DecryptOnlyTheDomainsInThisList()).FontSize(textCaption).TextColor(p.muted)
		hosts := a.r.interception.list()
		if len(hosts) == 0 {
			ui.Text(c.Key("decryption-domains-empty"), locales.NoDecryptionDomains()).Padding(12, 0).TextColor(p.muted)
		}
		for _, host := range hosts {
			ui.Row(c.Key("decryption-domain-" + host)).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Text(c, tr(host)).Grow(1).SingleLine()
				if ui.Button(c.Key("remove-decryption-"+host), locales.Remove()).Label(tr(locales.Remove2()+host)).Disabled(a.decrypting != "" || a.certificateBusy()).Background(ui.Transparent).Border(0, ui.Transparent).Clicked() {
					a.changeDomain(host, false)
				}
			})
		}
	})
}

func (a *application) securityMessages(c *ui.Context, p palette) {
	if a.security.problem != "" {
		ui.Text(c.Key("security-error"), tr(a.security.problem)).TextColor(p.danger)
	}
	if a.security.note != "" {
		ui.Text(c.Key("security-note"), tr(a.security.note)).FontSize(textCaption).TextColor(p.muted)
	}
}

func (a *application) proxySettings(c *ui.Context, p palette) {
	s := &a.security
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(28).Gap(20).Children(func() {
			ui.Text(c, locales.Proxy()).FontSize(textTitle).FontWeight(600)
			ui.Column(c).FillWidth().MaxWidth(680).Gap(18).Children(func() {
				ui.Column(c).FillWidth().Padding(18).Gap(14).Border(1, p.border).Radius(12).Children(func() {
					ui.Row(c).FillWidth().Gap(10).Children(func() {
						ui.Text(c, locales.LocalProxy()).FontWeight(500).Grow(1)
						label, color := "运行中", p.success
						if a.r.gateway.Status().State != "ready" {
							label, color = "已停止", p.danger
						}
						ui.Text(c.Key("proxy-address"), a.r.gateway.Status().Addr).Selectable().FontSize(textCaption).TextColor(p.muted)
						ui.Text(c, tr(label)).FontSize(textCaption).TextColor(color)
					})
					a.listenerControls(c, p)
				})
				ui.Column(c).FillWidth().Padding(18).Gap(12).Border(1, p.border).Radius(12).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
						ui.Text(c, locales.SystemProxy()).FontWeight(500).Grow(1)
						enabled := s.proxy.Enabled
						if ui.Switch(c.Key("system-proxy-toggle"), &enabled).Label(locales.UseAicatAsTheSystemProxy()).Disabled(s.proxyBusy || s.loading || a.certificateBusy() || !s.proxy.Supported).Changed() {
							if enabled {
								if !s.certificate.Trusted {
									s.note = "请先安装并信任本机证书"
									a.navigate(location{settings: true, page: settingsProxy})
								} else {
									s.proxyConfirm = true
								}
							} else {
								a.setSystemProxy(false)
							}
						}
					})
					label := "未启用"
					if s.loading {
						label = "正在检测…"
					}
					if s.proxy.Enabled {
						label = "已启用"
					}
					if s.proxyBusy {
						label = "正在更新…"
					}
					ui.Text(c.Key("system-proxy-status"), tr(label)).FontSize(textCaption).TextColor(p.muted)
					if s.proxy.Problem != "" {
						ui.Text(c.Key("system-proxy-error"), tr(s.proxy.Problem)).TextColor(p.danger)
					}
					if s.proxy.Recovery && !s.proxy.OwnedElsewhere {
						if ui.Button(c.Key("system-proxy-restore"), locales.RestoreOriginalSettings()).Disabled(s.proxyBusy).Clicked() {
							a.setSystemProxy(false)
						}
					}
				})
				a.certificateCard(c, p)
				a.decryptionDomainsCard(c, p)
				a.securityMessages(c, p)
			})
		})
	})
}

func (a *application) setSystemProxy(enabled bool) {
	s := &a.security
	if s.proxyBusy || a.certificateBusy() {
		return
	}
	s.proxyBusy = true
	s.problem = ""
	s.note = ""
	s.generation++
	s.loading = false
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
		defer cancel()
		err := a.r.connection.set(ctx, enabled)
		status := a.r.connection.status()
		a.post(func() {
			s.proxyBusy = false
			s.proxy = status
			if err != nil {
				s.problem = err.Error()
			}
		})
	}()
}

func (a *application) requestDecryption(host string) {
	s := &a.security
	if s.certificate.Trusted {
		a.changeDomain(host, true)
		return
	}
	s.pendingDomain = host
	s.pendingOpen = true
	s.problem = ""
	s.note = ""
	a.refreshSecurity()
}

func (a *application) changeDomain(host string, enabled bool) {
	if a.decrypting != "" {
		return
	}
	host, err := config.InterceptHost(host)
	if err != nil {
		a.security.domainError = err.Error()
		return
	}
	a.decrypting = host
	a.security.problem = ""
	a.security.domainError = ""
	go func() {
		err := a.r.interception.set(host, enabled)
		a.post(func() {
			a.decrypting = ""
			if err != nil {
				a.security.problem = err.Error()
				return
			}
			a.security.domainOpen = false
			a.security.pendingOpen = false
			a.security.pendingDomain = ""
			a.security.note = "已更新，重新发起请求后生效。"
		})
	}()
}

func (a *application) securityDialogs(c *ui.Context, p palette) {
	s := &a.security
	if ui.Modal(c.Key("add-domain-dialog"), &s.domainOpen, func() {
		ui.Column(c).Width(380).Gap(16).Children(func() {
			ui.Text(c, locales.AddDecryptionDomain()).FontSize(textBody).FontWeight(500)
			ui.TextInput(c.Key("decryption-domain-input"), &s.domain).Label(locales.DecryptionDomains()).Placeholder("example.com").AutoFocus().FillWidth()
			if s.domainError != "" {
				ui.Text(c.Key("domain-validation-error"), tr(s.domainError)).TextColor(p.danger)
			}
			a.securityMessages(c, p)
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c.Key("domain-cancel"), locales.Cancel()).Clicked() {
					s.domainOpen = false
				}
				if ui.PrimaryButton(c.Key("domain-save"), locales.Add()).Disabled(a.decrypting != "" || a.certificateBusy()).Clicked() {
					a.changeDomain(s.domain, true)
				}
			})
		})
	}).Dismissed() {
		s.domainError = ""
	}
	if ui.Modal(c.Key("pending-decryption-dialog"), &s.pendingOpen, func() {
		ui.Column(c).Width(420).Gap(16).Children(func() {
			ui.Text(c, tr(locales.Decrypt()+s.pendingDomain)).FontSize(textBody).FontWeight(500)
			a.certificateCard(c, p)
			a.securityMessages(c, p)
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c.Key("decryption-cancel"), locales.Cancel()).Clicked() {
					s.pendingOpen = false
					s.pendingDomain = ""
				}
				label := "客户端已单独信任"
				if s.certificate.Trusted {
					label = "启用此域名"
				}
				if ui.Button(c.Key("enable-with-client-trust"), tr(label)).Disabled(a.certificateBusy() || a.decrypting != "" || !s.certificate.Exists || s.certificate.PendingDeletion).Clicked() {
					a.changeDomain(s.pendingDomain, true)
				}
			})
		})
	}).Dismissed() {
		s.pendingDomain = ""
	}
	ui.Modal(c.Key("certificate-details-dialog"), &s.details, func() {
		ui.Column(c).Width(440).Gap(14).Children(func() {
			ui.Text(c, locales.CertificateDetails()).FontSize(textBody).FontWeight(500)
			for _, line := range []string{"名称：" + s.certificate.Name, "有效期自：" + s.certificate.NotBefore.Local().Format("2006-01-02"), "有效期至：" + s.certificate.NotAfter.Local().Format("2006-01-02"), "信任范围：当前 macOS 用户", "SHA-256：" + s.certificate.Fingerprint} {
				ui.Text(c, tr(line)).Selectable()
			}
			if s.certificateProblem != "" {
				ui.Text(c, tr(s.certificateProblem)).TextColor(p.danger)
			}
			if ui.Button(c.Key("certificate-recheck"), locales.CheckAgain()).Disabled(a.certificateBusy() || s.loading).Clicked() {
				a.refreshSecurity()
			}
			if ui.Button(c.Key("certificate-details-close"), locales.Close()).Clicked() {
				s.details = false
			}
		})
	})
	a.certificateDeleteDialog(c, p)
	ui.Modal(c.Key("system-proxy-confirm-dialog"), &s.proxyConfirm, func() {
		ui.Column(c).Width(400).Gap(16).Children(func() {
			ui.Text(c, locales.UseAicatAsTheSystemProxy()).FontSize(textBody).FontWeight(500)
			ui.Text(c, locales.ChangeThisMacSHttpAndHttpsProxyRestoreTheOriginalSettingsWhenDisabled())
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c.Key("system-proxy-cancel"), locales.Cancel()).Clicked() {
					s.proxyConfirm = false
				}
				if ui.PrimaryButton(c.Key("system-proxy-enable"), locales.Enable()).Clicked() {
					s.proxyConfirm = false
					a.setSystemProxy(true)
				}
			})
		})
	})
}
