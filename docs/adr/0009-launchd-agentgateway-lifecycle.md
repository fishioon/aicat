# Launchd Mode 将 agentgateway 数据面从 aicat Serve Session 解耦

状态：已由 [0017](0017-builtin-gateway-for-codex-wiring.md) 取代；standalone agentgateway 及其 LaunchAgent 整体下线。

当 agentgateway 同时承担电脑和手机的网络代理时，aicat、Claw 或 File Server 的重启不应中断数据面。aicat 因此保留兼容的 `child` 模式，并新增显式 `launchd` 模式：`aicat serve` 在当前 GUI 用户域幂等协调 AgentGateway LaunchAgent，由 `launchd` 独立保活 upstream binary；定义未变化时不重启，restart 只作为显式维护动作。

## Consequences

- `agentgateway.mode` 省略时仍为 `child`；`launchd` 必须显式配置，避免升级时静默改变既有生命周期。
- LaunchAgent 固定使用 aicat 从当前 `PATH` 解析出的 binary 绝对路径、`~/.aicat/` 工作目录和 `agentgateway.yaml` 绝对路径，不依赖 launchd 的 shell 环境。
- `aicat serve` 在修改 launchd 状态前完成 aicat 配置、CA identity 和 agentgateway runtime 配置校验；无效配置不会停止已运行的数据面。
- 已加载且定义未变化时协调为 no-op；binary 路径等定义变化时会替换任务并造成一次明确重启。
- 从 `launchd` 切回 `child` 时，下一次 `aicat serve` 卸载 LaunchAgent 后再启动 foreground child，因此切换会有一次网络中断。
- `aicat agentgateway restart` 是唯一由 aicat 提供的显式重启入口；`uninstall` 不修改配置，配置仍为 `launchd` 时下次 serve 会重新安装。
- 使用当前用户的 LaunchAgent，而非需要 root 且语义属于系统服务的 LaunchDaemon；aicat 仍拒绝 sudo 运行，也不修改 macOS system proxy。
