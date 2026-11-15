# 定时消息

## Sub-features

- `schedules-manage`：通过 MCP 创建、替换、查询、停用与删除绑定 Room 的定时规则（Schedule）。
- `schedules-deliver`：单次或周期到点后向原 Room 保存系统消息，驱动原 Agent，显式发布回复或按授权外发。
- `schedules-lifecycle`：规则跨重启保留、只恢复未来触发、错过与失败不重试、不补跑、账号隔离。

## How to get to it (user POV)

在已启用自动处理的 Room 中向 Agent 提出定时需求，例如「每天 18:00（Asia/Taipei）总结本群并发到群里」。Agent 通过以下动态工具管理当前 Room 的规则，时间或时区不明确时先询问：

```json
{
  "id": "daily-summary",
  "schedule": {
    "name": "每日群总结",
    "prompt": "读取本群今天的消息，生成总结，通过 rooms_reply 保存，再通过 messages_send 发到本群并关联 reply_id。",
    "cron": "0 18 * * *",
    "timezone": "Asia/Taipei",
    "enabled": true
  }
}
```

上例是 schedules_set 的参数。单次任务使用带偏移的 RFC3339 at 替代 cron 和 timezone。schedules_list({}) 返回当前 Room 的规则和下一触发时间；复用 id 与完整 schedule 替换内容或以 enabled:false 停用，schedules_delete({id}) 删除。外部 MCP 客户端须为这三个工具补充 room_id，其值取 GET /api/rooms 的稳定 id；Room 内 Agent 的工具由宿主绑定，不能指定其他 Room。

到点消息可在「消息 → 对应 Room」查看，标为「定时系统消息」。没有独立 Task 管理页面。创建规则需要当前账号和 Room 就绪；停用、删除可在渠道离线时执行。

## Driving it with HTTP and MCP

执行 `make e2e-build` 后，运行 `node e2e/schedules.cjs bin/aicat-e2e build/schedules-<run>`。使用独立 Home、真实 Bun 安装与渠道传输、真实 Room 存储和调度、认证 HTTP MCP，以及 headless 浏览器和隐藏原生应用。

验收覆盖 Agent 创建、查询、修改、停用、删除与跨 Room 参数拒绝；单次触发与五段 cron 时区；实际分钟边界上的 cron 投递与下一次时间；普通提及规则过滤与系统消息区分；入站不能伪造 schedule；snapshot Room 的定时消息不被合并；显式回复与按授权外发；自动处理或渠道关闭时跳过且恢复后不重试；重启恢复未来计时器、不补跑过期单次及离线错过的 cron；Room 落盘失败不执行、不重试；账号切换隔离。消息页面截图证明系统消息与关联回复可见。

真实 Agent 验证使用 `node e2e/schedules.cjs bin/aicat-e2e build/schedules-agent-<run> real-agent`，通过本机已登录的 Codex app-server 和真实模型，按自然语言创建、查询、停用和删除规则，再通过 rooms_reply 发布确认。可用 AICAT_CODEX_BINARY 指定二进制；使用隔离 Codex Home，结束时移除临时认证引用，不修改真实 Codex 配置。此入口不加入默认后台全套，避免自动产生模型调用。

工件包含 report.json、app.log、codex-trace.jsonl、mcp.json、scheduled-room.png 和隔离配置；真实 Agent 模式保存 real-codex-trace.jsonl 与 Codex 版本。cron 触发与离线跳过等待真实分钟边界，不修改时钟或运行游标。默认场景的模型与平台是协议夹具；real-agent 模式使用真实 Codex 和模型、平台仍为夹具。两者均不证明真实平台送达或原生窗口交互。

## Gotchas

- cron 为五段，最低每分钟一次；timezone 必须显式指定 IANA 时区，at 自带时区偏移。不唤醒睡眠或已退出的 aicat，运行中的计时行为遵循 Go 定时器和 cron 库。
- 定时规则只投递消息，不表示 Agent 已完成或平台已发送。回复与外发结果仍查看 Room；调度服务不维护独立执行或交付状态，投递失败保留运行日志。
- Room 自动处理关闭、渠道停用/断连或账号已切换时跳过本次，不绕过授权。周期等下一个 cron 时间，单次到点先持久停用再尝试投递，失败不自动重试。进程重启不补跑错过的 cron 或过期单次。
- 相同 Room 与 id 的 schedules_set 完整替换规则；相同内容的未触发规则不会重复注册。不同 Room 可使用相同 id。已消费或已过期的单次规则需要指定新的未来时间才能重新启用。
- 修改、停用和删除只影响后续触发，已进入 Room 的消息和 Agent 处理继续沿用原链路。规则最多 1000 条，仅在 aicat.yaml 保存定义，无独立运行状态文件。
