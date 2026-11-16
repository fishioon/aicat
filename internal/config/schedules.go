package config

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

type ScheduleSpec struct {
	Name     string `yaml:"name" json:"name" jsonschema:"Human-readable schedule name."`
	Prompt   string `yaml:"prompt" json:"prompt" jsonschema:"Task content delivered when the schedule fires."`
	At       string `yaml:"at,omitempty" json:"at,omitempty" jsonschema:"Single future RFC3339 timestamp with UTC offset. Mutually exclusive with cron."`
	Cron     string `yaml:"cron,omitempty" json:"cron,omitempty" jsonschema:"Five-field cron expression. Requires timezone."`
	Timezone string `yaml:"timezone,omitempty" json:"timezone,omitempty" jsonschema:"IANA timezone for cron, for example Asia/Taipei."`
	Enabled  bool   `yaml:"enabled" json:"enabled" jsonschema:"Whether the schedule is active."`
}

type Schedule struct {
	RoomID string       `yaml:"roomId" json:"room_id"`
	Spec   ScheduleSpec `yaml:"schedule" json:"schedule"`
}

func ValidScheduleID(id string) bool {
	return len(id) > 0 && len(id) <= 64 && strings.Trim(id, "abcdefghijklmnopqrstuvwxyz0123456789-_") == ""
}

func (s ScheduleSpec) Next(after time.Time) (time.Time, error) {
	if strings.TrimSpace(s.Name) == "" || len(s.Name) > 256 || strings.ContainsAny(s.Name, "\r\n\x00") || !utf8.ValidString(s.Name) || strings.TrimSpace(s.Prompt) == "" || len(s.Prompt) > 64<<10 || strings.ContainsRune(s.Prompt, 0) || !utf8.ValidString(s.Prompt) {
		return time.Time{}, errors.New("定时任务需要有效名称与任务内容，内容不超过 64 KiB")
	}
	if (s.At == "") == (s.Cron == "") {
		return time.Time{}, errors.New("at 与 cron 必须且只能提供一个")
	}
	if s.At != "" {
		if s.Timezone != "" {
			return time.Time{}, errors.New("单次任务使用 at 中的时区偏移，不另设 timezone")
		}
		t, err := time.Parse(time.RFC3339Nano, s.At)
		if err != nil {
			return time.Time{}, errors.New("at 必须是带时区偏移的 RFC3339 时间")
		}
		return t.UTC(), nil
	}
	if s.Timezone == "" || s.Timezone == "Local" || strings.ContainsAny(s.Timezone, " \t\r\n=") || len(s.Cron) > 256 || len(strings.Fields(s.Cron)) != 5 {
		return time.Time{}, errors.New("周期任务需要五段 cron 和显式 IANA timezone，例如 Asia/Taipei")
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return time.Time{}, errors.New("无效定时任务时区")
	}
	p := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	parsed, err := p.Parse("CRON_TZ=" + s.Timezone + " " + s.Cron)
	if err != nil {
		return time.Time{}, errors.New("无效的五段 cron 表达式")
	}
	next := parsed.Next(after)
	if next.IsZero() {
		return time.Time{}, errors.New("cron 没有可执行的日期")
	}
	return next.UTC(), nil
}

func SetSchedule(home, key string, schedule *Schedule) error {
	return update(home, func(root *yaml.Node) error {
		schedules := mapping(value(root, "schedules", yaml.MappingNode))
		if schedules == nil {
			return errors.New("schedules 必须是映射")
		}
		if schedule != nil {
			return value(schedules, key, yaml.MappingNode).Encode(schedule)
		}
		for i := 0; i < len(schedules.Content); i += 2 {
			if schedules.Content[i].Value == key {
				schedules.Content = append(schedules.Content[:i], schedules.Content[i+2:]...)
				break
			}
		}
		return nil
	})
}
