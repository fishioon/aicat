# 常驻进程发送 API

实现前列出的失败方式：未授权或浏览器跨站调用能发消息；配置误暴露公网；
重复 request_id、并发或进程重启导致重复发送；同 ID 不同正文被误当成同一请求；
联系人未找到、重名或映射错误导致误发；关闭 agent/reply 后仍发送；正文超限；
脚本失败或崩溃被当作成功/自动重试；错误泄漏正文和凭据；退出后监听未释放；
已授权桌面进程与 SSH 入口的权限混淆。

由 AICat 的 Serve Session 提供独立 HTTP 入口，与公开文件服务分开。
默认关闭，配置在唯一的 `~/.aicat/aicat.yaml`：

```yaml
messagesAPI:
  host: 127.0.0.1:19445
  tokenFile: /absolute/path/to/private/messages-api-token
```

tokenFile 为权限 0600 的普通文件，至少 32 字节随机凭据；凭据不写入 YAML。
host 只接受回环或 Tailscale 地址，拒绝通配地址和公网地址。
修改后重启 Serve Session。通过已授权的 AICat 应用启动；API 不授予 macOS UI 权限。

```http
POST /v1/messages/send
Authorization: Bearer <token>
Content-Type: application/json

{"agent":"work","target":"ai.fish","text":"账单内容","request_id":"bill-fish-202610"}
```

target 是脚本实际使用的精确搜索名，不是 room ID。只在该 agent 已知 Room 中匹配，
优先人工 search_name，否则读取本地渠道当前备注/名称；没有匹配返回 404，重名返回 409。
渠道无法读取或确认映射时返回 503，不忽略错误后继续猜测联系人。
首版支持企业微信本地及 iPad 微信文本（最多 5000 UTF-8 字节），沿用 enabled/reply.enabled、
本地会话验证和共享 GUI 锁。成功返回 `status: submitted_ui`、解析出的 roomid，
表示 UI 操作完成，不承诺对端已收到；实际送达通过渠道消息读取核实。

request_id 必填，跨重启持久保存在私有 data/messages-api 目录。相同 ID 和正文返回原结果，
不同正文/目标返回 409。开始发送前先持久化占位，崩溃或不确定失败不自动再次发送。
失败后的再次发送必须核对聊天记录后换新 request_id。

本地进程 E2E 使用隔离 HOME、合成加密企业微信数据库、记录型 AppleScript，
不访问真实联系人、不操作系统代理或钥匙串；命令及证据见 scripts/messages-api-e2e.py。

## 验证与安装边界

```sh
make app
python3 scripts/messages-api-e2e.py --binary bin/aicat --output build/messages-api-e2e-final
```

2026-11-02 本地结果：28 项进程 E2E 检查通过，产物
`build/messages-api-e2e-final/report.json` 包含二进制摘要与逐项断言；同目录有隔离配置、
加密数据库、UI 操作记录、跨重启持久请求回执及进程日志。重复运行需指定新的 output 目录。
macOS 两份内置发送脚本已编译检查，并改用与 doctor 一致的 `AXIsProcessTrusted()`，
不再依赖 `System Events.UI elements enabled`。未在真实会话中发送测试消息。

`make verify` 被已有 build 下两个临时调查文件的 gofmt 问题阻塞；全库 vet/test 还遇到
旧 `build/x-nickname-investigation-20261028-01` 缺少 go.sum 项。本次未修改这些实验文件或依赖。
等价的受维护代码检查已通过：

```sh
go vet ./cmd/... ./internal/... ./assets/... ./build/icon/... ./scripts/fixtures/...
go test ./cmd/... ./internal/... ./assets/... ./build/icon/... ./scripts/fixtures/...
```

初次验收只在本地修改、构建和验证；随后经用户明确授权，于 2026-11-02 安装到 mini。
`make app` 的本地未签名包不能直接覆盖已有授权安装；发布安装时必须沿用目标机证书签署
完整 app，核对 designated requirement 相同后再替换，以保持桌面授权身份。

### mini 安装与实际发送（2026-11-02）

实际启用 `127.0.0.1:19445`，Bearer 凭据保存在 mini 私有 secrets 目录，未对外发布监听。
安装过程未在 mini 编译源码：传输本地构建的 app 包，在已登录的 Aqua 会话中使用原证书
签署完整 app，严格校验签名并确认 designated requirement 与旧应用相同。
签名用的一次性 LaunchAgent 已卸载，未更改 TCC、钥匙串访问控制或系统代理配置。

首次生产调用在 UI 操作前返回 503：无关历史 Room 缺少名称被错误当作目标失败。
已修复为批量读取当前资料，忽略无名称的非候选会话，只对唯一匹配的目标执行适配器校验；
无法读取的资料、损坏映射、重名仍拒绝发送。增加该实际失败场景后，
`build/messages-api-e2e-live-fix/report.json` 的 29 项检查全部通过，并通过相关包 go vet。

- 通过验证的未签名二进制 SHA-256：
  `644ba338349b11c760a17699e44399c74324b5bf2f19b854c167ea1227598dba`。
- mini 已签名安装后二进制 SHA-256：
  `eae458186e7cf732c4329801bb7f4bda23ad6e3f3b714796176b155df712f9e3`。
- 新旧签名要求均为 `ai.netcat.aicat` 加同一证书指纹；应用 PID 在最终安装时为 68295。
- 未带凭据的请求返回 401；授权请求经 API 返回 200 / submitted_ui。
- 一条账单及后续按用户新规则更新的一条更正账单，均在预期私聊中读取到完整一致的正文；
  readback 消息 ID 分别为 `wecom-local:4226`、`wecom-local:4227`。更正消息明确替代前一条。

mini 的部署、私有回滚包与验收证据位于
`~/.aicat/deployments/messages-api-20261102T023720Z/`，目录权限 0700。
其中 `aicat.installed-before.app` / `aicat.previous.app` 为原应用，
`aicat.before.yaml` 为启用 API 前的配置；恢复时先正常停止应用，恢复旧包与配置后从应用入口启动。
账单正文、金额与凭据均仅保存在 mini 私有目录，未加入源码或文档。
