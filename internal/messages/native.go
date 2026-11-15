package messages

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/fishioon/aicat/internal/config"
)

func (c *Channel) nativeSource() string {
	c.mu.Lock()
	m := c.manifest
	c.mu.Unlock()
	if m == nil || len(m.IdentityFields) == 0 {
		return "aicat-channel://" + c.id + "/local"
	}
	values, err := config.ReadExtension(c.home, c.id)
	if err != nil {
		return ""
	}
	settings, _ := values["settings"].(map[string]any)
	identity := []string{}
	for _, key := range m.IdentityFields {
		value, _ := settings[key].(string)
		for _, field := range m.Fields {
			if field.Key == key && field.Type == "directory" {
				value = filepath.Clean(value)
			}
		}
		identity = append(identity, value)
	}
	return "aicat-channel://" + c.id + "/" + digest([]byte(strings.Join(identity, "\x00")))
}

func (c *Channel) selfID() string {
	c.mu.Lock()
	m := c.manifest
	c.mu.Unlock()
	if m == nil || m.SelfField == "" {
		return ""
	}
	values, err := config.ReadExtension(c.home, c.id)
	if err != nil {
		return ""
	}
	settings, _ := values["settings"].(map[string]any)
	value, _ := settings[m.SelfField].(string)
	return value
}

func (c *Channel) ReadNativeData(ctx context.Context, query map[string]any) (map[string]any, error) {
	file := filepath.Join(c.home, "state", c.id, "last-diagnostics.json")
	c.mu.Lock()
	running := c.account != nil
	c.mu.Unlock()
	if !running {
		raw, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			return map[string]any{"channel": c.Snapshot()}, nil
		}
		if err != nil {
			return nil, err
		}
		var result map[string]any
		if json.Unmarshal(raw, &result) != nil {
			return nil, errors.New("渠道诊断缓存损坏")
		}
		result["channel"], result["cached"] = c.Snapshot(), true
		return result, nil
	}
	raw, err := c.Gateway(ctx, map[string]any{"stage": "data", "query": query})
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result == nil {
		return nil, errors.New("渠道返回无效诊断数据")
	}
	if err = writeJSON(file, result); err != nil {
		return nil, err
	}
	result["channel"] = c.Snapshot()
	return result, nil
}

func (c *Channel) nativePackage() bool {
	d, ok := channelplugin.New(c.home).Descriptor(c.id)
	return ok && d.Format == "aicat"
}

func (c *Channel) nativeConfiguration() (map[string]any, error) {
	return c.nativeValues(true)
}

func (c *Channel) nativeValues(validate bool) (map[string]any, error) {
	m, err := channelplugin.New(c.home).Manifest(c.id)
	if err != nil {
		return nil, err
	}
	values, err := config.ReadExtension(c.home, c.id)
	if err != nil {
		return nil, err
	}
	settings, _ := values["settings"].(map[string]any)
	if settings == nil {
		settings = map[string]any{}
	}
	merged := map[string]any{}
	for key, value := range settings {
		merged[key] = value
	}
	account, err := c.credentials()
	if err != nil {
		return nil, err
	}
	for key, value := range account.Values {
		merged[key] = value
	}
	if validate {
		if err = validateNativeSettings(m, merged); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func validateNativeSettings(m channelplugin.Manifest, settings map[string]any) error {
	known := map[string]bool{}
	for _, field := range m.Fields {
		known[field.Key] = true
		value, exists := settings[field.Key]
		if !exists {
			if field.Required {
				return errors.New("请填写渠道配置：" + field.Label)
			}
			continue
		}
		switch field.Type {
		case "boolean":
			if _, ok := value.(bool); !ok {
				return errors.New("渠道配置应为布尔值：" + field.Label)
			}
		case "string", "secret", "directory":
			text, ok := value.(string)
			if !ok || len(text) > 4096 || strings.ContainsRune(text, 0) || (field.Required && strings.TrimSpace(text) == "") {
				return errors.New("渠道配置文本无效：" + field.Label)
			}
			if field.Type == "directory" && !filepath.IsAbs(text) {
				return errors.New("渠道目录必须是绝对路径")
			}
		}
	}
	for key := range settings {
		if !known[key] {
			return errors.New("未知渠道配置：" + key)
		}
	}
	return nil
}

func (c *Channel) configureNative(patch map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.account != nil || c.state.Phase == "connecting" {
		return errors.New("请先停用渠道再修改配置")
	}
	m, err := channelplugin.New(c.home).Manifest(c.id)
	if err != nil {
		return err
	}
	current, err := config.ReadExtension(c.home, c.id)
	if err != nil {
		return err
	}
	settings := map[string]any{}
	if previous, ok := current["settings"].(map[string]any); ok {
		for key, value := range previous {
			settings[key] = value
		}
	}
	account, err := c.credentials()
	if err != nil {
		return err
	}
	for key, value := range account.Values {
		settings[key] = value
	}
	secrets := map[string]bool{}
	for _, field := range m.Fields {
		if field.Type == "secret" {
			secrets[field.Key] = true
		}
	}
	for key, value := range patch {
		if text, ok := value.(string); secrets[key] && ok && text == "" {
			continue
		}
		settings[key] = value
	}
	if err = validateNativeSettings(m, settings); err != nil {
		return err
	}
	public, private := map[string]any{}, map[string]any{}
	for key, value := range settings {
		if secrets[key] {
			private[key] = value
		} else {
			public[key] = value
		}
	}
	previous, previousErr := os.ReadFile(c.credentialPath())
	if previousErr != nil && !os.IsNotExist(previousErr) {
		return previousErr
	}
	if err = writeJSON(c.credentialPath(), AccountConfig{AccountID: "local", Values: private}); err != nil {
		return err
	}
	err = config.UpdateExtension(c.home, c.id, map[string]any{"settings": public}, nil)
	if err != nil {
		if os.IsNotExist(previousErr) {
			return errors.Join(err, os.Remove(c.credentialPath()))
		}
		var restored AccountConfig
		if decodeErr := json.Unmarshal(previous, &restored); decodeErr != nil {
			return errors.Join(err, decodeErr)
		}
		return errors.Join(err, writeJSON(c.credentialPath(), restored))
	}
	return nil
}
