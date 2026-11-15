package config

import (
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

func MigrateCodexBinary(home, binary string) error {
	if binary == "" {
		return nil
	}
	if len(binary) > 4096 || strings.ContainsAny(binary, "\r\n\x00") {
		return errors.New("旧 Codex 路径无效")
	}
	return update(home, func(root *yaml.Node) error {
		section := mapping(value(root, "messageExecution", yaml.MappingNode))
		if section == nil {
			return errors.New("messageExecution 必须是映射")
		}
		node := value(section, "codexBinary", yaml.ScalarNode)
		var existing string
		if err := node.Decode(&existing); err != nil {
			return err
		}
		if existing != "" && existing != binary {
			return errors.New("多个渠道的旧 Codex 路径不同，不能自动合并为统一执行配置")
		}
		return node.Encode(binary)
	})
}

func RemoveLegacyChannelSettings(home, id string) error {
	return update(home, func(root *yaml.Node) error {
		extensions := mapping(value(root, "extensions", yaml.MappingNode))
		if extensions == nil {
			return errors.New("extensions 必须是映射")
		}
		section := mapping(value(extensions, id, yaml.MappingNode))
		if section == nil {
			return errors.New("扩展配置必须是映射")
		}
		for i := len(section.Content) - 2; i >= 0; i -= 2 {
			if section.Content[i].Value == "agent" || section.Content[i].Value == "local" {
				section.Content = append(section.Content[:i], section.Content[i+2:]...)
			}
		}
		return nil
	})
}
