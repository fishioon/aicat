package config

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

type RoomSettings struct {
	Name       string            `yaml:"name,omitempty" json:"name"`
	SendTarget string            `yaml:"sendTarget,omitempty" json:"sendTarget"`
	Automatic  bool              `yaml:"automatic" json:"automatic"`
	Prompt     string            `yaml:"prompt" json:"prompt"`
	Model      string            `yaml:"model" json:"model"`
	Context    map[string]string `yaml:"context,omitempty" json:"context"`
	Trigger    RoomTrigger       `yaml:"trigger" json:"trigger"`
}

type RoomDefaults struct {
	Automatic bool              `yaml:"automatic" json:"automatic"`
	Prompt    string            `yaml:"prompt" json:"prompt"`
	Model     string            `yaml:"model" json:"model"`
	Context   map[string]string `yaml:"context,omitempty" json:"context"`
	Trigger   RoomTrigger       `yaml:"trigger" json:"trigger"`
}

type RoomTemplate struct {
	Automatic *bool                `json:"automatic,omitempty"`
	Prompt    *string              `json:"prompt,omitempty"`
	Model     *string              `json:"model,omitempty"`
	Context   map[string]string    `json:"context,omitempty"`
	Trigger   *RoomTriggerTemplate `json:"trigger,omitempty"`
}

type RoomTriggerTemplate struct {
	Chat  *RoomChatTrigger    `json:"chat,omitempty"`
	Regex *RoomPatternTrigger `json:"regex,omitempty"`
	Self  *RoomPatternTrigger `json:"self,omitempty"`
}

type RoomTrigger struct {
	Chat  RoomChatTrigger    `yaml:"chat" json:"chat"`
	Regex RoomPatternTrigger `yaml:"regex" json:"regex"`
	Self  RoomPatternTrigger `yaml:"self" json:"self"`
}

type RoomChatTrigger struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type RoomPatternTrigger struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Pattern string `yaml:"pattern,omitempty" json:"pattern"`
}

func (d RoomDefaults) Validate() error {
	if d.Automatic {
		return errors.New("Room 默认配置不能自动开启处理")
	}
	return d.Settings().Validate()
}

func ValidRoomID(id string) bool {
	return len(id) == 64 && strings.Trim(id, "0123456789abcdef") == ""
}

func DefaultRoomDefaults() RoomDefaults {
	return RoomDefaults{
		Context: map[string]string{},
		Trigger: RoomTrigger{
			Chat: RoomChatTrigger{Enabled: true},
		},
	}
}

func DefaultRoomSettings() RoomSettings {
	return DefaultRoomDefaults().Settings()
}

func (d RoomDefaults) Settings() RoomSettings {
	return RoomSettings{
		Automatic: d.Automatic,
		Prompt:    d.Prompt,
		Model:     d.Model,
		Context:   cloneContext(d.Context),
		Trigger:   d.Trigger,
	}
}

func ApplyRoomTemplate(settings RoomSettings, template *RoomTemplate) RoomSettings {
	if template == nil {
		return settings
	}
	if template.Automatic != nil {
		settings.Automatic = *template.Automatic
	}
	if template.Prompt != nil {
		settings.Prompt = *template.Prompt
	}
	if template.Model != nil {
		settings.Model = *template.Model
	}
	if template.Context != nil {
		settings.Context = cloneContext(template.Context)
	}
	if template.Trigger != nil {
		if template.Trigger.Chat != nil {
			settings.Trigger.Chat = *template.Trigger.Chat
		}
		if template.Trigger.Regex != nil {
			settings.Trigger.Regex = *template.Trigger.Regex
		}
		if template.Trigger.Self != nil {
			settings.Trigger.Self = *template.Trigger.Self
		}
	}
	return settings
}

func (t RoomTemplate) Validate() error {
	if t.Automatic != nil && *t.Automatic {
		return errors.New("渠道模板不能自行开启自动处理")
	}
	return ApplyRoomTemplate(DefaultRoomSettings(), &t).Validate()
}

func (s RoomSettings) Validate() error {
	if len(s.Name) > 256 || strings.ContainsAny(s.Name, "\r\n\x00") || !utf8.ValidString(s.Name) {
		return errors.New("Room 名称无效")
	}
	if len(s.SendTarget) > 256 || strings.ContainsAny(s.SendTarget, "\r\n\x00") || !utf8.ValidString(s.SendTarget) {
		return errors.New("Room 外发寻址名称无效")
	}
	if len(s.Prompt) > 64<<10 || !utf8.ValidString(s.Prompt) || strings.ContainsRune(s.Prompt, 0) {
		return errors.New("Room prompt 必须是有效文本且不超过 64 KiB")
	}
	if len(s.Model) > 256 || strings.TrimSpace(s.Model) != s.Model || strings.ContainsAny(s.Model, "\r\n\x00") || !utf8.ValidString(s.Model) {
		return errors.New("Room model 无效")
	}
	if len(s.Context) > 32 {
		return errors.New("Room 上下文最多 32 项")
	}
	for key, value := range s.Context {
		if strings.TrimSpace(key) == "" || len(key) > 128 || len(value) > 4096 || !utf8.ValidString(key+value) || strings.ContainsAny(key, "\r\n\x00") || strings.ContainsRune(value, 0) {
			return errors.New("Room 上下文无效")
		}
	}
	if err := s.Trigger.Regex.validate("规则匹配"); err != nil {
		return err
	}
	return s.Trigger.Self.validate("自己发送的消息")
}

func (t RoomPatternTrigger) validate(label string) error {
	if !t.Enabled {
		return nil
	}
	if t.Pattern == "" {
		return errors.New("启用" + label + "时必须填写正则表达式")
	}
	if len(t.Pattern) > 4096 || strings.ContainsRune(t.Pattern, 0) || !utf8.ValidString(t.Pattern) {
		return errors.New(label + "的正则表达式无效")
	}
	if _, err := regexp.Compile(t.Pattern); err != nil {
		return errors.New(label + "的正则表达式无效")
	}
	return nil
}

func cloneContext(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
