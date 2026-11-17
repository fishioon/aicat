# 企业微信 BOT

扩展 → 推荐 → 企业微信 BOT，从 NPM 安装官方 `@wecom/wecom-openclaw-plugin` 固定版本，再填写 Bot ID/Secret、保存验证并启用。应用自带 Bun，平台通信复用官方SDK，兼容适配不允许插件自行启动Agent或隐式发送。

扩展页仅负责安装、平台鉴权、启停与诊断，不提供 Codex、自动处理或模型设置。私有Secret不进入YAML、日志或页面状态，验证连接结束后关闭；启用建立长连接，停用回收宿主，卸载保留凭据和Room。包更新先暂存校验再切换，失败保留旧版。

平台会话按绑定账号和会话ID映射到Room，私聊或群聊各自隔离，同一群内不同发送者进入同一Room。支持SDK提供的单聊/群聊文本；Chat 模式只认私聊或结构化 `@` 当前账号，其他群消息可使用 Room 的规则匹配。消息先同步写Room再确认，断线待确认队列恢复；最近200条相同内容重推不重复追加或执行，冲突拒绝。非文本以元数据保存，不下载媒体，历史、自发与过期消息不触发。

在「消息」选择Room，设置自动处理、触发、Room prompt、context与model。固定Codex app-server，普通聊天复用thread/steer，任务重启不重跑。固定Agent prompt始终与Room prompt组合。

Agent通过messages_send MCP指定目标Room并交原插件sendText发送；先保存意图，长文本按UTF-8边界分段，超时或断线为unknown、不自动重发。相同外发请求返回持久回执，不重复平台调用。sent仅代表渠道确认，不代表已读。

后台验证使用真实官方包和SDK、隔离Home、本地WSS平台夹具与headless页面：

```sh
make e2e-build
node e2e/wecom.cjs bin/aicat-e2e build/wecom-run
node e2e/wecom.cjs bin/aicat-e2e build/wecom-agent-run agent
```

覆盖真实Bun安装与鉴权、凭据隔离、收消息/Room确认、写盘失败重推、启停、MCP鉴权、外发状态、重启与Agent协议。平台和模型均为协议替身，不证明真实企微送达；真实平台入口wecom-live.cjs单独授权使用。统一Room、两层prompt、模型和消息页验收见 `node e2e/rooms.cjs`。
