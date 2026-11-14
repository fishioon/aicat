package wecomlocal

import (
	"context"
	"errors"
	"github.com/fishioon/aicat/internal/channelconfig"
	"github.com/fishioon/aicat/internal/config"
	"os"
)

func (c *channel) ReadConfig() (map[string]any, error) { return config.ReadExtension(c.home, ID) }

func (c *channel) UpdateConfig(patch map[string]any) error {
	if err := channelconfig.CheckPatch(patch, true); err != nil {
		return err
	}
	previous, err := channelconfig.Load(c.home, ID)
	if err != nil {
		return err
	}
	return config.UpdateExtension(c.home, ID, patch, func(values map[string]any) error {
		candidate, err := channelconfig.Decode(values)
		if err != nil {
			return err
		}
		if err = channelconfig.Validate(candidate); err != nil {
			return err
		}
		if candidate.Agent.Enabled && candidate.Local == nil {
			return errors.New("请先配置本地数据库")
		}
		if _, changed := patch["local"]; changed {
			if candidate.Local == nil {
				return errors.New("本地数据库配置不能为空")
			}
			key, err := c.key(candidate.Local)
			if err != nil {
				return err
			}
			if err = c.validate(context.Background(), candidate.Local, key); err != nil {
				return err
			}
		}
		if candidate.Agent.Enabled && !previous.Agent.Enabled {
			if err = os.Remove(c.baselinePath(candidate.Local)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	})
}
