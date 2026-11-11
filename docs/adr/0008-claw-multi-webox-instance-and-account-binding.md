# Claw Agent 以稳定命名空间连接多个 Webox，并初始化真实微信账号

状态：accepted

Claw 将 Webox 配置改为 `claw.agents` 显式列表。每个 agent 使用用户指定且不可由账号或 URL
推导的稳定 `id` 作为 `~/.aicat/agent/<id>/` 持久目录命名空间，并独占一个 Webox Instance。每次运行时，
agent 先确认该实例当前登录的可见微信号，再以微信数据库中的稳定 `account_id` 进入 polling；该值不保证
具有 `wxid_` 前缀。这样实例
目录不会随可修改的 WeChat ID 漂移，也不会把 Webox 自造的 provider ID 误当成微信身份。

## Consequences

- 配置 schema 保持 `version: 1` 并直接替换未发布的旧单 agent 结构，不提供迁移或兼容读取；agent 的
  `id`、`wechatID`、`url`、`tokenFile` 和 `sharedDir` 均须显式配置，trigger 由每个 agent 独立配置且
  未配置的 `enabled` 等价于 `false`；可选 `prompt` 也属于 agent。`tokenFile` 与 `sharedDir`
  使用绝对路径，不展开 `~` 或环境变量；删除 `AICAT_WEBOX_URL`、`AICAT_WEBOX_MEDIA_DIR` 和
  `AICAT_CODEX_BIN` 覆盖，但 `claw.codex.binary` 继续可配置并可默认到 `codex`；空 agent 列表合法；
- agent `id` 在配置中唯一并匹配 `^[a-z][a-z0-9_-]{0,63}$`。`sharedDir` 由每个 agent 单独配置，但不同
  agent 可以指向同一目录，aicat 不校验其唯一性；
- Webox 增加受现有 token 保护的 `GET /ilink/bot/userinfo`，返回必需的 `account_id`、`wechat_id` 和
  可选的 `nickname`、`avatar_url`；Webox 同时删除 `provider-account-id`，所有消息以
  `SessionTable.username` 作为统一 `roomid` 并保持 `tolist` 为空，当前账号发出的消息以真实 `account_id` 作为 `from`；
- aicat 只同步校验本地确定性配置；随后每个 agent worker 独立调用 `userinfo`，获取 `account_id` 并将
  返回的 `wechat_id` 与配置的 `wechatID` 比较。请求失败、返回值无效或微信号不一致时，该 worker 记录
  错误并按退避策略 sleep 后重试，不影响 aicat、agentgateway 或其他 agent；校验成功后 `account_id`
  只保存在当前进程内，worker 进入 polling，且该运行周期不再调用 `userinfo`；
- 每个 agent 拥有独立 polling、与普通请求错误相同的 sleep，以及 Agent Directory 中持久的全局 Webox Cursor；
  `getupdates` 消息缺少 `msgid` 或 `roomid` 时，当前响应按普通请求失败处理，sleep 后继续使用原 cursor 请求。
  一个 Webox 的运行错误不影响其他 agent；
- Claw 状态位于 `~/.aicat/agent/<id>/`。每个 Webox `roomid` 直接对应
  `rooms/<roomid>/`，不再生成另一套 Room ID。Room Directory 保存 `context/messages.log`，并在绑定 Codex
  thread 后以 `room.json` 保存 `thread_id`；企业微信的发送目标优先取该文件里人工维护的 `search_name`，私聊缺省回退对方资料 `search_name`，见 [0014](0014-wecom-applescript-send.md)；目录自身就是 Codex task cwd，不再持久化外部 workspace；
- 所有 agent 共享一个在启动准备阶段拉起的 Codex App Server child，为每个启用的 agent 创建或复用自己的 Codex project；每个 Room 绑定独立的持久 Codex thread，新 thread 关联该 agent 的 project。默认名称 `aicat-<agent.id>`、路径 `rooms/`，配置可覆盖，project 不改变 Room cwd 或权限。历史 thread 通过一次性脚本补关联。
  agent `prompt` 追加到内置安全指令之后，并在其 Room 的 `thread/start` 和 `thread/resume` 时传入，因此
  共享 child 不会使不同 agent 共用 prompt。恢复时保留原 thread 和历史，先以当前完整 `developerInstructions` 更新冷恢复配置，
  再用 `thread/inject_items` 补发明确替代旧版应用规则的 developer 消息；补发失败不得启动 turn。
  这样新版规则立即进入模型请求，压缩后也能从当前配置重建；同一次 Serve Session 的后续 turn 不重复补发。
  协议版本标记变化只在成功恢复后更新，不再丢弃 thread 绑定。
  aicat 从稳定的 `~/.aicat/agent/` cwd 启动该 child，使进程启动环境不依赖用户调用 aicat 时的目录，
  且不额外创建专用启动目录；
  每个 Room 的 task cwd 仍是自己的 Room Directory。共享 child 异常仍属于整个 Serve Session 的故障。
