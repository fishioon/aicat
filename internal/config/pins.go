package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func SetPinnedNavigation(home string, ids []string) error {
	return update(home, func(root *yaml.Node) error {
		n := value(root, "navigation", yaml.MappingNode)
		if n.Kind != yaml.MappingNode {
			return fmt.Errorf("navigation must contain a mapping")
		}
		return value(n, "pinned", yaml.SequenceNode).Encode(ids)
	})
}

// SetPinnedDomains persists sidebar shortcuts without changing capture limits.
func SetPinnedDomains(home string, hosts []string) error {
	return update(home, func(root *yaml.Node) error {
		n := value(root, "traffic", yaml.MappingNode)
		if n.Kind != yaml.MappingNode {
			return fmt.Errorf("traffic must contain a mapping")
		}
		return value(n, "pinnedDomains", yaml.SequenceNode).Encode(hosts)
	})
}
