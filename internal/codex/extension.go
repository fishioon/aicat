package codex

import (
	"context"
	"net/http"
	"path/filepath"

	"github.com/fishioon/aicat/internal/codex/catalog"
	"github.com/fishioon/aicat/internal/extension"
)

// Extension is the Codex extension as aicat registers it. It decrypts Codex's
// HTTPS to chatgpt.com, so it needs the root certificate trusted.
var Extension = extension.Extension{
	ID:         "codex",
	Name:       "Codex",
	Summary:    "把已接入供应商的模型加入 Codex 的模型列表",
	Lede:       "把已接入供应商的模型加入 Codex 的模型列表，Codex 保留自己的 ChatGPT 登录。",
	Categories: []string{"Agent", "模型"},
	NeedsTrust: true,
	Restart:    "已在运行的 Codex 需要重新启动",
	Build:      build,
}

func build(env extension.Env) (extension.Parts, error) {
	models := catalog.Open(filepath.Join(env.Home, "cache", "models-dev.json"), modelsDevURL())
	h, err := New(Options{Backend: backendOrigin(), Providers: env.Providers, Forward: env.Forward,
		Catalog: models, Recorder: env.Recorder, Recording: env.Recording})
	if err != nil {
		return extension.Parts{}, err
	}
	door := newDoor(h)
	return extension.Parts{
		// it answers the requests it serves before capture sees them
		Addon:      door,
		Intercepts: door.intercepts,
		Routes:     map[string]http.Handler{Path + "/": h},
		Run:        func(ctx context.Context) { models.KeepFresh(ctx) },
	}, nil
}
