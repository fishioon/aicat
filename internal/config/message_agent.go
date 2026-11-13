package config

import (
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

type MessageAgent struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Binary  string `yaml:"binary,omitempty" json:"binary"`
}

func SetMessageAgent(home, id string, settings MessageAgent) error {
	settings.Binary = strings.TrimSpace(settings.Binary)
	if len(settings.Binary) > 4096 || strings.ContainsAny(settings.Binary, "\r\n\x00") {
		return errors.New("Codex 路径无效")
	}
	return update(home, func(root *yaml.Node) error {
		extensions := mapping(value(root, "extensions", yaml.MappingNode))
		if extensions == nil {
			return errors.New("extensions 必须是映射")
		}
		section := mapping(value(extensions, id, yaml.MappingNode))
		if section == nil {
			return errors.New("扩展配置必须是映射")
		}
		return value(section, "agent", yaml.MappingNode).Encode(settings)
	})
}
