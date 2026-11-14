package messages

import (
	"github.com/fishioon/aicat/internal/channelconfig"
	"github.com/fishioon/aicat/internal/config"
)

func (c *Channel) ReadConfig() (map[string]any, error) {
	return config.ReadExtension(c.home, c.id)
}

func (c *Channel) UpdateConfig(patch map[string]any) error {
	if err := channelconfig.CheckPatch(patch, false); err != nil {
		return err
	}
	return config.UpdateExtension(c.home, c.id, patch, func(values map[string]any) error {
		candidate, err := channelconfig.Decode(values)
		if err != nil {
			return err
		}
		return channelconfig.Validate(candidate)
	})
}
