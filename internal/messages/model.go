package messages

import (
	"encoding/json"
)

const Received = "org.aicat.message.received.v1"
const SendRequested = "org.aicat.message.send.requested.v1"
const ReplyPublished = "org.aicat.room.reply.published.v1"

type Event struct {
	Subject     string  `json:"subject,omitempty"`
	SpecVersion string  `json:"specversion"`
	ID          string  `json:"id"`
	Source      string  `json:"source"`
	Type        string  `json:"type"`
	Time        string  `json:"time,omitempty"`
	ContentType string  `json:"datacontenttype"`
	Data        Message `json:"data"`
}

type Text struct {
	Content string `json:"content"`
}

type Message struct {
	Historical    bool            `json:"historical,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	ReplyID       string          `json:"reply_id,omitempty"`
	Delivery      *Delivery       `json:"delivery,omitempty"`
	AgentSnapshot *Text           `json:"agent_snapshot,omitempty"`
	ReplyTo       string          `json:"reply_to,omitempty"`
	ContextKey    string          `json:"context_key,omitempty"`
	SearchName    string          `json:"search_name,omitempty"`
	LocalID       string          `json:"local_id,omitempty"`
	ServerID      string          `json:"server_id,omitempty"`
	Sequence      string          `json:"sequence,omitempty"`
	ContentType   int64           `json:"content_type,omitempty"`
	Flag          int64           `json:"flag,omitempty"`
	Outgoing      bool            `json:"outgoing,omitempty"`
	RoomType      string          `json:"room_type,omitempty"`
	MentionIDs    []string        `json:"mention_ids,omitempty"`
	MentionAll    bool            `json:"mention_all,omitempty"`
	MentionStatus string          `json:"mention_status,omitempty"`
	SenderKind    string          `json:"sender_kind,omitempty"`
	MediaStatus   string          `json:"media_status,omitempty"`
	RawContent    string          `json:"raw_content,omitempty"`
	Image         json.RawMessage `json:"image,omitempty"`
	Voice         json.RawMessage `json:"voice,omitempty"`
	File          json.RawMessage `json:"file,omitempty"`
	Link          json.RawMessage `json:"link,omitempty"`
	ID            string          `json:"msgid,omitempty"`
	Action        string          `json:"action"`
	From          string          `json:"from,omitempty"`
	ToList        []string        `json:"tolist,omitempty"`
	RoomID        string          `json:"roomid"`
	Time          *int64          `json:"msgtime,omitempty"`
	Type          string          `json:"msgtype"`
	Text          Text            `json:"text"`
}

type AccountConfig struct {
	Values    map[string]any `json:"values,omitempty"`
	AccountID string         `json:"accountId"`
	Secret    string         `json:"secret"`
}

type Receipt struct {
	Delivery
	Parts []Delivery `json:"parts,omitempty"`
}

type Delivery struct {
	Source string          `json:"source"`
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Result *PlatformResult `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type PlatformResult struct {
	MessageID    string `json:"messageId,omitempty"`
	ChatID       string `json:"chatId"`
	Channel      string `json:"channel"`
	Confirmation string `json:"confirmation,omitempty"`
}

type Record struct {
	ID         string `json:"id"`
	RecordedAt string `json:"recordedAt"`
	Offset     int64  `json:"offset"`
	Event      Event  `json:"event"`
	Status     string `json:"status,omitempty"`
}

type State struct {
	Dropped    uint64   `json:"dropped"`
	AccountID  string   `json:"accountId"`
	Configured bool     `json:"configured"`
	Validated  bool     `json:"validated"`
	Phase      string   `json:"phase"`
	Error      string   `json:"error"`
	PID        int      `json:"pid"`
	Records    []Record `json:"records"`
	RoomDir    string   `json:"roomDir"`
	Tasks      []Task   `json:"tasks"`
}
