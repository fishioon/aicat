# Claw 使用 Room 直连 Webox 与 Codex，aicat 托管 child 生命周期

状态：Room 消息、trigger 和回复语义由 [0007](0007-claw-room-context-log-and-trigger-gate.md) 定义；
多 agent、目录和账号绑定由 [0008](0008-claw-multi-webox-instance-and-account-binding.md) 定义。本文只保留
Child Mode 的 host、Room 身份和 thread 绑定决策；可选的独立 AgentGateway LaunchAgent 生命周期由
[0009](0009-launchd-agentgateway-lifecycle.md) 定义；Room 的 Codex 权限 profile 由
[0015](0015-claw-room-network-permission.md) 定义。本文的 agentgateway child host 决策已由
[0017](0017-builtin-gateway-for-codex-wiring.md) 取代，Room 身份与 thread 绑定继续有效。

Child Mode 下，`aicat serve` 以前台 child 启动 PATH 中的 standalone `agentgateway`。`claw.agents` 非空时，aicat
同时运行 Claw。每个 Webox 私聊或群聊映射为唯一长期 Room，并独占一个持久 Codex thread；所有 Room
共享一个按需启动的 Codex app-server child。

## Why

aicat 必须保留自己的 process image 才能同时运行 Claw，所以 launcher 使用统一的 child host；
agentgateway 仍保持 upstream clean 和 standalone。Room 到持久 Codex thread 的一对一绑定让会话上下文保持
隔离，同时避免为每条消息启动独立进程。

## Consequences

- 是否启动 Claw 只由 `claw.agents` 是否为空决定；
- Codex app-server 延迟到首个 Room Duty Cycle 启动，并在 Serve Session 内由所有 Room 复用；
- 每个 Room 以 Webox `roomid` 作为唯一标识和目录名，`room.json` 持久保存独占的 `thread_id`，恢复后继续使用同一 thread；企业微信的发送目标优先取该文件里人工维护的 `search_name`，私聊缺省回退对方资料 `search_name`，见 [0014](0014-wecom-applescript-send.md)；
- Claw 使用 `approvalPolicy: never`；turn 使用开启网络的 `aicat-room` permission profile
  （见 [0015](0015-claw-room-network-permission.md)），Room Directory 是主工作区，
  Webox Shared Directory 的 `outbox/` 是额外 writable root；
- aicat 的内置安全 developer instructions 不可由配置覆盖；用户指令追加在其后，并在 thread 新建和恢复时应用；
- Child Mode 下任一核心 child 异常退出都会结束 Serve Session；正常终止时 aicat 负责 signal、等待和强制回收。Launchd Mode 下 agentgateway 不属于 Serve Session。
