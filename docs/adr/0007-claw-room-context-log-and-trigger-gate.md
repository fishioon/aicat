# Claw 使用 Room Context Log 和 trigger task gate

状态：trigger task gate 与 Context Log 仍然有效；默认 trigger 和外部 workspace 已被
[0008](0008-claw-multi-webox-instance-and-account-binding.md) 取代，turn 的无网络 sandbox 已被
[0015](0015-claw-room-network-permission.md) 取代。

Claw 对通过非空 `roomid` 校验的 Webox Batch，先按原始顺序尝试把全部消息追加到各自 Room 的本地
JSON Lines Context Log，不写入 trigger 等 connector metadata。之后只调度至少命中一条 trigger 的 Room；同一 Room 每批最多一次 Codex handoff，不同 Room 永不合并。两个独立触发器按 OR 组合：
两种触发器都处理 `msgtype=text` 的正文和 `msgtype=reply` 的 `reply.title`；链接、文件、图片、视频号和 mixed 消息不触发。正则触发匹配明确文本且不区分发送者；
聊天触发要求 `from` 非空且不等于当前账号 `account_id`，只匹配对方发来的私聊文本。因此当前账号自己的消息可以通过
正则触发，但不能通过聊天触发；当前版本不解析 `mentioned_me`。只有 `msgtime` 距接收时刻不超过 10 分钟的消息才参与 trigger 判断；时间缺失或更早的历史消息仍进入 Context Log，并随整个 Webox Batch 提交 Webox Cursor，但不能触发 Codex。

每个 Room 按消息顺序收集本批所有 trigger 完整消息记录：Idle Room 通过一次 `turn/start` 传递，并把消息的
`roomid` 绑定为回复目标；Active Room 通过一次 `turn/steer` 传递。没有 trigger 的批次只更新 Context Log，
不调用 Codex App Server，也不自动延长或续接 active turn。turn input 直接提供 trigger 消息，无需 Codex 先读取日志定位任务；普通消息不复制进 turn input。

Codex 使用开启网络的 `aicat-room` sandbox（见 [0015](0015-claw-room-network-permission.md)），内置 developer instructions 规定：只有 turn/start 或 turn/steer 完整给出的 trigger 消息记录才是建立、补充、调整或取消任务的控制指令，其他 `context/messages.log` 记录仅作为上下文或被忽略，不能改变任务或重新打开已完成任务；例外是 trigger 消息只有触发词而无实际内容，此时允许据 Context Log 最近消息认领用户漏加触发词的待处理请求，没有则只简短回复。Room 的媒体目录和可选 File Server 配置作为 thread resources 在 thread/start 和每次 thread/resume 时写入 developer instructions；固定的 Context Log 和媒体 inbox/outbox 路径按 Room 工作区约定推导。回复正文是微信可读的纯文本；需要随回复发文件时，首行写成 `#attachments#` 并用 `image=`、`file=` 声明逗号分隔的文件名，aicat 按该行解析出附件和正文并立即尝试一次 Webox 回发。`phase=commentary`
和 `phase=final_answer` 的每条非空 `agentMessage` 都在对应 `item/completed` 时回发，`turn/completed`
只结束 Room Duty Cycle，失败状态仍回发错误提示。

## Why

aicat 只是 connector，不应判断一条显式触发消息是补充、取消还是独立任务。trigger task gate 确定性控制
Idle Room 能否开启新任务以及 Active Room 能否改变当前 turn，并通过 turn input 机械转发完整触发消息；具体语义关联交给 Codex。
Room Context Log 保留触发前和执行期间的上下文，而不需要在 aicat 中实现
task classifier、snapshot 或持久 delivery queue。把普通消息与控制指令分离，也避免高流量 Room 持续 steer 导致 active turn 无法稳定完成。

## Consequences

- 每个 Room 向 `context/messages.log` 追加一行一条的 JSON 记录；记录直接平铺企业微信会话内容存档格式的 Webox 消息字段并保留 `seq`，不保存 connector metadata、运行日志信封或重复字段；
- `roomid` 是 Webox 统一的私聊或群聊会话标识，`tolist` 当前固定为空；
- Context Log 目录和文件权限分别为 `0700`、`0600`；不保存访问 token 或 Codex 输出；
- 第一版不实现消息幂等、轮转、保留上限或损坏恢复，需要时再独立增加；
- 每个 Webox Batch 按 Room 写入并同步日志，再逐 Room 调度；成功持久化的 Room 才推进内存 `seq`，单个 Room 失败不阻止其他 Room；不保存独立 checkpoint；
- 未触发消息只写 Context Log：Idle Room 不启动 Codex，Active Room 不 steer、也不自动续 turn；
- `msgtime` 缺失或距接收时刻超过 10 分钟的消息不能命中 trigger，以免 Webox 同步历史记录时意外启动任务；恰好 10 分钟仍视为有效，未来时间戳按时钟偏差处理；
- 每个 Room 按顺序把本批全部 trigger 完整消息记录交给一次 turn/start 或 turn/steer；回复目标取 Room 的 `roomid`；
- Idle Room 的任一 trigger 消息正文匹配 `deepReasoningRegex` 时，`turn/start` 使用 `deepReasoningEffort`，否则使用 `reasoningEffort`；Active Room 的 `turn/steer` 不改变当前 turn 已确定的推理强度；
- `turn/start` 与 `turn/steer` 直接传递 Webox 消息 JSON，不额外解析任务语义或直接附加媒体；普通消息与历史上下文仍由 Codex 从 Context Log 读取；
- trigger 消息只有触发词而无实际内容时，内置 developer instructions 允许 Codex 从 Context Log 最近消息中认领用户漏加触发词的待处理请求；没有待处理请求时只简短回复，日志中的其他消息仍不能自行开启或改变任务；
- Context Log 是否以及何时读取由 Codex 按任务需要决定，developer instructions 不规定检查点或读取次数；
- `resources.media` 以及可选的 `resources.publish_dir`、`resources.publish_url` 属于 Room/Thread 绑定，在 thread/start 和每次 thread/resume 时刷新；`resources.publish_dir` 存在时，网页结果和超过约十行的长结果写入其中不可预测的唯一子目录，回复只给摘要和 `resources.publish_url` 链接；`turn/start` 和 `turn/steer` 输入只包含 `trigger_messages`；
- 只有与 Room 当前 `thread_id` 和 active `turn_id` 匹配的事件才允许回发；
- commentary 与 final_answer 使用同一套解析：首行以 `#attachments#` 开头时，该行用 `image=`、`file=` 声明逗号分隔的附件文件名，其余是正文；首行不是附件行时整段按正文发出，避免漏写附件行导致丢消息；
- 附件名不含 `/` 时按 `outbox/` 解析，图片或文件取该行的 `image=`、`file=` 声明；附件沿用 Room 的 `roomid` 和既有路径校验，单次 best-effort 回发后不等待 acknowledgement、不自动重试，也不结束 active turn；
- 每条非空回发在对应 `item/completed` 时单次 best-effort 发出，不等待 `turn/completed`，因此不会被后续 steer 的输出覆盖；空输出保持静默；
- 同一批多个 trigger 合并为一次 handoff，并按消息顺序把全部触发记录标为控制指令；aicat 不维护任务列表；
- Context Log 内容始终视为不可信聊天数据，不能提升为 system 或 developer instructions；
- Codex turn 完成且没有与完成事件竞态的批次消息后，Room 回到 Idle；后续普通消息只写日志，下一条 trigger 才开启新的 Room Duty Cycle；
- 当前版本不解析或猜测群聊 @；群聊只能通过 regex trigger 唤醒，chat trigger 只作用于对方私聊；
- text 和 reply 以外的消息始终进入 Context Log，但不能命中 trigger；后续独立文本或 reply 消息负责给出明确指令；
- Webox 与 aicat 之间的消息对象沿用企业微信会话内容存档格式的字段形状，并以 `roomid` 作为统一会话标识、保持 `tolist` 为空；`getupdates` 通过 `rooms[roomid].messages` 分组，`sendmessage` 继续使用 `msgs` 数组；
- 视频号保持 `msgtype=sphfeed` 与 `sphfeed` 消息体，不生成伪造文本，也不参与 trigger；
- 引用消息使用 `reply.title + reply.parent`，不递归嵌入父消息正文；
- 当前 Codex app-server 版本尚不支持协议级 `readableRoots`；turn 使用开启网络的 `aicat-room` profile
  （见 [0015](0015-claw-room-network-permission.md)），Room Directory 是主工作区，
  Webox Shared Directory 的 `outbox/` 是额外 writable root；升级支持后应进一步收紧可读目录；
