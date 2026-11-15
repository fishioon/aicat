package channelplugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/fishioon/aicat/internal/config"
)

const ChannelAPI = 1

type Manifest struct {
	Prepare         string              `json:"prepare"`
	IdentityFields  []string            `json:"identityFields"`
	SelfField       string              `json:"selfField"`
	SendTargetLabel string              `json:"sendTargetLabel"`
	API             int                 `json:"api"`
	ID              string              `json:"id"`
	Entry           string              `json:"entry"`
	Capabilities    Capabilities        `json:"capabilities"`
	Room            config.RoomSettings `json:"room"`
	Fields          []Field             `json:"fields"`
}

type Capabilities struct {
	Hosts      []string `json:"hosts"`
	Routes     []string `json:"routes"`
	Send       bool     `json:"send"`
	Intercepts []string `json:"intercepts"`
	Page       bool     `json:"page"`
	WebSocket  bool     `json:"websocket"`
	Native     []string `json:"native"`
}

type Field struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

func inspectManifest(packageDir string, d Descriptor) (Manifest, string, error) {
	var pkg struct {
		Name    string    `json:"name"`
		Version string    `json:"version"`
		Aicat   *Manifest `json:"aicat"`
	}
	file := filepath.Join(packageDir, "package.json")
	info, err := os.Stat(file)
	if err != nil {
		return Manifest{}, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 256<<10 {
		return Manifest{}, "", errors.New("渠道包声明文件无效或过大")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return Manifest{}, "", err
	}
	if json.Unmarshal(raw, &pkg) != nil || pkg.Name != d.Package || pkg.Version != d.Version || pkg.Aicat == nil {
		return Manifest{}, "", errors.New("NPM 包缺少匹配的 aicat 渠道声明")
	}
	m := *pkg.Aicat
	if m.API != ChannelAPI || m.ID != d.ID || m.Entry == "" || len(m.Fields) > 64 || len(m.IdentityFields) > 64 || len(m.SendTargetLabel) > 256 || len(m.Capabilities.Intercepts) > 64 || len(m.Capabilities.Native) > 16 {
		return Manifest{}, "", errors.New("渠道 API 版本、标识或入口无效")
	}
	if m.Room.Mode == "" {
		m.Room.Mode = "conversation"
		m.Room.Trigger.All = true
	}
	if m.Room.Automatic || m.Room.AllowOutbound {
		return Manifest{}, "", errors.New("渠道模板不能自行开启自动处理或外发授权")
	}
	if err = m.Room.Validate(); err != nil {
		return Manifest{}, "", err
	}
	seen := map[string]bool{}
	for _, field := range m.Fields {
		if field.Key == "" || strings.ContainsAny(field.Key, ". /\\\x00\r\n") || len(field.Key) > 128 || len(field.Label) > 256 || field.Key == "agent" || field.Key == "enabled" || field.Key == "model" || field.Key == "prompt" || seen[field.Key] || (field.Type != "string" && field.Type != "directory" && field.Type != "boolean" && field.Type != "secret") {
			return Manifest{}, "", errors.New("渠道配置字段无效或侵入 Room 配置")
		}
		seen[field.Key] = true
	}
	identitySeen := map[string]bool{}
	for _, key := range m.IdentityFields {
		if key == "" || identitySeen[key] {
			return Manifest{}, "", errors.New("渠道身份字段为空或重复")
		}
		identitySeen[key] = true
	}
	for _, key := range append(append([]string{}, m.IdentityFields...), m.SelfField) {
		if key == "" {
			continue
		}
		valid := false
		for _, field := range m.Fields {
			valid = valid || (field.Key == key && field.Required && (field.Type == "string" || field.Type == "directory"))
		}
		if !valid {
			return Manifest{}, "", errors.New("渠道身份字段必须是必填的公开文本字段")
		}
	}
	if m.Prepare != "" {
		if len(m.Capabilities.Native) == 0 {
			return Manifest{}, "", errors.New("自动准备入口需要声明原生能力")
		}
		if _, err = safeEntry(packageDir, m.Prepare); err != nil {
			return Manifest{}, "", err
		}
	}
	for _, host := range m.Capabilities.Intercepts {
		if strings.TrimSpace(host) != host || host == "" || strings.ContainsAny(host, "/\\\r\n\x00") || strings.Contains(host, "*") {
			return Manifest{}, "", errors.New("渠道解密域名声明无效")
		}
	}
	if len(m.Capabilities.Hosts) > 64 || len(m.Capabilities.Routes) > 32 {
		return Manifest{}, "", errors.New("渠道代理能力声明过多")
	}
	for _, host := range m.Capabilities.Hosts {
		if host == "" || strings.TrimSpace(host) != host || strings.ContainsAny(host, "/\\\r\n\x00*") {
			return Manifest{}, "", errors.New("渠道观测域名声明无效")
		}
	}
	for _, route := range m.Capabilities.Routes {
		if !m.Capabilities.Page || !strings.HasPrefix(route, "/.aicat/"+m.ID+"/") || strings.ContainsAny(route, "?#\\\r\n\x00") || strings.Contains(route, "..") {
			return Manifest{}, "", errors.New("渠道采集入口必须位于自身命名空间")
		}
	}
	nativeSeen := map[string]bool{}
	for _, capability := range m.Capabilities.Native {
		if nativeSeen[capability] {
			return Manifest{}, "", errors.New("原生能力声明重复")
		}
		nativeSeen[capability] = true
		if capability != "wecom-local" {
			return Manifest{}, "", errors.New("未知原生能力")
		}
	}
	entry, err := safeEntry(packageDir, m.Entry)
	return m, entry, err
}

func safeEntry(packageDir, entry string) (string, error) {
	if filepath.IsAbs(entry) {
		return "", errors.New("插件入口必须是包内相对路径")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(packageDir, entry))
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(packageDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("插件入口越界")
	}
	if info, err := os.Stat(resolved); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("插件入口不可用")
	}
	return resolved, nil
}

func (s *Store) Manifest(id string) (Manifest, error) {
	d, ok := s.find(id)
	if !ok || d.Format != "aicat" {
		return Manifest{}, errors.New("不是 aicat 渠道包")
	}
	installed, err := s.Current(id)
	if err != nil {
		return Manifest{}, err
	}
	if _, err = s.installationEntry(id); err != nil {
		return Manifest{}, err
	}
	d.Version = installed.Version
	m, _, err := inspectManifest(filepath.Join(s.Directory(id), "node_modules", filepath.FromSlash(d.Package)), d)
	return m, err
}
