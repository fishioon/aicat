# 消息接入规范

消息渠道以 NPM 包交付，读取平台消息或业务事件后提交到 aicat；aicat 先写 Room，再按 Room 配置驱动固定的 Codex app-server。领域术语见 [CONTEXT](../CONTEXT.md)，安装协议见[渠道包](extension-packages-design.md)。

## 流程与职责

```text
平台会话 → NPM 渠道解析并提交消息 → aicat 校验绑定 → Room 日志同步落盘 → 接收确认
                                                       ↓ Room 自动处理与触发规则
                                 共享 Codex app-server（启动时注入 aicat MCP）
                                                       ↓ MCP tools
                         rooms_list → 解析目标 Room
                         messages_send(target_room_id, text) → 渠道实现
                         schedules_* → 指定 Room 的定时规则
```

| 组件 | 职责 |
| --- | --- |
| 渠道包 | 平台连接、凭据准备、消息解析、会话识别、业务摘要及可选外发；声明有限代理或原生能力 |
| Room 服务 | 来源与会话隔离、唯一消息日志、行为配置、触发与调度、发送请求与记录 |
| Codex 执行器 | app-server 协议、thread/turn、model 与 MCP 连接；不在渠道页配置 |
| 消息页面 | 展示所有渠道的 Room、历史、业务摘要、任务与发送状态，编辑 Room 设置 |

渠道包不写 Room 文件、不持有 Agent thread、不启动 Agent、不决定 MCP 授权，也不实现消息页面。渠道把回复编码成平台支持的格式并发送；本地回复由 aicat 消息页面展示。

## 入站

使用 CloudEvents 1.0：`type=org.aicat.message.received.v1`、`datacontenttype=application/json`，提供非空事件 `id`、绑定 `source`，正文包含 `msgid/roomid/msgtype`。渠道、账号与 session 由宿主绑定。一个平台会话对应一个 Room：企微私聊或群聊各一个，扑克一桌一个，一手牌通过 `context_key` 表示 Room 内的业务对象。

平台时间使用 `msgtime` 毫秒；缺失时按首次接收时间补齐，重推不刷新。入站、历史和 `room_context` 都是数据，不能覆盖开发者指令。`summary` 最多 8 KiB；`agent_snapshot.content` 是最多 64 KiB 的非空文本，须配 `context_key`，与完整正文一起保存。

先追加唯一 `rooms/<稳定ID>/context/messages.log` 并 fsync，再提交内存调度，成功后才确认。落盘失败不确认、不执行；接收确认不代表 Agent 完成。最近可读 200 条按消息 ID 比较正文，相同内容重推确认但不重复写入或触发，冲突拒绝；超窗不保证永久去重。历史、自发回流、非文本、过期或明显未来的消息只保存。

原生包使用 stdio JSONL `api.publish`，宿主持久保存待确认队列，确认返回后 publish Promise 完成；渠道收到确认再推进游标。HTTP `POST /api/messages/receive/{channel}` 复用相同服务，只允许本机 peer、拒绝浏览器 Origin。

## 会话展示信息

原生渠道可调用 `api.updateRoom({roomid,name,avatar,kind,members})`，其中 `members` 按平台成员 ID 提供 `{name,avatar}`；`kind` 为 direct/group/system。插件负责解析联系人备注、群名、群昵称和头像地址，aicat 不解析平台数据库。名称与头像属于展示数据，不作为寻址、授权或 Agent 指令。

宿主将更新作为 `org.aicat.room.presentation.updated.v1` 事件经同一绑定来源、持久待确认队列及 fsync 确认，保存在 `rooms/<稳定ID>/presentation.json`。更新不写消息日志、不推进聊天活动时间、不触发 Agent，也不会单独创建可见会话；既有历史直接使用最新名称与头像。成员最多 512 个，名称和头像地址有大小限制，头像只接受 HTTPS；加载失败使用默认头像。自定义 Room 名称优先于平台名称，元数据不覆盖 Room 设置。

## Room 配置与执行

唯一行为配置是 `aicat.yaml` 的 `rooms.<稳定ID>`，包含名称、automatic、prompt、model、mode、context、trigger，以及渠道需要的 sendTarget。ID 来自渠道、绑定来源和平台会话的摘要，来源记录不能通过设置改写。新 Room 默认关闭自动处理；包只提供初始模板，更新不覆盖用户修改。

两层 prompt 始终组合：固定 Agent prompt 只约束数据、身份、权限和不可信输入，Room prompt 描述会话业务。工具参数、返回状态和确认语义由 MCP 工具描述提供，不写入总 prompt。个人座位等事实放 Room context，不写源码。Agent 固定使用共享 Codex app-server；model 由其真实 `model/list` 提供，每 Room 选择，空值使用 Codex 默认。不可用或发生指定模型重路由时显示失败，不静默回退。

`conversation` 复用 Room 持久 thread，执行中新消息仅在配置版本相同时 `turn/steer`；配置变化从后续任务生效。`snapshot` 为每次任务创建独立临时 thread，连续待处理状态只执行最新一条，中间任务标为 superseded，完整日志保留；不 steer 正在处理的快照。快照输入只投影精简状态。

任务只保存在内存，重启不恢复、不从日志重放。model 变化会重新建立 thread；Room prompt 变化向持久 thread 更新指令。Agent 的普通 commentary/final output 不进入 Room 消息链路，也不自动发送。

## 定时消息

Agent 通过 schedules_set、schedules_list、schedules_delete 管理指定 Room 的定时规则；room_id 使用 rooms_list 返回的稳定 ID。规则只产生消息，不直接执行模型或发送平台消息。单次使用带时区偏移的 at，周期使用五段 cron 与显式 IANA timezone。

到点后 aicat 校验当前渠道账号、连接与 Room 自动处理开关，向同一 messages.log 保存 sender_kind=system、from=aicat://scheduler 且带 schedule 元数据的入站消息，再交原 Dispatcher。渠道入站和发送请求不能伪造 schedule。系统消息绕过普通提及/关键词过滤，按 conversation 处理避免 snapshot 合并；结果仍须显式调用 messages_send。周期由 cron 调度，单次使用 time.AfterFunc，到点先持久停用。重启只恢复未来触发，错过或投递失败不重试、不补跑；schedules_list 只返回规则与下一触发时间，不维护独立交付状态。普通 Agent Task 仍不跨重启恢复。

## Room 目录与发送

Agent 通过真实 HTTP `/mcp` 及 Bearer 认证调用工具。普通 commentary/final_answer 不发布、不转发，也不写入Room消息。

- `rooms_list({query?,channel?,limit?})`：返回稳定Room ID、渠道、名称、最近摘要和更新时间。
- `messages_send({target_room_id,text,reply_to?})`：`target_room_id`必填，可以是当前Room或其他可见Room；`reply_to`必须属于目标Room。
- aicat解析稳定Room ID，校验渠道账号绑定，保存外发意图和持久请求，再调用渠道实现。渠道决定是否支持发送、确认类型和返回状态。
- `reply_to`命中入站消息时继承其context_key；跨Room发送同时记录目标Room，不把模型提供的来源当作可信身份。
- 外发先保存正文意图和持久请求记录，再交给渠道。pending、sent、failed、unknown 与正文关联。相同请求返回原回执；同 ID 改正文拒绝；unknown 不自动重发。重启发现未确认 pending 转 unknown，不重新发送。
- sent 仅代表渠道声明的确认边界，不代表对端已读。

## 验收

接入须有可独立执行的后台 E2E，以实际包、传输和公共服务证明消息→落盘→调度→MCP发送，并在有外发能力时证明授权和渠道调用。覆盖账号/会话隔离、落盘失败不确认、不执行、历史与回流过滤、普通输出不发送、跨Room目标解析、停用、unknown不重发、重启与业务关联。

模型或平台替身必须在报告标明，不作为真实送达证明。保留命令、二进制摘要、报告、日志、协议轨迹与截图。整轮开发结束集中运行 `make e2e`；定向入口见[功能地图](feature-map/README.md)。后台方式遵守 [AGENTS.md](../AGENTS.md)。
