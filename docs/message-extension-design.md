# 渠道消息与 Agent 交接

当前实现以企业微信长连接 Bot 为例：原版 OpenClaw 渠道插件运行在应用内 Bun 宿主中，AICat 管理账号、Room 日志、Codex 和主动发送。安装与生命周期见[渠道宿主](extension-packages-design.md)，用户入口见[企业微信 BOT](feature-map/wecom.md)。

## 消息信封

渠道宿主与 Go 通过 stdio JSONL 交换请求、结果、状态及消息推送。账号与 session 在交接层绑定；消息内容不决定账号身份。消息使用 CloudEvents 1.0：

```json
{
  "specversion": "1.0",
  "id": "event-id",
  "source": "aicat-channel://wecom/account-identity",
  "type": "org.aicat.message.received.v1",
  "time": "2026-11-13T08:00:00Z",
  "datacontenttype": "application/json",
  "data": {
    "msgid": "platform-message-id",
    "action": "send",
    "from": "sender-id",
    "tolist": ["bot-id"],
    "roomid": "room-id",
    "msgtype": "text",
    "text": {"content": "消息正文"}
  }
}
```

data 是旧版 AICat 消息，不再嵌套 account 或 message。平台有时间时使用 msgtime 毫秒时间戳；缺失时保持缺失，以 event.time 的宿主首次接收时间判断是否过期。宿主重推保留原事件 ID、时间与内容，不在重推时刷新时间。来源绑定、消息 ID、会话及类型经过 Go 校验。

## Room 与确认

Room 目录键由渠道、绑定身份、roomid 的摘要生成，不直接拼接用户提供的目录名。

- messages.jsonl 保存完整事件、本地记录 ID、记录时间及发送状态流水。
- context/messages.log 每行保存一个旧版消息正文，包含接收与确认成功的发送；pending/unknown 不混进聊天历史。
- tasks/ 保存完整触发消息及交付状态，任务 ID 采用本地记录 ID。
- agent.json 保存 Room 的持久 Codex thread。

入站事件与上下文持久同步后，对允许处理的消息再保存待交付任务，最后向宿主确认。存储失败不确认，宿主继续保存与重推未确认事件。当前不做入站去重，确认丢失可能重复归档或交付。尾部不完整流水在追加前恢复，不静默把日志写入失败当成接收成功。

上下文日志只作为历史数据，不自行启动任务。非文本、明显过期或未来时间、空文本及自己的回流不作为新的触发请求。群聊当前标记为不支持；后续只有被 @ 的消息才能作为群聊触发输入。

## Codex 驱动

AICat 按需共享一个 codex app-server --stdio 子进程。Room 使用独立持久 thread 和独立工作目录；同一进程中的已加载 thread 不反复 resume，进程恢复后使用 thread/resume。

输入沿用旧版：trigger_messages 是本次交付的完整消息数组，requesters 按发送人保留本批最后一条消息的 from、msgid、roomid。普通空闲 Room 使用 turn/start，执行中的新触发消息使用 turn/steer，expectedTurnId 明确指向当前 turn。Codex 无需先读日志才能知道本次请求，历史按需读 context/messages.log。

待交付消息先持久化，运行前记录状态；同一 turn 可以包含 start 和随后 steer 的多条消息，turn 结束时更新这些消息的执行状态。遗留 running 恢复为 unknown，failed/unknown 不自动重跑；pending 在账号连接恢复后继续交付。旧格式任务缺少完整触发内容时明确失败，不从历史猜测或重新创建任务。

普通 commentary/final_answer 的 agentMessage 在 item/completed 时主动回发。显式发送工具只允许当前 Room 和固定 Agent source；工具已确认发送的相同文本不再通过普通输出重复回发。客户端最后一段文字不产生第二次额外发送。

## 主动发送

公共发送语义为 sendText。出站事件 type 为 org.aicat.message.send.requested.v1，data 使用同一正文结构的 action、roomid、msgtype、text.content；禁止伪填平台入站身份和时间。桌面、MCP、内部 Codex 的来源分别固定为 aicat-ui://desktop、aicat-agent://mcp、aicat-agent://codex。

外部 MCP 使用 `{channel,event}` 指定渠道。内部 Codex 由当前 Room 绑定选择账号，不从用户文本推断账号。平台目标与凭据由已启用的渠道绑定确定。

发送前保存 pending 回执并追加事件流水，然后调用官方插件 outbound.sendText；取得平台确认后记录 sent，超时、断线或进程退出导致无法判定时记录 unknown。相同 source+id 同内容查询返回原回执，内容不同拒绝，pending/unknown 不自动重发。回执成功只表示平台确认，不表示用户已读。
