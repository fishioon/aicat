package config

import (
	"errors"
	"gopkg.in/yaml.v3"
)

func ReadExtension(home, id string) (map[string]any, error) {
	cfg, err := Load(home)
	if err != nil {
		return nil, err
	}
	values := cfg.Extensions[id].Values
	if values == nil {
		values = map[string]any{}
	}
	return values, nil
}

func UpdateExtension(home, id string, patch map[string]any, validate func(map[string]any) error) error {
	if id == "" {
		return errors.New("扩展 ID 不能为空")
	}
	if _, exists := patch["enabled"]; exists {
		return errors.New("请通过扩展开关修改启用状态")
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
		if err := mergeExtension(section, patch); err != nil {
			return err
		}
		candidate := map[string]any{}
		if err := section.Decode(&candidate); err != nil {
			return err
		}
		delete(candidate, "enabled")
		if validate != nil {
			return validate(candidate)
		}
		return nil
	})
}

func mergeExtension(section *yaml.Node, patch map[string]any) error {
	for key, next := range patch {
		node := value(section, key, yaml.MappingNode)
		if fields, ok := next.(map[string]any); ok {
			if node.Kind != yaml.MappingNode {
				*node = yaml.Node{Kind: yaml.MappingNode}
			}
			if err := mergeExtension(node, fields); err != nil {
				return err
			}
		} else if err := node.Encode(next); err != nil {
			return err
		}
	}
	return nil
}
