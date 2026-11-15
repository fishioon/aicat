# 渠道消息与 Agent 交接

职责与验收规则见[消息接入规范](message-ingress-standard.md)，包声明见[NPM 渠道](extension-packages-design.md)。

## 消息信封

```json
{
  "specversion":"1.0",
  "id":"event-id",
  "source":"aicat-channel://channel/account-identity",
  "type":"org.aicat.message.received.v1",
  "time":"2026-11-15T08:00:00Z",
  "datacontenttype":"application/json",
  "data":{
    "msgid":"platform-id","action":"receive","from":"sender-id",
    "roomid":"conversation-id","msgtype":"text","msgtime":1794729600000,
    "text":{"content":"消息正文"}
  }
}
```

身份由绑定来源决定。msgtime 缺失时首次接收补齐；summary 是展示摘要，agent_snapshot.content 是执行器投影，context_key 标识业务对象。historical 与 outgoing 只入历史，不触发。完整消息正文保留在唯一 context/messages.log，不嵌套 account 或 event。

## 持久数据

| 路径 | 内容 |
| --- | --- |
| aicat.yaml / rooms.<hash> | 名称、Room prompt、model、context、mode、触发与授权、寻址 |
| rooms/<hash>/room.json | 渠道、来源、平台会话的不可变绑定 |
| rooms/<hash>/context/messages.log | 入站、正式回复、外发正文意图 |
| rooms/<hash>/agent.json | 普通对话 thread、模型与工具权限、指令摘要 |
| rooms/<hash>/outbound 与 deliveries | 外发请求、回执和已发布正文关联 |
| state/<channel> | 包宿主与渠道自身游标、采样、待确认队列 |
| logs/aicat.log | 运行状态，不记录正文或凭据 |

任务只在内存保留，重启不恢复。旧 messages.jsonl 在下一次成功追加后清理。尾部未完成消息先截断再写。最近200条相同消息重推去重；超窗不保证永久去重。已有 Room 和旧明确列出的客户端会话迁移为独立配置，旧渠道开关不会扩散到之后发现的新 Room。

## 查询与设置

Desktop 提供 ListRooms、ReadRoomByID、UpdateRoomSettings、ListRoomModels，类型由 MyGo 生成。HTTP GET /api/rooms、GET /api/rooms/{id}、PUT /api/rooms/{id}/settings 调用同一服务；保留按渠道/会话查询入口。控制 API 要求本机 peer 且无浏览器 Origin，MCP 另外要求 Bearer。卸载包仍可通过通用服务阅读历史。Room摘要包含持久外发异常与本次运行保留的任务异常，桌面与HTTP查询使用相同的宿主聚合结果；不维护已读或阅读位置。

## 执行与结果

固定 Codex app-server，按 Room 选择 model。输入为 trigger_messages、requesters、room_context；固定 Agent prompt 与 Room prompt 组合。conversation 可复用 thread/steer；snapshot 独立执行和合并等待状态。正式回复通过 rooms_reply HTTP MCP 保存，外发通过 messages_send HTTP MCP 授权后交渠道，二者没有隐式联动。

内部工具只接收正文及 reply_to/reply_id，宿主生成身份与当前 Room subject；兼容旧完整事件时仍校验其目标和来源。saved 是本地保存，sent 是渠道确认，unknown 不自动重发。外发请求 ID 与正文关联持久化，避免重启或重查造成再次发送。
