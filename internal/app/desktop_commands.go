//go:build !nogui

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/listenaddr"
	"github.com/fishioon/aicat/internal/traffic"
	"os"
	"strings"
	"time"
)

// command serializes desktop mutations and quit. Slow OS work runs on the
// protocol handler's goroutine; native operations dispatch through MyGo.
func (a *application) command(ctx context.Context, action string, raw json.RawMessage) (any, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending.Load() || a.quitting.Load() {
		return nil, errors.New("应用正在退出")
	}
	var input struct {
		Enabled  bool         `json:"enabled"`
		Values   []string     `json:"values"`
		Language string       `json:"language"`
		Theme    string       `json:"theme"`
		Address  string       `json:"address"`
		Host     string       `json:"host"`
		Text     string       `json:"text"`
		IDs      []traffic.ID `json:"ids"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, err
		}
	}
	if strings.HasPrefix(action, "certificate-") {
		a.checkedAt = time.Time{}
	}
	switch action {
	case "state":
		cfg, err := config.Load(a.r.home)
		if err != nil {
			return nil, err
		}
		if time.Since(a.checkedAt) > 10*time.Second {
			a.certificate, a.certificateError = a.r.connection.inspect(ctx)
			a.checkedAt = time.Now()
		}
		cert, inspectErr := a.certificate, a.certificateError
		addresses, _ := listenaddr.Addresses()
		return map[string]any{"preferences": map[string]string{"language": cfg.UI.Language, "theme": cfg.UI.Theme}, "language": mygo.App.Locale(), "trafficEnabled": a.enabled, "recording": a.recording, "navigationPins": cfg.Navigation.Pinned, "pinnedDomains": cfg.Traffic.PinnedDomains, "gateway": a.r.gateway.Status(), "addresses": addresses, "certificate": cert, "certificateError": errorText(inspectErr), "proxy": a.r.connection.status(), "domains": a.r.interception.list()}, nil
	case "preferences":
		p := config.Preferences{Language: input.Language, Theme: input.Theme}
		if err := config.SetPreferences(a.r.home, p); err != nil {
			return nil, err
		}
		a.applyPreferences(p)
		a.updateTray()
	case "traffic":
		if err := config.SetTrafficEnabled(a.r.home, input.Enabled); err != nil {
			return nil, err
		}
		a.enabled = input.Enabled
		a.r.capture.SetRecording(a.enabled && a.recording)
		a.updateTray()
	case "recording":
		if !a.enabled {
			return nil, errors.New("流量功能已关闭")
		}
		a.recording = input.Enabled
		a.r.capture.SetRecording(a.recording)
		a.updateTray()
	case "navigation":
		return nil, config.SetPinnedNavigation(a.r.home, input.Values)
	case "pins":
		return nil, config.SetPinnedDomains(a.r.home, input.Values)
	case "listener":
		err := a.r.connection.rebind(ctx, a.r.gateway, input.Address)
		a.updateTray()
		return nil, err
	case "domain":
		return nil, a.r.interception.set(input.Host, input.Enabled)
	case "system-proxy":
		return nil, a.r.connection.set(ctx, input.Enabled)
	case "certificate-install":
		deadline, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return nil, a.r.connection.install(deadline)
	case "certificate-delete":
		return nil, a.r.connection.deleteCertificate(ctx)
	case "certificate-recheck":
		return nil, nil
	case "certificate-keychain":
		return nil, openCertificateKeychain()
	case "certificate-export":
		path, err := chooseCertificateExport(a.win)
		if err != nil || path == "" {
			return nil, err
		}
		return nil, a.r.connection.exportCertificate(path)
	case "clipboard":
		return nil, mygo.Clipboard.Write(transfer.TextData(input.Text))
	case "export":
		items := []traffic.Summary{}
		for _, id := range input.IDs {
			record, err := a.r.traffic.Get(ctx, id)
			if err == nil {
				items = append(items, record.Summary)
			}
		}
		b, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return nil, err
		}
		path, err := chooseTrafficExport(a.win)
		if err != nil || path == "" {
			return nil, err
		}
		return nil, os.WriteFile(path, b, 0600)
	case "curl":
		if len(input.IDs) != 1 {
			return nil, errors.New("select one request")
		}
		record, err := a.r.traffic.Get(ctx, input.IDs[0])
		if err != nil {
			return nil, err
		}
		if record.Tunnel || record.RequestReceived.Body.State != "complete" {
			return nil, errors.New("请求正文不完整")
		}
		body, err := a.r.traffic.OpenBody(ctx, record.RequestReceived.Body.ID)
		if err != nil {
			return nil, err
		}
		defer body.Reader.Close()
		return curlRequest(record.RequestReceived, body.Reader)
	default:
		return nil, fmt.Errorf("unknown desktop command: %s", action)
	}
	return nil, nil
}
