# Claw Room 通过开启网络的 aicat-room permission profile 访问外部服务

状态：accepted；取代 [0005](0005-claw-room-and-child-host.md) 和
[0007](0007-claw-room-context-log-and-trigger-gate.md) 中「turn 使用无网络 sandbox」的前提；文中的
agentgateway 出图端点由 [0017](0017-builtin-gateway-for-codex-wiring.md) 改由内置网关承担，网络 profile 决策不变。

Room 需要访问网关等外部服务，例如通过 `agentgateway` 的 `/v1/images/generations` 出图，因此不再使用
内置 `:workspace` profile。aicat 启动共享 Codex app-server 时用 `-c` 定义 `aicat-room`：文件系统规则与
`:workspace` 相同（`:project_roots` 可写、`:root` 可读、`:tmpdir` 与 `:slash_tmp` 可写），
`network.enabled = true`；thread/start 和 thread/resume 显式选择该 profile。

Codex 只在隐式使用内置 profile 时才读取 `sandbox_workspace_write`，客户端显式选择 profile 后该配置被忽略：
实测 `-P :workspace` 加上 `sandbox_workspace_write.network_access = true` 仍然无法出网，所以开启网络必须在
aicat 侧选择自带 profile。Codex 同时要求定义了 `[permissions]` profile 时必须给出 `default_permissions`，
因此 app-server 启动参数里一并设置 `default_permissions="aicat-room"`。

所有 Room 共用一个 Codex app-server child，网络因此按 Serve Session 生效，不做 per-agent 或 per-Room 开关。

## Consequences

- Room 内的 shell 与网络工具可以访问任意出网目标，不再限于网关；
- Context Log 仍视为不可信聊天数据，但外泄面变大：内置 developer instructions 的「不要泄露凭据或秘密」
  是唯一约束，Room 目录不应保存明文凭据；
- `aicat-room` 的 filesystem 规则是 `:workspace` 语义的副本，Codex 改变 `:workspace` 时不会自动跟随，
  需要人工核对；
- 需要收紧时用 profile 的 `network.domains` 白名单走托管代理，或让不同 Room 选择不同 profile；
- 网络访问能力不改变 thread 绑定、trigger、Context Log 和回复回发语义。
