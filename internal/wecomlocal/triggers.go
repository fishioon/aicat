package wecomlocal

import (
	"strings"

	"github.com/fishioon/aicat/internal/channelconfig"
	"github.com/fishioon/aicat/internal/messages"
)

func triggerReason(local *channelconfig.LocalMessages, message messages.Message) string {
	if local.ReplyRooms[message.RoomID] == "" {
		return ""
	}
	trigger, configured := local.Triggers[message.RoomID]
	if !configured || trigger.All {
		return "all"
	}
	if message.RoomType == "direct" && trigger.Direct {
		return "direct"
	}
	if message.RoomType != "group" {
		return ""
	}
	if message.MentionStatus == "parsed" {
		if trigger.Mention {
			for _, id := range message.MentionIDs {
				if id == local.AccountID {
					return "mention"
				}
			}
		}
		if trigger.MentionAll && message.MentionAll {
			return "mention_all"
		}
	}
	text := strings.ToLower(message.Text.Content)
	for _, keyword := range trigger.Keywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return "keyword"
		}
	}
	return ""
}
