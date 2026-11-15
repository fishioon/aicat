package messages

import (
	"strings"

	"github.com/fishioon/aicat/internal/config"
)

func roomTriggerReason(trigger config.RoomTrigger, message Message, self string) string {
	if trigger.All {
		return "all"
	}
	if message.RoomType == "direct" && trigger.Direct {
		return "direct"
	}
	if message.RoomType == "group" && message.MentionStatus == "parsed" {
		if trigger.Mention && self != "" {
			for _, id := range message.MentionIDs {
				if id == self {
					return "mention"
				}
			}
		}
		if trigger.MentionAll && message.MentionAll {
			return "mention_all"
		}
	}
	if message.RoomType != "group" {
		return ""
	}
	text := strings.ToLower(message.Text.Content)
	for _, keyword := range trigger.Keywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return "keyword"
		}
	}
	return ""
}
