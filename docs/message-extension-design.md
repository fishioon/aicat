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

身份由绑定来源决定。msgtime 缺失时首次接收补齐；summary 是展示摘要，context_key 标识业务对象。self 表示当前账号发出的消息，只由 self 触发条件匹配；historical 只入历史，不触发。context/messages.log 只保存渠道提交的平台原始消息正文，不嵌套 account 或 event，不写入 Agent 外发或定时触发。

## 持久数据

| 路径 | 内容 |
| --- | --- |
| aicat.yaml / roomDefaults | 节点级 automatic、prompt、model、context 与触发默认值 |
| rooms/<hash>/room.json | 渠道、来源、平台会话绑定，Room 覆盖设置与 thread 状态 |
| rooms/<hash>/context/messages.log | 渠道提交的平台原始消息 |
| state/<channel> | 包宿主与渠道自身游标、采样、待确认队列 |
| logs/aicat.log | 运行状态、Agent 外发、定时触发与任务执行，不记录正文或凭据 |

任务只在内存保留，重启不恢复。旧 messages.jsonl 在下一次成功追加后清理。尾部未完成消息先截断再写。最近200条相同消息重推去重；超窗不保证永久去重。Room 注册不修改 `aicat.yaml`；用户设置和持久 thread 状态按 Room 原子更新 `room.json`。

## 查询与设置

Desktop 提供 ListRooms、ReadRoomByID、UpdateRoomSettings、ListRoomModels，类型由 MyGo 生成。HTTP GET /api/rooms、GET /api/rooms/{id}、PUT /api/rooms/{id}/settings 调用同一服务；保留按渠道/会话查询入口。控制 API 要求本机 peer 且无浏览器 Origin，MCP 另外要求 Bearer。卸载包后 Room 仍保留在列表中。Room 查询只返回绑定、展示名称、最近活动与设置，不聚合任务或外发状态。

## 执行与结果

所有渠道共享固定 Codex app-server 和 Agent 配置，按 Room 建立持久 thread 并选择 model。输入为 trigger_messages、requesters、room_context 和 source_room；固定 Agent prompt 只约束数据与权限边界，Room prompt 描述业务。Agent 启动时直接连接 aicat MCP，发送消息统一使用 messages_send。

messages_send 要求 target_room_id，可用 rooms_list 解析稳定 ID；reply_to 只在命中目标 Room 的平台消息时传给渠道，否则忽略。aicat 校验目标与渠道账号绑定后直接调用渠道实现，结果写入运行日志并返回，不保存外发记录。渠道决定确认类型；每次调用只发送一次，失败或结果不明不自动重发。
