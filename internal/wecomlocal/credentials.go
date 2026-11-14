package wecomlocal

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/fishioon/aicat/internal/channelconfig"
)

type credential struct {
	AccountID string `json:"accountId"`
	DataDir   string `json:"dataDir"`
	Key       string `json:"key"`
}

func validKey(key string) bool {
	raw, err := base64.StdEncoding.Strict().DecodeString(key)
	return err == nil && len(raw) == 16 && base64.StdEncoding.EncodeToString(raw) == key
}

func (c *channel) key(local *channelconfig.LocalMessages) (string, error) {
	file := filepath.Join(c.home, "credentials", ID+".json")
	raw, err := os.ReadFile(file)
	if err != nil {
		return "", errors.New("请填写 Base64 数据库密钥")
	}
	var saved credential
	if json.Unmarshal(raw, &saved) != nil || saved.AccountID != local.AccountID || saved.DataDir != filepath.Clean(local.DataDir) || !validKey(saved.Key) {
		return "", errors.New("请重新配置此账号和数据库目录的 Base64 密钥")
	}
	return saved.Key, nil
}

func (c *channel) saveKey(local *channelconfig.LocalMessages, key string) error {
	return writePrivate(filepath.Join(c.home, "credentials", ID+".json"), credential{AccountID: local.AccountID, DataDir: filepath.Clean(local.DataDir), Key: key})
}

func writePrivate(file string, value any) error {
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".local-save-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(value)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), file); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func normalizeKey(key string) string { return strings.TrimSpace(key) }
