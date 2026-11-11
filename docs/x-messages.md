# X 私信接入

`kind: x` 用官方 [Chat XDK Go v0.5.2](https://github.com/xdevplatform/chat-xdk/tree/go/chatxdk/v0.5.2/go/chatxdk)
连接 XChat 加密私信。适配器在 aicat 进程内运行，不启动额外 runtime，不保存聊天 PIN。
支持一对一文字消息和文字回复。通过 X Activity Stream 长连接收取事件，
不再定时查询会话列表；群聊和图片尚未接入。
当前仅保留 X 读写适配器，AI 准入暂停。旧绑定链接提示已删除；后续认证方案另行设计。

## 配置

行为配置仍只有 `~/.aicat/aicat.yaml`：

```yaml
claw:
  codex:
    binary: /absolute/path/to/codex
    model: YOUR_CONFIGURED_CODEX_MODEL
  agents:
    - id: x-main
      kind: x
      enabled: true
      reply:
        enabled: true
      tokenFile: /absolute/path/to/.aicat/credentials/x-main.json
      x:
        accountID: "2087789522482503680"
      trigger:
        chat:
          enabled: true
      prompt: 使用中文简洁回复。
```

`enabled`、`reply.enabled` 均须显式开启。`trigger.chat.enabled` 控制私聊任务触发；
模型、Skills、MCP、Room、turn 和文件/网络权限沿用普通入口，不另设 X 权限策略。
`claw.codex.model` 填本机已授权可用的 Codex 模型；可在 agent 的 `model` 字段覆盖。
当前 app-server 若无法自动取得模型目录，会返回 `No models are available`，此时需要显式填写模型。
每段会话对应独立持久 Codex thread。X 数字账号 ID 不等于 @用户名。发送者还须先完成
当前的[聊天身份流程](identity-binding.md)仅支持企业微信，X 消息不会触发 AI，也不返回关联链接。

凭据 JSON 设为 `chmod 600`，父目录设为 `chmod 700`，不要提交到 Git：

```json
{
  "account_id": "2087789522482503680",
  "app_bearer_token": "App-only Bearer Token from the same developer App",
  "access_token": "OAuth 2.0 user access token",
  "refresh_token": "OAuth 2.0 refresh token",
  "client_id": "developer App client ID",
  "client_secret": "confidential App client secret",
  "private_key_b64": "official SDK ExportKeys result, Base64",
  "signing_key_version": "existing XChat key version"
}
```

使用官方 SDK `Unlock` 恢复已有 XChat 密钥后导出；不要为已有聊天账号另建替代密钥。
OAuth 授权须包含当前 XChat 所需的收发/公钥权限及 `offline.access`，App 需有可用额度。
令牌过期返回 401 时自动刷新，旋转后的令牌原子写回同一文件。
App Bearer Token 用于长连接和查询订阅，用户令牌用于创建私信订阅、查询密钥和发送回复。
启动时检查并补齐当前账号的 `chat.received` / `chat.sent` 持久订阅，不改动其他订阅。
私信订阅的用户授权须包含 `dm.read`。同一个 App 只启动一个流消费者，避免多个进程争抢事件和刷新令牌。
旧配置的 `pollIntervalSeconds` 仅为兼容保留，已无作用，不会自动回退到轮询。

## 消费和验签

- 双方公钥均加载进官方 SDK，包括账号自己发出的消息。严格验签始终开启；失败时刷新公钥重试一次。
- 首次启动建立时间边界，不触发旧消息；旧 polling 游标会迁移到流式游标。
- 长连接使用 `GET /2/activity/stream?start_time=<最近5分钟的UTC时间>`，连接超时与 REST 请求分开。
  当前账号不允许 `backfill_minutes` 参数，已实测 `start_time` 可用。响应头最多等待 45 秒；
  空行心跳保持连接，90 秒没有任何字节行时取消并重连。断线指数退避，最大 1 分钟；
  429 按服务端冷却时间等待，401/403 冷却 1 分钟后重新读取本地 App Token。
- 每个待交付事件先原子写入私有 `x-stream-pending.json`；Claw 持久化返回游标并在下一次读取带回后才清除。
  持久游标分别保存最近 2048 个 `event_uuid` 和已验签消息 ID，处理重放和重复投递。
  消息身份使用已验签的 UUID，不使用服务端分配的 unsigned sequence ID。
- 重连和重启使用最多 5 分钟的 backfill 补漏，不能保证恢复更长离线期间的全部消息；
  沿用 Claw 的 10 分钟触发时效，较旧文本不会触发任务。backfill 不会反复触发已提交消息。
- 流事件提供 conversation key change 时由官方 SDK 严格验签并缓存，回复直接用缓存密钥；
  只有密钥缺失时才按需读取一次会话事件补齐，不为回复每次回查历史。消息请求可以从流接收并回复，
  本入口不再遍历或自动接受历史收件请求。
- 无法验签/解密的新事件写入 `quarantine/<event_uuid>.json` 后跳过，不交给 Agent；
  记录只有事件、会话 ID 和原因。待提交文件仅保存必要的加密事件，文件权限为 0600。
- 自己发出的消息可入上下文，正常 `chat` 触发器不响应自身消息。不要开启 `regex.self` 来创建回声循环。
- 对方发送独立的 `STOP` 或 `停止` 后该会话暂停自动回复；`START` 或 `恢复` 重新开启，状态跨重启保留。
  这些命令在事件被消费时生效；已经提交给 X 的回复无法撤回。
- REST 端点分别保存冷却时间，流的冷却与回复发送独立；401 刷新不消耗 429 重试次数。
  同时存在 reset 和 Retry-After 时取较晚时间并留 1 秒余量。明确的 429 最多尝试 3 次；发送结果未知的网络错误不自动重发，以免重复发送。
  沿用 Claw 现有交付语义，不承诺崩溃前后任务/出站消息恰好一次；发送失败查看日志后人工确认，避免盲目补发。

## 诊断与验证

```sh
bin/aicat messages --agent x-main doctor
bin/aicat messages --agent x-main read
bin/aicat messages --agent x-main send --room 1374290616-2087789522482503680 --stdin
```

`doctor` 检查用户授权、公钥和私信订阅；`read` 空游标返回新边界，之后传 `--cursor` 等待下一条流事件。
不要与正式运行体同时对同一账号执行流读取；`read` 不修改 Claw 的 cursor，但共享私有待交付状态。
X 不需要 UI 搜索名或辅助功能权限。`apiURL` 默认 `https://api.x.com`；仅本地 E2E 使用 loopback HTTP。

SDK 含官方 Rust 静态库，要求 cgo；支持 macOS arm64/amd64 和 Linux amd64。
`make cli` 保持纯 Go，可构建，但启用 X 时明确报不支持。SDK 与其依赖声明见
[第三方许可](third-party/xchat/THIRD_PARTY_NOTICES.md)，macOS app 包内也包含声明。

结束开发后运行：

```sh
make verify
make cli
python3 scripts/xchat-e2e.py --binary bin/aicat
```

E2E 启动真实 aicat 进程和官方 SDK，用公开测试密钥构造有效/无效签名消息；
X HTTP 与 Codex RPC 使用本地确定性 fixture，HOME 隔离，不改变系统代理或真实 Keychain。
输出可重复检查的 `report.json`、日志、RPC、游标和收发记录。真实 X → 真实 Agent 的联调需另行发测试私信。

部署统一在 mini 节点。更新 `/Applications/aicat.app` 前必须用目标机原有证书签署完整 app，
核对新旧 designated requirement 一致并通过 `codesign --verify --deep --strict`，保留配置和回滚包。
本机 X 入口保持关闭，现有会员身份准入和 Agent 权限保持共用流程。
