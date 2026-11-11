# aicat V1 使用 Go 准备本地状态并 `exec` agentgateway

状态：已由 [0005](0005-claw-room-and-child-host.md) 的 child host 和 [0009](0009-launchd-agentgateway-lifecycle.md) 的可选 LaunchAgent 生命周期取代；child 与 launchd 两条路线整体由
[0017](0017-builtin-gateway-for-codex-wiring.md) 取代。保留本文记录最初的 launcher 决策。

aicat V1 使用 Go 实现。`aicat serve` 只确保 `~/.aicat/`、首次配置和本地 CA 存在，然后从 `PATH`
查找 `agentgateway`，将 cwd 切换到 `~/.aicat/`，最后通过 `exec` 将自身替换为
`agentgateway --file agentgateway.yaml`。

## Why

aicat 的产品价值是准备少量 macOS 本地状态，不是重写 agentgateway runner。Rust library embedding
要求 aicat 复制 upstream standalone orchestration；parent/child supervision 又引入 binary revision、
readiness、signal、logging、drain 和异常退出协调。`exec` 把 interface 压缩为“准备成功后替换进程”，
所有 runtime 生命周期自然回到 agentgateway。

## Consequences

- root project 不再包含 Rust crate 或 agentgateway Rust dependency graph；
- aicat 不下载、不构建、不做 revision handshake，也不修改 upstream submodule；
- agentgateway 必须由用户安装到 `PATH`；
- agentgateway、Web UI、用户和自动化 agent 直接共享唯一持久 YAML；所有 writer 必须保留 loopback
  network authorization，避免 wildcard listener 成为 LAN open proxy；
- aicat 不提供 readiness、Serve Session Lock、后台 daemon 或 stop cleanup protocol；
- terminal signal 和 agentgateway exit code 在 `exec` 后无需转发；
- CA 由 Go 标准库生成和验证，V1 不自动修改 macOS Keychain；
- system proxy 继续由用户手动配置。
