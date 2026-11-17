package messages

import (
	"maps"

	"github.com/fishioon/aicat/internal/config"
)

const roomSettingsVersion = 1

type roomOverrides struct {
	Version    int                   `json:"version,omitempty"`
	Name       *string               `json:"name,omitempty"`
	SendTarget *string               `json:"sendTarget,omitempty"`
	Automatic  *bool                 `json:"automatic,omitempty"`
	Prompt     *string               `json:"prompt,omitempty"`
	Model      *string               `json:"model,omitempty"`
	Context    *map[string]string    `json:"context,omitempty"`
	Trigger    *roomTriggerOverrides `json:"trigger,omitempty"`
}

type roomTriggerOverrides struct {
	Chat  *bool               `json:"chat,omitempty"`
	Regex *roomRegexOverrides `json:"regex,omitempty"`
}

type roomRegexOverrides struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Pattern *string `json:"pattern,omitempty"`
	Self    *bool   `json:"self,omitempty"`
}

func (s *roomStore) readSettings(id string) (config.RoomSettings, error) {
	if s.defaultsErr != nil {
		return config.RoomSettings{}, s.defaultsErr
	}
	settings := s.defaults
	record, err := readRoomRecord(s.home, id)
	if err != nil {
		return config.RoomSettings{}, err
	}
	if record.Settings != nil {
		applyRoomOverrides(&settings, *record.Settings)
	}
	if err = settings.Validate(); err != nil {
		return config.RoomSettings{}, err
	}
	return settings, nil
}

func (s *roomStore) writeSettings(id string, settings config.RoomSettings) error {
	if s.defaultsErr != nil {
		return s.defaultsErr
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	overrides := diffRoomOverrides(s.defaults, settings)
	_, err := s.updateRecord(id, func(record *RoomRecord) error {
		record.Settings = overrides
		return nil
	})
	return err
}

func applyRoomOverrides(settings *config.RoomSettings, overrides roomOverrides) {
	if overrides.Name != nil {
		settings.Name = *overrides.Name
	}
	if overrides.SendTarget != nil {
		settings.SendTarget = *overrides.SendTarget
	}
	if overrides.Automatic != nil {
		settings.Automatic = *overrides.Automatic
	}
	if overrides.Prompt != nil {
		settings.Prompt = *overrides.Prompt
	}
	if overrides.Model != nil {
		settings.Model = *overrides.Model
	}
	if overrides.Context != nil {
		settings.Context = cloneStringMap(*overrides.Context)
	}
	if overrides.Trigger != nil {
		if overrides.Trigger.Chat != nil {
			settings.Trigger.Chat.Enabled = *overrides.Trigger.Chat
		}
		if overrides.Trigger.Regex != nil {
			if overrides.Trigger.Regex.Enabled != nil {
				settings.Trigger.Regex.Enabled = *overrides.Trigger.Regex.Enabled
			}
			if overrides.Trigger.Regex.Pattern != nil {
				settings.Trigger.Regex.Pattern = *overrides.Trigger.Regex.Pattern
			}
			if overrides.Trigger.Regex.Self != nil {
				settings.Trigger.Regex.Self = *overrides.Trigger.Regex.Self
			}
		}
	}
}

func diffRoomOverrides(defaults, settings config.RoomSettings) *roomOverrides {
	result := roomOverrides{}
	result.Name = changedString(defaults.Name, settings.Name)
	result.SendTarget = changedString(defaults.SendTarget, settings.SendTarget)
	result.Automatic = changedBool(defaults.Automatic, settings.Automatic)
	result.Prompt = changedString(defaults.Prompt, settings.Prompt)
	result.Model = changedString(defaults.Model, settings.Model)
	if !maps.Equal(defaults.Context, settings.Context) {
		context := cloneStringMap(settings.Context)
		result.Context = &context
	}
	result.Trigger = diffRoomTrigger(defaults.Trigger, settings.Trigger)
	if result.Name == nil && result.SendTarget == nil && result.Automatic == nil && result.Prompt == nil && result.Model == nil && result.Context == nil && result.Trigger == nil {
		return nil
	}
	return &result
}

func diffRoomTrigger(defaults, settings config.RoomTrigger) *roomTriggerOverrides {
	result := roomTriggerOverrides{}
	result.Chat = changedBool(defaults.Chat.Enabled, settings.Chat.Enabled)
	regex := roomRegexOverrides{}
	regex.Enabled = changedBool(defaults.Regex.Enabled, settings.Regex.Enabled)
	regex.Pattern = changedString(defaults.Regex.Pattern, settings.Regex.Pattern)
	regex.Self = changedBool(defaults.Regex.Self, settings.Regex.Self)
	if regex.Enabled != nil || regex.Pattern != nil || regex.Self != nil {
		result.Regex = &regex
	}
	if result.Chat == nil && result.Regex == nil {
		return nil
	}
	return &result
}

func changedString(before, after string) *string {
	if before == after {
		return nil
	}
	return &after
}

func changedBool(before, after bool) *bool {
	if before == after {
		return nil
	}
	return &after
}

func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
