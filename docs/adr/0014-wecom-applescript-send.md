# aicat 直接通过 AppleScript 发送企业微信回复

状态：accepted

企业微信会话存档代理运行在 Linux 容器中，只负责归档解密、媒体流和用户查询；AppleScript、辅助功能
权限与企业微信窗口属于 macOS 当前 GUI 用户会话。`wecom-archive` agent 因此直接配置本机
`sendScript` 绝对路径，由 aicat 使用 `/usr/bin/osascript` 执行，不引入通用 Host Action 协议。

aicat 在每次发送时从当前 Room 的 `room.json` 读取人工维护的 `search_name`；私聊没有配置时回退到
触发时写入的 `users/<roomid>.json` 里的 `search_name`（企业微信内部成员是 email，外部联系人是
备注），群聊没有配置时直接报错。首次绑定 `thread_id` 保留已有发送配置，后续人工修改无需重启。
回退只读本地文件、忽略缓存有效期，也不发起查询，发送因此不依赖资料查询接口；不再通过
`/v1/rooms/get` 推导群聊发送目标。目标缺失或不可用时直接报错，不补值、不重试。企业微信普通任务
触发时按消息的 `from` 查询完整用户资料并落盘，资料使用 5 分钟文件缓存。
普通任务的资料查询失败只记录日志，不阻止交付 Codex；身份缓存与发送配置分别维护。

普通回复共用发送入口与超时；普通微信仍通过 Webox API 向原生 `roomid` 发送，
企业微信一次调用脚本，按顺序传入文字和图片。图片必须先解析为
当前 Room `inbox/` 或 `outbox/` 下的本机绝对路径。全部企业微信发送脚本共享串行执行边界，避免多个
Room 同时操作同一个 GUI。

## Consequences

- Webox 与 netcat-api 不返回或执行宿主机 action。
- `wecom-archive` agent 缺少绝对 `sendScript` 时配置校验失败。
- 企业微信群聊必须在 `room.json` 配置可搜索的名称；私聊可以改用触发时写入的对方资料
  `search_name`，旧 `groups/` 缓存不再用于路由。
- 脚本按名称查找私聊或群聊并发送文字与图片，不支持文件或发送结果回调。
- AppleScript 退出成功只代表自动化流程完成，不验证企业微信本地数据库；失败后不自动重试，避免重复发送。
