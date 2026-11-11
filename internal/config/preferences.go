package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Preferences is the app-wide presentation state in the sole aicat.yaml config.
type Preferences struct {
	Language string `yaml:"language"`
	Theme    string `yaml:"theme"`
}

func (p Preferences) Validate() error {
	if p.Language != "system" && p.Language != "zh-Hans" && p.Language != "en" {
		return fmt.Errorf("ui.language must be system, zh-Hans, or en")
	}
	if p.Theme != "system" && p.Theme != "light" && p.Theme != "dark" {
		return fmt.Errorf("ui.theme must be system, light, or dark")
	}
	return nil
}

func SetPreferences(home string, p Preferences) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return update(home, func(root *yaml.Node) error {
		n := value(root, "ui", yaml.MappingNode)
		if n.Kind != yaml.MappingNode {
			return fmt.Errorf("ui must contain a mapping")
		}
		if err := value(n, "language", yaml.ScalarNode).Encode(p.Language); err != nil {
			return err
		}
		return value(n, "theme", yaml.ScalarNode).Encode(p.Theme)
	})
}
