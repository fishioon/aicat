package config

import (
	"fmt"
	"github.com/fishioon/aicat/internal/listenaddr"
	"gopkg.in/yaml.v3"
)

func SetGatewayAddress(home, address string) error {
	if err := listenaddr.Validate(address); err != nil {
		return err
	}
	return update(home, func(root *yaml.Node) error {
		n := value(root, "gateway", yaml.MappingNode)
		if n.Kind != yaml.MappingNode {
			return fmt.Errorf("gateway must contain a mapping")
		}
		return value(n, "addr", yaml.ScalarNode).Encode(address)
	})
}
