package messages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/fishioon/aicat/internal/channelconfig"
	"github.com/fishioon/aicat/internal/config"
)

func (c *Channel) migrateNativeConfiguration(previous channelconfig.Settings) error {
	c.mu.Lock()
	m := c.manifest
	c.mu.Unlock()
	if m == nil || previous.Local == nil || !slices.Contains(m.Capabilities.Native, "wecom-local") {
		return nil
	}
	values, err := config.ReadExtension(c.home, c.id)
	if err != nil {
		return err
	}
	if _, exists := values["settings"]; !exists {
		public := map[string]any{"accountId": previous.Local.AccountID, "dataDir": filepath.Clean(previous.Local.DataDir)}
		if err = config.UpdateExtension(c.home, c.id, map[string]any{"settings": public}, nil); err != nil {
			return err
		}
	}
	raw, err := os.ReadFile(c.credentialPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var saved struct {
		AccountID string `json:"accountId"`
		DataDir   string `json:"dataDir"`
		Key       string `json:"key"`
	}
	if json.Unmarshal(raw, &saved) != nil {
		return nil
	}
	if saved.AccountID != previous.Local.AccountID || filepath.Clean(saved.DataDir) != filepath.Clean(previous.Local.DataDir) || saved.Key == "" {
		return nil
	}
	return writeJSON(c.credentialPath(), AccountConfig{AccountID: "local", Values: map[string]any{"key": saved.Key}})
}
