# 企业微信 BOT

企业微信 BOT 是可安装的 OpenClaw 渠道插件。AICat 只内置 Bun、渠道宿主与 SDK 兼容层，应用资源不包含企业微信插件或平台 SDK。推荐列表声明官方 npm 包 `@wecom/wecom-openclaw-plugin` 的固定版本 `2026.10.30`；当前支持一个长连接机器人账号、私聊文本接收与主动发送。群聊、媒体、插件附带工具、CLI 和 Webhook 尚未开放。

## 安装、配置与启用

1. 扩展 → 推荐 → 企业微信 BOT，点击安装。宿主使用应用内 Bun 从 npm registry 安装原版包及依赖到隔离暂存目录，禁用安装脚本，检查包名、版本及声明入口，成功后原子登记。安装不加载插件、不连接平台、不启用账号。
2. 安装后条目进入已安装列表，不带“内置”标记。详情展示发布者、包版本、配置表单及诊断入口。
3. 点击配置，填写 Bot ID 和 Secret，保存并验证账号。鉴权验证通过官方 SDK 建立一次短连接；验证阶段不加载渠道处理器、不写 Room、不触发 Codex，也不发送消息。验证成功或失败后关闭连接。配置错误或平台拒绝鉴权时启用仍被阻止。
4. 凭据和插件版本匹配且已通过验证后，才可点击启用。正式宿主加载已安装包并开始收发，状态区分连接中、已连接和错误。
5. 可单独开启 Codex 自动处理。Codex 使用本机的登录和模型配置；可执行文件留空时自动查找。
6. 停用断开连接、取消自动处理并回收子进程。安装包、凭据、Room 历史及发送回执保留；重启从本地加载，不重新下载依赖。SDK 兼容层引用当前应用资源，启动时在本地迁移旧拷贝或更新应用路径；实验与生产使用同一份兼容层源码。修改账号需先停用并重新通过验证。

行为设置保存在唯一的 aicat.yaml；安装与运行数据分别保存：

| 路径 | 内容 |
| --- | --- |
| extensions/wecom/current.json | 当前实际安装版本和依赖锁摘要 |
| extensions/wecom/<实际版本>/ | 原版插件、依赖、bun.lock 和安装记录 |
| cache/channel-packages/ | Bun 下载缓存 |
| credentials/wecom.json | Bot ID、Secret，0600，不回传界面 |
| credentials/wecom.json.validated | 当前凭据、插件版本与平台端点的验证指纹 |
| state/channel-host/ | 宿主进程锁和未确认事件 |
| rooms/<Room键>/messages.jsonl | 完整事件和发送状态流水 |
| rooms/<Room键>/context/messages.log | 一行一条旧版消息正文，供 Codex 读取历史 |
| rooms/<Room键>/tasks/ | 持久交付与执行状态 |
| rooms/<Room键>/agent.json | 持久 Codex thread 绑定 |
| messages/receipts/ | 出站 source+id 的发送回执 |

## 消息与 Codex

宿主使用 CloudEvents 1.0 推送入站。data 直接承载旧版消息正文，包含 msgid、action、from、tolist、roomid、msgtype、text.content；不嵌套 message，也不重复存放账号字段。账号归属由宿主绑定的来源和会话校验，SDK 的 AccountId 留在内部路由使用。

平台没有提供时间时保留 msgtime 缺失，按宿主首次接收并持久保存的 event.time 判断新鲜度；重推不刷新该时间。非私聊、不支持消息和超过十分钟的消息不触发 Codex。日志与待交付任务持久化成功后才确认渠道；写入失败不确认。入站当前不查重，确认丢失后的重推可能产生重复记录。

Room 按渠道账号、平台身份和会话隔离。上下文日志只记录入站消息及平台确认成功的发送消息，不混入 pending/unknown 状态。完整发送过程留在事件流水和回执中，结果不明不自动重发。

Codex 按需共享一个 app-server，每个 Room 独占持久 thread。普通触发用 turn/start，执行中的新消息用 turn/steer；完整消息通过 trigger_messages 直接交付，并附 requesters。历史按需从 context/messages.log 读取。任务失败、运行中崩溃或结果不明不自动重跑；未交付 pending 在渠道连接恢复后继续交付。任务启动时读取一次历史，此后使用待处理索引和最近 50 条快照，界面刷新不遍历历史文件。

文本超过单次 4000 字节时按 UTF-8 边界分段，每段持久保存稳定 ID 与发送状态，父回执的 parts 返回已尝试分段；任一段结果不明即停止后续段，同一请求不自动续发。上下文只追加已确认成功的分段。

普通 commentary/final_answer 在 agentMessage 完成时，由 AICat 经插件 outbound.sendText 主动发送到当前 Room。显式 messages_send 仍受当前 Room 约束；已通过工具发送的相同文本不再自动回发。企业微信插件内部使用官方 SDK 的 sendMessage，不依赖当前消息回调帧。

外部 MCP messages_send 使用独立 channel 参数选择渠道，例如 `{channel:"wecom",event:{...}}`；事件 source 固定为 aicat-agent://mcp，data 使用同一旧版正文结构。界面固定 source 为 aicat-ui://desktop，内部 Codex 使用 aicat-agent://codex。

网络故障导致插件退出后，宿主通知 Go 重建账号，退避上限 32 秒；鉴权重试耗尽或账号被其他实例踢下线时停止重连，需用户重新启用。

## 后台验收

```sh
make e2e-build
node e2e/wecom.cjs bin/aicat-e2e build/wecom-install-e2e
node e2e/wecom.cjs bin/aicat-e2e build/wecom-agent-e2e agent
```

场景从推荐列表安装真实 npm 官方包，使用隔离 Home、原版插件、原版 SDK 和本机合成 WebSocket 平台。浏览器 headless，原生窗口通过 AICAT_E2E_BACKGROUND=1 隐藏。报告包含包与 SDK 摘要、应用日志、平台帧、Room 文件和截图；不以合成平台替代真实平台鉴权和送达。

真实闭环使用 `node e2e/wecom-live.cjs build/wecom-live-<run>`，仅在用户已授权真实收发时运行。脚本先检查原宿主是否仍在运行；运行中则退出，不暂停或接管原连接。脚本只读真实 Home 的账号，确认日常连接已停用后，在隔离 Home 完成安装、真实鉴权、启用及 Codex 处理；用户发带唯一标记的私聊后核对任务与发送结果，并另行确认收件端。结束后关闭测试连接并删除复制的凭据，日常连接由用户在 AICat 中启用，不修改真实 Home 的配置。
