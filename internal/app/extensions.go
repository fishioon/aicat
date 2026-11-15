package app

import (
	"github.com/fishioon/aicat/internal/codex"
	"github.com/fishioon/aicat/internal/extension"
)

// builtins are the built-in extensions, one line each. aicat knows nothing
// else about them.
var builtins = []extension.Extension{
	trafficExtension,
	codex.Extension,
}

// traffic is the oldest built-in and not yet built as the others are: its
// capture lives in the gateway's traffic addon; turning it off only stops
// capture, while the gateway and existing observations stay available (see setTraffic).
var trafficExtension = extension.Extension{
	ID:         "traffic",
	Name:       "流量",
	Summary:    "检查经过网关的 HTTP 和 HTTPS 请求与响应",
	Lede:       "检查经过网关的 HTTP 和 HTTPS 请求与响应。",
	Categories: []string{"网络", "调试"},
	Build:      func(extension.Env) (extension.Parts, error) { return extension.Parts{}, nil },
}
