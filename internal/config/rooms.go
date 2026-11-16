package config

import (
	"errors"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type RoomSettings struct {
	Name       string            `yaml:"name,omitempty" json:"name"`
	SendTarget string            `yaml:"sendTarget,omitempty" json:"sendTarget"`
	Automatic  bool              `yaml:"automatic" json:"automatic"`
	Prompt     string            `yaml:"prompt" json:"prompt"`
	Model      string            `yaml:"model" json:"model"`
	Mode       string            `yaml:"mode" json:"mode"`
	Context    map[string]string `yaml:"context,omitempty" json:"context"`
	Trigger    RoomTrigger       `yaml:"trigger" json:"trigger"`
}

type RoomTrigger struct {
	All        bool     `yaml:"all" json:"all"`
	Direct     bool     `yaml:"direct" json:"direct"`
	Mention    bool     `yaml:"mention" json:"mention"`
	MentionAll bool     `yaml:"mentionAll" json:"mentionAll"`
	Keywords   []string `yaml:"keywords,omitempty" json:"keywords"`
}

func ValidRoomID(id string) bool {
	return len(id) == 64 && strings.Trim(id, "0123456789abcdef") == ""
}

func DefaultRoomSettings() RoomSettings {
	return RoomSettings{Mode: "conversation", Context: map[string]string{}, Trigger: RoomTrigger{All: true, Keywords: []string{}}}
}

func (s RoomSettings) Validate() error {
	if len(s.Name) > 256 || strings.ContainsAny(s.Name, "\r\n\x00") || !utf8.ValidString(s.Name) {
		return errors.New("Room 名称无效")
	}
	if len(s.SendTarget) > 256 || strings.ContainsAny(s.SendTarget, "\r\n\x00") || !utf8.ValidString(s.SendTarget) {
		return errors.New("Room 外发寻址名称无效")
	}
	if s.Mode != "conversation" && s.Mode != "snapshot" {
		return errors.New("Room 处理方式必须是 conversation 或 snapshot")
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
	if len(s.Trigger.Keywords) > 32 {
		return errors.New("每个 Room 最多配置 32 个关键词")
	}
	seen := map[string]bool{}
	for _, keyword := range s.Trigger.Keywords {
		folded := strings.ToLower(strings.TrimSpace(keyword))
		if folded == "" || keyword != strings.TrimSpace(keyword) || len(keyword) > 128 || strings.ContainsAny(keyword, "\r\n\x00") || !utf8.ValidString(keyword) || seen[folded] {
			return errors.New("Room 关键词必须是有效、不重复的非空文本")
		}
		seen[folded] = true
	}
	return nil
}

func ReadRoomSettings(home, id string) (RoomSettings, error) {
	if !ValidRoomID(id) {
		return RoomSettings{}, errors.New("无效 Room ID")
	}
	cfg, err := Load(home)
	if err != nil {
		return RoomSettings{}, err
	}
	settings, exists := cfg.Rooms[id]
	if !exists {
		return DefaultRoomSettings(), nil
	}
	return settings, settings.Validate()
}

func SetRoomSettings(home, id string, settings RoomSettings) error {
	return saveRoomSettings(home, id, settings, false)
}

func EnsureRoomSettings(home, id string, settings RoomSettings) error {
	return saveRoomSettings(home, id, settings, true)
}

func saveRoomSettings(home, id string, settings RoomSettings, onlyMissing bool) error {
	if !ValidRoomID(id) {
		return errors.New("无效 Room ID")
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	return update(home, func(root *yaml.Node) error {
		rooms := mapping(value(root, "rooms", yaml.MappingNode))
		if rooms == nil {
			return errors.New("rooms 必须是映射")
		}
		if onlyMissing {
			for i := 0; i < len(rooms.Content); i += 2 {
				if rooms.Content[i].Value == id {
					var existing RoomSettings
					if err := rooms.Content[i+1].Decode(&existing); err != nil {
						return err
					}
					return existing.Validate()
				}
			}
		}
		return value(rooms, id, yaml.MappingNode).Encode(settings)
	})
}
