// Package traffic owns bounded process-local snapshots, independent of UI and proxy.
package traffic

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type ID uint64
type BodyID string
type State string

const (
	Requesting State = "request"
	Responding State = "response"
	Completed  State = "completed"
	Failed     State = "failed"
	Canceled   State = "canceled"
)

var (
	ErrNotFound       = errors.New("traffic: not found or expired")
	ErrClosed         = errors.New("traffic: service closed")
	ErrSlowSubscriber = errors.New("traffic: subscriber overflow; watch again")
)

type Options struct {
	MaxRecords                      int
	MaxBodyBytes, MaxTotalBodyBytes int64
	SubscriberBuffer                int
}
type ClientApp struct {
	Name     string `json:"name"`
	BundleID string `json:"bundleId,omitempty"`
}

func (a ClientApp) Key() string {
	if a.BundleID != "" {
		return "bundle:" + strings.ToLower(a.BundleID)
	}
	if a.Name != "" {
		return "process:" + strings.ToLower(a.Name)
	}
	return ""
}

type Summary struct {
	ResponseBytes int64         `json:"responseBytes"`
	ID            ID            `json:"id"`
	Revision      uint64        `json:"revision"`
	StartedAt     time.Time     `json:"startedAt"`
	Method        string        `json:"method"`
	URL           string        `json:"url"`
	State         State         `json:"state"`
	StatusCode    int           `json:"statusCode"`
	Duration      time.Duration `json:"durationNs"`
	Error         string        `json:"error,omitempty"`
	Tunnel        bool          `json:"tunnel,omitempty"`
	ClientApp     ClientApp     `json:"clientApp,omitzero"`
}
type BodyInfo struct {
	ID       BodyID `json:"id"`
	State    string `json:"state"`
	Observed int64  `json:"observed"`
	Captured int64  `json:"captured"`
}
type Request struct {
	Method string      `json:"method"`
	URL    string      `json:"url"`
	Proto  string      `json:"proto"`
	Header http.Header `json:"headers"`
	Body   BodyInfo    `json:"body"`
}
type Response struct {
	StatusCode int         `json:"statusCode"`
	Proto      string      `json:"proto,omitempty"`
	Header     http.Header `json:"headers"`
	Body       BodyInfo    `json:"body"`
}
type ExecutionLog struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}
type Execution struct {
	RuleID   string         `json:"ruleId,omitempty"`
	PluginID string         `json:"pluginId,omitempty"`
	Stage    string         `json:"stage"`
	Status   string         `json:"status"`
	Error    string         `json:"error,omitempty"`
	Duration time.Duration  `json:"durationNs,omitempty"`
	Logs     []ExecutionLog `json:"logs,omitempty"`
}
type Record struct {
	Summary
	pendingClient    string
	RuleRevision     string      `json:"ruleRevision,omitempty"`
	Synthetic        bool        `json:"synthetic,omitempty"`
	Executions       []Execution `json:"executions,omitempty"`
	RequestReceived  Request     `json:"requestReceived"`
	RequestSent      *Request    `json:"requestSent,omitempty"`
	ResponseReceived *Response   `json:"responseReceived,omitempty"`
	ResponseSent     *Response   `json:"responseSent,omitempty"`
}
type Query struct {
	Limit        int
	Before       ID
	Host, Method string
	State        State
	Text         string
}
type Page struct {
	Items    []Summary `json:"items"`
	NextPage string    `json:"nextPage,omitempty"`
}
type Body struct {
	Info   BodyInfo
	Reader io.ReadCloser
}
type Start struct {
	RuleRevision  string
	At            time.Time
	Request       Request
	Tunnel        bool
	ClientApp     ClientApp
	PendingClient string
}
type Point uint8

const (
	RequestReceived Point = iota
	RequestSent
	ResponseReceived
	ResponseSent
)

type Observation struct {
	Executions []Execution
	Synthetic  bool
	Point      Point
	Request    *Request
	Response   *Response
}
type Completion struct {
	At    time.Time
	State State
	Error string
}
type Change struct {
	Kind string
	ID   ID
}
