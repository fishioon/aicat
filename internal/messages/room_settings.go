package messages

import (
	"maps"

	"github.com/fishioon/aicat/internal/config"
)

const roomSettingsVersion = 1

type roomOverrides struct {
	Version   int                   `json:"version,omitempty"`
	Name      *string               `json:"name,omitempty"`
	Automatic *bool                 `json:"automatic,omitempty"`
	Prompt    *string               `json:"prompt,omitempty"`
	Model     *string               `json:"model,omitempty"`
	Context   *map[string]string    `json:"context,omitempty"`
	Trigger   *roomTriggerOverrides `json:"trigger,omitempty"`
}

type roomTriggerOverrides struct {
	Chat  *bool                 `json:"chat,omitempty"`
	Regex *roomPatternOverrides `json:"regex,omitempty"`
	Self  *roomPatternOverrides `json:"self,omitempty"`
}

type roomPatternOverrides struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Pattern *string `json:"pattern,omitempty"`
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
		applyPatternOverrides(&settings.Trigger.Regex, overrides.Trigger.Regex)
		applyPatternOverrides(&settings.Trigger.Self, overrides.Trigger.Self)
	}
}

func diffRoomOverrides(defaults, settings config.RoomSettings) *roomOverrides {
	result := roomOverrides{}
	result.Name = changedString(defaults.Name, settings.Name)
	result.Automatic = changedBool(defaults.Automatic, settings.Automatic)
	result.Prompt = changedString(defaults.Prompt, settings.Prompt)
	result.Model = changedString(defaults.Model, settings.Model)
	if !maps.Equal(defaults.Context, settings.Context) {
		context := cloneStringMap(settings.Context)
		result.Context = &context
	}
	result.Trigger = diffRoomTrigger(defaults.Trigger, settings.Trigger)
	if result.Name == nil && result.Automatic == nil && result.Prompt == nil && result.Model == nil && result.Context == nil && result.Trigger == nil {
		return nil
	}
	return &result
}

func diffRoomTrigger(defaults, settings config.RoomTrigger) *roomTriggerOverrides {
	result := roomTriggerOverrides{}
	result.Chat = changedBool(defaults.Chat.Enabled, settings.Chat.Enabled)
	result.Regex = diffPatternTrigger(defaults.Regex, settings.Regex)
	result.Self = diffPatternTrigger(defaults.Self, settings.Self)
	if result.Chat == nil && result.Regex == nil && result.Self == nil {
		return nil
	}
	return &result
}

func applyPatternOverrides(trigger *config.RoomPatternTrigger, overrides *roomPatternOverrides) {
	if overrides == nil {
		return
	}
	if overrides.Enabled != nil {
		trigger.Enabled = *overrides.Enabled
	}
	if overrides.Pattern != nil {
		trigger.Pattern = *overrides.Pattern
	}
}

func diffPatternTrigger(defaults, settings config.RoomPatternTrigger) *roomPatternOverrides {
	result := roomPatternOverrides{Enabled: changedBool(defaults.Enabled, settings.Enabled), Pattern: changedString(defaults.Pattern, settings.Pattern)}
	if result.Enabled == nil && result.Pattern == nil {
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
