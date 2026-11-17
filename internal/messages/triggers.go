package messages

import (
	"regexp"

	"github.com/fishioon/aicat/internal/config"
)

func roomTriggerReason(trigger config.RoomTrigger, message Message, self string) string {
	if message.Schedule != nil {
		return "schedule"
	}
	if message.Self {
		if matchesPattern(trigger.Self, message) {
			return "self"
		}
		return ""
	}
	if trigger.Chat.Enabled && chatAddressed(message, self) {
		return "chat"
	}
	if matchesPattern(trigger.Regex, message) {
		return "regex"
	}
	return ""
}

func chatAddressed(message Message, self string) bool {
	if message.RoomType == "direct" || message.RoomType == "private" {
		return true
	}
	if message.RoomType != "group" || message.MentionStatus != "parsed" || self == "" {
		return false
	}
	for _, id := range message.MentionIDs {
		if id == self {
			return true
		}
	}
	return false
}

func matchesPattern(trigger config.RoomPatternTrigger, message Message) bool {
	return trigger.Enabled && regexp.MustCompile(trigger.Pattern).MatchString(message.Text.Content)
}
