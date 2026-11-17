package config

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func InitializeRoomConfig(home string) error {
	path := filepath.Join(home, "aicat.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return errors.New("aicat.yaml must contain a mapping")
	}
	root := doc.Content[0]
	hasLegacyRooms := mappingValue(root, "rooms") != nil
	defaultsNode := mappingValue(root, "roomDefaults")
	hasMode := defaultsNode != nil && mappingValue(defaultsNode, "mode") != nil
	if !hasLegacyRooms && !hasMode {
		return nil
	}
	return update(home, func(root *yaml.Node) error {
		removeMappingValue(root, "rooms")
		current := mappingValue(root, "roomDefaults")
		if current == nil || current.Kind != yaml.MappingNode || len(current.Content) == 0 {
			if !hasLegacyRooms {
				return nil
			}
			return value(root, "roomDefaults", yaml.MappingNode).Encode(DefaultRoomDefaults())
		}
		removeMappingValue(current, "mode")
		return nil
	})
}

func removeMappingValue(mapping *yaml.Node, key string) {
	if mapping.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return
		}
	}
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}
