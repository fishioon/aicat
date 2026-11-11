//go:build !nogui

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/fishioon/aicat/internal/certificate"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/gateway"
	"github.com/fishioon/aicat/internal/listenaddr"
	"github.com/fishioon/aicat/internal/systemproxy"
	"github.com/fishioon/aicat/internal/traffic"
)

// Desktop is the typed boundary between the web UI and desktop services.
// MyGo generates its TypeScript client; view modules depend only on host.ts.
type Desktop struct{ app *application }

// DesktopState is a coherent snapshot of settings and live desktop state.
type DesktopState struct {
	Preferences      config.Preferences `json:"preferences"`
	Language         string             `json:"language"`
	TrafficEnabled   bool               `json:"trafficEnabled"`
	Recording        bool               `json:"recording"`
	NavigationPins   []string           `json:"navigationPins"`
	PinnedDomains    []string           `json:"pinnedDomains"`
	Gateway          gateway.Status     `json:"gateway"`
	Addresses        []string           `json:"addresses"`
	Certificate      certificate.Status `json:"certificate"`
	CertificateError string             `json:"certificateError"`
	Proxy            systemproxy.Status `json:"proxy"`
	Domains          []string           `json:"domains"`
	Production       bool               `json:"production"`
	Revision         uint64             `json:"revision"`
}

// StateChange invalidates the page's snapshot after a committed mutation.
type StateChange struct {
	Revision uint64 `json:"revision"`
}

var DesktopChanged = mygo.NewEvent[StateChange]("desktop:changed")

// State reads the current settings and OS status. It starts no services.
func (d *Desktop) State(ctx context.Context) (DesktopState, error) {
	a := d.app
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.ready.Load() || a.pending.Load() || a.quitting.Load() {
		return DesktopState{}, errors.New("应用尚未就绪或正在退出")
	}
	cfg, err := config.Load(a.r.home)
	if err != nil {
		return DesktopState{}, err
	}
	if time.Since(a.checkedAt) > 10*time.Second {
		a.certificate, a.certificateError = a.r.connection.inspect(ctx)
		a.checkedAt = time.Now()
	}
	addresses, _ := listenaddr.Addresses()
	return DesktopState{Preferences: cfg.UI, Language: mygo.App.Locale(), TrafficEnabled: a.enabled, Recording: a.recording, NavigationPins: cfg.Navigation.Pinned, PinnedDomains: cfg.Traffic.PinnedDomains, Gateway: a.r.gateway.Status(), Addresses: addresses, Certificate: a.certificate, CertificateError: errorText(a.certificateError), Proxy: a.r.connection.status(), Domains: a.r.interception.list(), Production: !mygo.IsDev(), Revision: a.revision.Load()}, nil
}
func (d *Desktop) mutate(ctx context.Context, operation func() error) error {
	a := d.app
	defer a.notifyState()
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.ready.Load() || a.pending.Load() || a.quitting.Load() {
		return errors.New("应用尚未就绪或正在退出")
	}
	return operation()
}
func (a *application) notifyState() {
	if !a.ready.Load() || a.quitting.Load() {
		return
	}
	revision := a.revision.Add(1)
	_ = DesktopChanged.Broadcast(StateChange{Revision: revision})
	if a.previewNotify != nil {
		a.previewNotify(StateChange{Revision: revision})
	}
}

// SetPreferences persists language and theme before applying either.
func (d *Desktop) SetPreferences(ctx context.Context, p config.Preferences) error {
	return d.mutate(ctx, func() error {
		if err := config.SetPreferences(d.app.r.home, p); err != nil {
			return err
		}
		d.app.applyPreferences(p)
		d.app.updateTray()
		return nil
	})
}
func (d *Desktop) SetTrafficEnabled(ctx context.Context, enabled bool) error {
	return d.mutate(ctx, func() error {
		a := d.app
		if err := config.SetTrafficEnabled(a.r.home, enabled); err != nil {
			return err
		}
		a.enabled = enabled
		a.r.capture.SetRecording(a.enabled && a.recording)
		a.updateTray()
		return nil
	})
}
func (d *Desktop) SetRecording(ctx context.Context, enabled bool) error {
	return d.mutate(ctx, func() error {
		a := d.app
		if !a.enabled {
			return errors.New("流量功能已关闭")
		}
		a.recording = enabled
		a.r.capture.SetRecording(enabled)
		a.updateTray()
		return nil
	})
}
func (d *Desktop) SetNavigationPins(ctx context.Context, values []string) error {
	return d.mutate(ctx, func() error { return config.SetPinnedNavigation(d.app.r.home, values) })
}
func (d *Desktop) SetDomainPins(ctx context.Context, values []string) error {
	return d.mutate(ctx, func() error { return config.SetPinnedDomains(d.app.r.home, values) })
}
func (d *Desktop) SetListener(ctx context.Context, address string) error {
	return d.mutate(ctx, func() error {
		a := d.app
		err := a.r.connection.rebind(ctx, a.r.gateway, address)
		a.updateTray()
		return err
	})
}
func (d *Desktop) SetDecryptionDomain(ctx context.Context, host string, enabled bool) error {
	return d.mutate(ctx, func() error { return d.app.r.interception.set(host, enabled) })
}
func (d *Desktop) SetSystemProxy(ctx context.Context, enabled bool) error {
	return d.mutate(ctx, func() error { return d.app.r.connection.set(ctx, enabled) })
}
func (d *Desktop) InstallCertificate(ctx context.Context) error {
	return d.mutate(ctx, func() error {
		d.app.checkedAt = time.Time{}
		deadline, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return d.app.r.connection.install(deadline)
	})
}
func (d *Desktop) DeleteCertificate(ctx context.Context) error {
	return d.mutate(ctx, func() error { d.app.checkedAt = time.Time{}; return d.app.r.connection.deleteCertificate(ctx) })
}
func (d *Desktop) RecheckCertificate(ctx context.Context) error {
	return d.mutate(ctx, func() error { d.app.checkedAt = time.Time{}; return nil })
}
func (d *Desktop) OpenKeychain(ctx context.Context) error {
	return d.mutate(ctx, openCertificateKeychain)
}
func (d *Desktop) ExportCertificate(ctx context.Context) error {
	return d.mutate(ctx, func() error {
		path, err := chooseCertificateExport(mygo.CallerWindow(ctx))
		if err != nil || path == "" {
			return err
		}
		return d.app.r.connection.exportCertificate(path)
	})
}
func (d *Desktop) CopyText(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return mygo.Clipboard.Write(transfer.TextData(text))
}
func (d *Desktop) ExportTraffic(ctx context.Context, ids []traffic.ID) error {
	return d.mutate(ctx, func() error {
		items := []traffic.Summary{}
		for _, id := range ids {
			record, err := d.app.r.traffic.Get(ctx, id)
			if err == nil {
				items = append(items, record.Summary)
			}
		}
		b, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return err
		}
		path, err := chooseTrafficExport(mygo.CallerWindow(ctx))
		if err != nil || path == "" {
			return err
		}
		return os.WriteFile(path, b, 0600)
	})
}
func (d *Desktop) RequestCURL(ctx context.Context, id traffic.ID) (string, error) {
	record, err := d.app.r.traffic.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if record.Tunnel || record.RequestReceived.Body.State != "complete" {
		return "", errors.New("请求正文不完整")
	}
	body, err := d.app.r.traffic.OpenBody(ctx, record.RequestReceived.Body.ID)
	if err != nil {
		return "", err
	}
	defer body.Reader.Close()
	return curlRequest(record.RequestReceived, body.Reader)
}
func (d *Desktop) Record(ctx context.Context, id traffic.ID) (traffic.Record, error) {
	return d.app.r.traffic.Get(ctx, id)
}

type BodySnapshot struct {
	Info traffic.BodyInfo `json:"info"`
	Data []byte           `json:"data"`
}

func (d *Desktop) Body(ctx context.Context, id traffic.BodyID) (BodySnapshot, error) {
	body, err := d.app.r.traffic.OpenBody(ctx, id)
	if err != nil {
		return BodySnapshot{}, err
	}
	defer body.Reader.Close()
	data, err := io.ReadAll(body.Reader)
	return BodySnapshot{Info: body.Info, Data: data}, err
}
func (d *Desktop) ClearTraffic(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.app.r.traffic.Clear()
}

// trafficViews counts active native channel subscriptions for diagnostics.
var trafficViews atomic.Int32
