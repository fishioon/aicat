package messages

import (
	"regexp"

	"github.com/fishioon/aicat/internal/config"
)

func roomTriggerReason(trigger config.RoomTrigger, message Message, self string) string {
	if message.Schedule != nil {
		return "schedule"
	}
	chatMatched := trigger.Chat.Enabled && !message.Outgoing &&
		(message.RoomType == "direct" || message.RoomType == "private")
	if trigger.Chat.Enabled && !message.Outgoing && message.RoomType == "group" && message.MentionStatus == "parsed" && self != "" {
		for _, id := range message.MentionIDs {
			if id == self {
				chatMatched = true
				break
			}
		}
	}
	regexMatched := trigger.Regex.Enabled &&
		(trigger.Regex.Self || !message.Outgoing) &&
		regexp.MustCompile(trigger.Regex.Pattern).MatchString(message.Text.Content)
	if chatMatched {
		return "chat"
	}
	if regexMatched {
		return "regex"
	}
	return ""
}
