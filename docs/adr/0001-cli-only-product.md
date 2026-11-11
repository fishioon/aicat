# aicat 第一版采用 CLI-only 产品形态

状态：CLI-only 与「不提供原生 GUI」已由 [0016](0016-tray-app-with-capability-switches.md) 取代；
agentgateway 的 foreground-only 生命周期曾由 [0009](0009-launchd-agentgateway-lifecycle.md) 扩展，该方向已由
[0017](0017-builtin-gateway-for-codex-wiring.md) 取代。不修改 macOS system proxy 的决策仍有效。

aicat 第一版以 Go 命令行为唯一原生控制面，并移除现有 SwiftUI menu bar app，而不是让 CLI 与原生 app 长期并存。V1 唯一业务命令是 `aicat serve`；它准备 `~/.aicat/`、配置和 CA 后，通过 `exec` 启动 PATH 中的 standalone agentgateway。需要图形界面时使用 agentgateway 在 `https://ai.cat/ui` 提供的 Web UI。macOS 或具体客户端的 HTTP/HTTPS proxy 由用户自行配置为 `127.0.0.1:8888`，aicat 不提供 `proxy on/off`、不读写或恢复 system proxy。现有 app 没有实际用户安装基线，因此 CLI V1 采用 clean break，不承诺迁移旧 `binds` 配置或 app 状态。
