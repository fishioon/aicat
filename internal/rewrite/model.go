// Package rewrite owns ordered rules and transactional script modifications.
package rewrite

import (
	"github.com/fishioon/aicat/internal/plugins"
	"net/http"
	"time"
)

type Config struct {
	Enabled       bool          `json:"enabled" yaml:"enabled"`
	Timeout       time.Duration `json:"timeoutNs" yaml:"timeout"`
	MemoryBytes   int64         `json:"memoryBytes" yaml:"memoryBytes"`
	MaxConcurrent int           `json:"maxConcurrent" yaml:"maxConcurrent"`
	MaxBodyBytes  int64         `json:"maxBodyBytes" yaml:"maxBodyBytes"`
	Rules         []Rule        `json:"rules" yaml:"rules"`
}
type Rule struct {
	ID       string         `json:"id" yaml:"id"`
	Name     string         `json:"name" yaml:"name"`
	Enabled  bool           `json:"enabled" yaml:"enabled"`
	Match    Match          `json:"match" yaml:"match"`
	Plugin   string         `json:"plugin" yaml:"plugin"`
	Settings map[string]any `json:"settings,omitempty" yaml:"settings,omitempty"`
}
type Match struct {
	Hosts      []string `json:"hosts,omitempty" yaml:"hosts,omitempty"`
	Methods    []string `json:"methods,omitempty" yaml:"methods,omitempty"`
	PathPrefix string   `json:"pathPrefix,omitempty" yaml:"pathPrefix,omitempty"`
}
type Request struct {
	Method string      `json:"method"`
	URL    string      `json:"url"`
	Header http.Header `json:"headers"`
	Body   []byte      `json:"body"`
}
type Response struct {
	StatusCode int         `json:"statusCode"`
	Header     http.Header `json:"headers"`
	Body       []byte      `json:"body"`
}
type Exchange struct {
	Request   Request   `json:"request"`
	Response  *Response `json:"response,omitempty"`
	Synthetic bool      `json:"synthetic,omitempty"`
}
type Execution struct {
	RuleID   string            `json:"ruleId,omitempty"`
	PluginID string            `json:"pluginId,omitempty"`
	Stage    string            `json:"stage"`
	Status   string            `json:"status"`
	Error    string            `json:"error,omitempty"`
	Duration time.Duration     `json:"durationNs,omitempty"`
	Logs     []plugins.CallLog `json:"logs,omitempty"`
}
type RuleStatus struct {
	Rule
	Error string `json:"error,omitempty"`
}
type Status struct {
	ActiveEnabled   bool           `json:"activeEnabled"`
	Config          Config         `json:"config"`
	ActiveRevision  string         `json:"activeRevision"`
	Revision        string         `json:"revision"`
	Error           string         `json:"error,omitempty"`
	RestartRequired bool           `json:"restartRequired"`
	Rules           []RuleStatus   `json:"activeRules"`
	Plugins         []plugins.Info `json:"activePlugins"`
}
