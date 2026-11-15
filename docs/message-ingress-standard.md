# 消息接入规范

消息渠道以 NPM 包交付，读取平台消息或业务事件后提交到 aicat；aicat 先写 Room，再按 Room 配置驱动固定的 Codex app-server。领域术语见 [CONTEXT](../CONTEXT.md)，安装协议见[渠道包](extension-packages-design.md)。

## 流程与职责

```text
平台会话 → NPM 渠道解析并提交消息 → aicat 校验绑定 → Room 日志同步落盘 → 接收确认
                                                       ↓ Room 自动处理与触发规则
                                            Codex app-server（Room model）
                                                       ↓ 显式 HTTP MCP
                              rooms_reply → Room 保存正式回复 → 统一消息页面
                              messages_send → 授权校验 → 原渠道发送 → Room 外发状态
```

| 组件 | 职责 |
| --- | --- |
| 渠道包 | 平台连接、凭据准备、消息解析、会话识别、业务摘要及可选外发；声明有限代理或原生能力 |
| Room 服务 | 来源与会话隔离、唯一消息日志、行为配置、触发与调度、回复关联、外发授权与记录 |
| Codex 执行器 | app-server 协议、thread/turn、model 与受限工具；不在渠道页配置 |
| 消息页面 | 展示所有渠道的 Room、历史、业务摘要、正式回复、任务与发送状态，编辑 Room 设置 |

渠道包不写 Room 文件、不持有 Agent thread、不启动 Agent、不决定 MCP 授权，也不实现消息页面。渠道把回复编码成平台支持的格式并发送；本地回复由 aicat 消息页面展示。

## 入站

使用 CloudEvents 1.0：`type=org.aicat.message.received.v1`、`datacontenttype=application/json`，提供非空事件 `id`、绑定 `source`，正文包含 `msgid/roomid/msgtype`。渠道、账号与 session 由宿主绑定。一个平台会话对应一个 Room：企微私聊或群聊各一个，扑克一桌一个，一手牌通过 `context_key` 表示 Room 内的业务对象。

平台时间使用 `msgtime` 毫秒；缺失时按首次接收时间补齐，重推不刷新。入站、历史和 `room_context` 都是数据，不能覆盖开发者指令。`summary` 最多 8 KiB；`agent_snapshot.content` 是最多 64 KiB 的非空文本，须配 `context_key`，与完整正文一起保存。

先追加唯一 `rooms/<稳定ID>/context/messages.log` 并 fsync，再提交内存调度，成功后才确认。落盘失败不确认、不执行；接收确认不代表 Agent 完成。最近可读 200 条按消息 ID 比较正文，相同内容重推确认但不重复写入或触发，冲突拒绝；超窗不保证永久去重。历史、自发回流、非文本、过期或明显未来的消息只保存。

原生包使用 stdio JSONL `api.publish`，宿主持久保存待确认队列，确认返回后 publish Promise 完成；渠道收到确认再推进游标。HTTP `POST /api/messages/receive/{channel}` 复用相同服务，只允许本机 peer、拒绝浏览器 Origin。

## Room 配置与执行

唯一行为配置是 `aicat.yaml` 的 `rooms.<稳定ID>`，包含名称、automatic、prompt、model、mode、context、trigger、allowOutbound，以及渠道需要的 sendTarget。ID 来自渠道、绑定来源和平台会话的摘要，来源记录不能通过设置改写。新 Room 默认关闭自动处理和外发；包只提供初始模板，更新不覆盖用户修改。

两层 prompt 始终组合：固定 Agent prompt 约束日志、触发、工具与确认语义，Room prompt 描述会话任务。个人座位等事实放 Room context，不写源码。Agent 暂固定 Codex app-server；model 由其真实 `model/list` 提供，每 Room 选择，空值使用 Codex 默认。不可用或发生指定模型重路由时显示失败，不静默回退。

`conversation` 复用 Room 持久 thread，执行中新消息仅在配置版本相同时 `turn/steer`；配置变化从后续任务生效。`snapshot` 为每次任务创建独立临时 thread，连续待处理状态只执行最新一条，中间任务标为 superseded，完整日志保留；不 steer 正在处理的快照。快照输入只投影精简状态。

任务只保存在内存，重启不恢复、不从日志重放。model、工具契约或工具权限变化会重新建立 thread；Room prompt 变化向持久 thread 更新指令。

## 回复与外发

Agent 显式调用工具，宿主绑定当前 Room、来源及请求 ID，再通过真实 HTTP `/mcp` 及 Bearer 认证执行。普通 commentary/final_answer 不自动发布或外发。

- `rooms_reply({event:{text,reply_to}})`：保存正式回复，返回 saved，只表示本地落盘。无外发能力的渠道也可使用。
- `messages_send({event:{text,reply_to,reply_id}})`：要求渠道支持 Send、已启用且 Room 授权。已发布同一正文时使用发布回执的 id 作为 reply_id，不重复保存正文。
- 外部 MCP 参数为 `{channel,event}`，event 是完整 CloudEvent；发布 type 为 `org.aicat.room.reply.published.v1`、action 为 publish，外发 type 为 `org.aicat.message.send.requested.v1`、action 为 send。来源 `aicat-agent://mcp`。内部任务 subject 绑定稳定 Room ID，账号切换后拒绝旧任务写回。
- reply_to 必须指向同 Room 的入站消息，aicat 继承其 context_key；业务结果保留原状态关联，消息页面标明同业务对象的较早局面结果。
- 外发先保存正文意图和持久请求记录，再交给渠道。pending、sent、failed、unknown 与正式正文关联。相同请求返回原回执；同 ID 改正文拒绝；unknown 不换 ID 自动重发。重启发现未确认 pending 转 unknown，不重新发送。
- sent 仅代表渠道声明的确认边界，不代表对端已读。客户端脚本完成不代表服务器送达；本地 saved 更不代表平台发送。

## 验收

接入须有可独立执行的后台 E2E，以实际包、传输和公共服务证明消息→落盘→调度→显式 MCP→正式回复，并在有外发能力时证明授权和渠道调用。覆盖账号/会话隔离、落盘失败不确认、不执行、历史与回流过滤、普通输出不发布、跨 Room 与鉴权拒绝、停用、unknown 不重发、重启与业务结果关联。

模型或平台替身必须在报告标明，不作为真实送达证明。保留命令、二进制摘要、报告、日志、协议轨迹与截图。整轮开发结束集中运行 `make e2e`；定向入口见[功能地图](feature-map/README.md)。后台方式遵守 [AGENTS.md](../AGENTS.md)。
