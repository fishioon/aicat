package channelconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/fishioon/aicat/internal/config"
	"gopkg.in/yaml.v3"
)

type Settings struct {
	Agent MessageAgent   `json:"agent"`
	Local *LocalMessages `json:"local,omitempty"`
}

func Load(home, id string) (Settings, error) {
	values, err := config.ReadExtension(home, id)
	if err != nil {
		return Settings{}, err
	}
	return Decode(values)
}

func Decode(values map[string]any) (Settings, error) {
	var result Settings
	raw, err := yaml.Marshal(values)
	if err == nil {
		err = yaml.Unmarshal(raw, &result)
	}
	return result, err
}

func Validate(settings Settings) error {
	if len(settings.Agent.Binary) > 4096 || strings.ContainsAny(settings.Agent.Binary, "\r\n\x00") {
		return errors.New("Codex 路径无效")
	}
	if settings.Local != nil {
		if err := settings.Local.Validate(); err != nil {
			return err
		}

	}
	return nil
}

func CheckPatch(patch map[string]any, local bool) error {
	if _, exists := patch["agent"]; exists {
		return errors.New("自动处理、prompt 和 model 请在 Room 中设置")
	}
	if _, exists := patch["local"]; exists && !local {
		return errors.New("扩展不支持本地数据库配置")
	}
	for key, value := range patch {
		if _, ok := value.(map[string]any); !ok {
			return errors.New("扩展配置必须是对象：" + key)
		}
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var settings Settings
	return decoder.Decode(&settings)
}
