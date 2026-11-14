package channelconfig

import (
	"errors"
	"path/filepath"
	"strings"
)

type LocalMessages struct {
	AccountID  string                  `yaml:"accountId" json:"accountId"`
	DataDir    string                  `yaml:"dataDir" json:"dataDir"`
	ReplyRooms map[string]string       `yaml:"replyRooms,omitempty" json:"replyRooms,omitempty"`
	Triggers   map[string]LocalTrigger `yaml:"triggers,omitempty" json:"triggers,omitempty"`
}

type LocalTrigger struct {
	Direct     bool     `yaml:"direct" json:"direct"`
	Mention    bool     `yaml:"mention" json:"mention"`
	MentionAll bool     `yaml:"mentionAll" json:"mentionAll"`
	Keywords   []string `yaml:"keywords,omitempty" json:"keywords,omitempty"`
	All        bool     `yaml:"all" json:"all"`
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
	if len(c.ReplyRooms) > 32 {
		return errors.New("自动处理会话最多 32 个")
	}
	for room, name := range c.ReplyRooms {
		if strings.TrimSpace(room) == "" || len(room) > 256 || strings.ContainsAny(room, "\r\n\x00") || strings.TrimSpace(name) == "" || len(name) > 256 || strings.ContainsAny(name, "\r\n\x00") {
			return errors.New("自动处理会话需要有效 Room ID 和唯一搜索名称")
		}
	}
	for room, trigger := range c.Triggers {
		if _, ok := c.ReplyRooms[room]; !ok {
			return errors.New("触发规则必须属于已配置的自动处理会话")
		}
		if len(trigger.Keywords) > 32 {
			return errors.New("每个会话最多配置 32 个关键词")
		}
		seen := map[string]bool{}
		for _, keyword := range trigger.Keywords {
			folded := strings.ToLower(strings.TrimSpace(keyword))
			if folded == "" || keyword != strings.TrimSpace(keyword) || len(keyword) > 128 || strings.ContainsAny(keyword, "\r\n\x00") || seen[folded] {
				return errors.New("关键词应为不重复的非空文本，每项最多 128 字节")
			}
			seen[folded] = true
		}
	}
	return nil
}
