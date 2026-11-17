package channelconfig

import (
	"errors"
	"path/filepath"
	"strings"
)

type LocalMessages struct {
	AccountID string `yaml:"accountId" json:"accountId"`
	DataDir   string `yaml:"dataDir" json:"dataDir"`
}

func (c LocalMessages) Validate() error {
	if strings.TrimSpace(c.AccountID) == "" || len(c.AccountID) > 256 || strings.ContainsAny(c.AccountID, "\r\n\x00") {
		return errors.New("请填写有效的本地账号 ID")
	}
	for _, path := range []string{c.DataDir} {
		if !filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsAny(path, "\r\n\x00") {
			return errors.New("数据库目录必须是绝对路径")
		}
	}
	return nil
}
