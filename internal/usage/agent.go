package usage

import (
	"net/http"
	"strings"
)

// AgentHeader lets a caller inside aicat, such as the Codex extension,
// name the agent a call is for. The provider entry removes it before
// forwarding.
const AgentHeader = "X-Aicat-Agent"

// known maps the product name agents send in User-Agent to how the
// overview names them.
var known = map[string]string{
	"claude-cli":     "Claude Code",
	"claude-code":    "Claude Code",
	"claude-desktop": "Claude Desktop",
	"codex_cli_rs":   "Codex",
	"codex_exec":     "Codex",
	"codex-tui":      "Codex",
	"opencode":       "OpenCode",
	"cursor":         "Cursor",
	"cline":          "Cline",
	"roo-code":       "Roo Code",
	"kilo-code":      "Kilo Code",
	"zed":            "Zed",
	"goose":          "Goose",
	"aider":          "Aider",
	"crush":          "Crush",
	"gemini-cli":     "Gemini CLI",
	"qwen-code":      "Qwen Code",
}

// AgentOf names the agent of a request: the header when set, else the
// product at the front of User-Agent, "" when there is none.
func AgentOf(h http.Header) string {
	if name := strings.TrimSpace(h.Get(AgentHeader)); name != "" {
		return name
	}
	ua := strings.TrimSpace(h.Get("User-Agent"))
	product := ua
	if i := strings.IndexAny(ua, "/ ("); i >= 0 {
		product = ua[:i]
	}
	if product == "" {
		return ""
	}
	if name, ok := known[strings.ToLower(product)]; ok {
		return name
	}
	return product
}
