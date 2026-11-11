package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fishioon/aicat/internal/rewrite"
	"gopkg.in/yaml.v3"
)

// 配置的读、改、原子替换共享同一把锁，避免不同设置的异步保存互相覆盖。
var updateMu sync.Mutex

func update(home string, edit func(*yaml.Node) error) error {
	updateMu.Lock()
	defer updateMu.Unlock()
	path := filepath.Join(home, "aicat.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("aicat.yaml must contain a mapping")
	}
	if err = edit(doc.Content[0]); err != nil {
		return err
	}
	data, err = yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(home, ".aicat-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func SetTrafficEnabled(home string, enabled bool) error {
	return update(home, func(root *yaml.Node) error {
		features := value(root, "features", yaml.MappingNode)
		if features.Kind != yaml.MappingNode {
			return fmt.Errorf("features must contain a mapping")
		}
		return value(features, "traffic", yaml.ScalarNode).Encode(enabled)
	})
}

func SetSystemProxyEnabled(home string, enabled bool) error {
	return update(home, func(root *yaml.Node) error {
		n := value(root, "systemProxy", yaml.MappingNode)
		if n.Kind != yaml.MappingNode {
			return fmt.Errorf("systemProxy must contain a mapping")
		}
		return value(n, "enabled", yaml.ScalarNode).Encode(enabled)
	})
}

func SetRewrite(home string, c rewrite.Config) error {
	return update(home, func(root *yaml.Node) error {
		n := value(root, "rewrite", yaml.MappingNode)
		if n.Kind != yaml.MappingNode {
			return errors.New("rewrite must contain a mapping")
		}
		fields := []struct {
			key   string
			value any
		}{{"enabled", c.Enabled}, {"timeout", c.Timeout}, {"memoryBytes", c.MemoryBytes}, {"maxConcurrent", c.MaxConcurrent}, {"maxBodyBytes", c.MaxBodyBytes}, {"rules", c.Rules}}
		for _, field := range fields {
			if err := value(n, field.key, yaml.ScalarNode).Encode(field.value); err != nil {
				return err
			}
		}
		return nil
	})
}
