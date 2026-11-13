package config

import (
	"errors"
	"gopkg.in/yaml.v3"
	"path/filepath"
	"strings"
)

type LocalMessages struct {
	AccountID string `yaml:"accountId" json:"accountId"`
	DataDir   string `yaml:"dataDir" json:"dataDir"`
	KeyFile   string `yaml:"keyFile" json:"keyFile"`
}

func (c LocalMessages) Validate() error {
	if strings.TrimSpace(c.AccountID) == "" || len(c.AccountID) > 256 || strings.ContainsAny(c.AccountID, "\r\n\x00") {
		return errors.New("请填写有效的本地账号 ID")
	}
	for _, path := range []string{c.DataDir, c.KeyFile} {
		if !filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsAny(path, "\r\n\x00") {
			return errors.New("数据库目录和密钥文件必须是绝对路径")
		}
	}
	return nil
}

func SetLocalMessages(home, id string, settings LocalMessages) error {
	if err := settings.Validate(); err != nil {
		return err
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
		return value(section, "local", yaml.MappingNode).Encode(settings)
	})
}
