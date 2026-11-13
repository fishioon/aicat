# 企业微信客户端

内置的客户端扩展，ID 为 `wecom-local`，默认停用，与可安装的[企业微信 BOT](wecom.md)分别配置和启停。应用内 Bun 执行 TS 读取器，从客户端本地 `message.db` 和 WAL 增量读取消息，经 stdio CloudEvent 写入现有 Room 服务。Agent 通过 aicat 的 MCP 请求客户端脚本发送文本；扩展不自动执行 Agent、不扫描进程内存或提取密钥。

## 使用

扩展 → 已安装 → 企业微信客户端 → 配置，填写本地账号 ID、含 `message.db` 的数据库目录，以及手动获取的 Base64 原始密钥。密钥解码后必须为 16 字节，是企微传入数据库的派生前输入；不是旧 `keys.json` 中的最终 AES 密钥。旧配置需要重新填写这个输入。

保存时使用未修改的 SQLite3 Multiple Ciphers 2.7.0 验证密钥、数据库完整性与消息表结构，验证不导入消息。密码式输入框不回显已保存密钥；留空只保留同一账号和数据库目录的密钥。更换账号或目录需重新填写，错误密钥不覆盖已有有效配置。

启用后导入本地历史，之后每秒检查增量。诊断页显示本次运行最近 50 条消息，区分收到、发出及媒体元数据；正文默认简短预览，可展开原文，并提供打开 Room 日志入口。停用回收专用 Bun 宿主，保留配置、游标和 Room。

`aicat.yaml` 只保存行为配置和目录，不保存密钥：

```yaml
extensions:
  wecom-local:
    enabled: false
    local:
      accountId: your_account_id
      dataDir: /absolute/path/to/account/Data
```

密钥与上述账号、目录绑定，保存于 Home 下的 `credentials/wecom-local.json`（0600），不回传界面，不进入 YAML、Room、日志或进程参数；只经宿主 stdin 传递。Base64 只是二进制编码，按凭据保管。当前不自动获取密钥，也不附加企业微信进程或申请调试权限；后续提取工具不属于当前实现。

## 数据与恢复

- 应用随包携带由未修改上游源码构建的 SQLite3 Multiple Ciphers 2.7.0 动态库，Bun 通过 `Database.setCustomSQLite` 加载。Base64 解码后的 16 字节经 `PRAGMA hexkey` 按显式长度交给 `aes128cbc`（`legacy=0`）；库执行口令派生、按页解密和 WAL 处理。没有自定义 C 密钥适配，也不生成明文数据库。
- 数据库连接为 readonly 并设置 query_only；每批在 SQLite 读事务中查询，允许企微并发写入。文件被替换或变化后释放旧连接再重开，读取失败不推进游标。SQLite 可维护自身的 `-shm` 协调文件，但不能写入 DB 和 WAL 正文。
- WAL 的可见性、checksum、提交边界和读锁交给 SQLite 库处理，未提交或撕裂尾部不作为新消息。
- 游标按账号和数据目录隔离，使用本地 `message_id`，与消息时间无关；首次从 0 导入已有历史。Room 日志和纯消息上下文同步落盘后才确认，读取器收到确认后原子保存游标。
- 事件 ID 由来源和本地消息 ID 确定。Room 以持久事件和消息 ID 校验重放，补齐缺失上下文，避免确认丢失或崩溃恢复造成重复追加。同一标识内容变化时拒绝接收。
- 重启校验最后消费消息的指纹；最大 ID 倒退、锚点丢失或被替换时停止推进，不自动清除进度。更换账号或数据目录使用独立来源与游标；BOT 的配置、宿主和 Room 命名空间不共用。
- `state/wecom-local/` 保存专用宿主锁和 `cursor-<identity>.json`；`rooms/*/messages.jsonl` 保存事件，`rooms/*/context/messages.log` 保存消息，文件权限为 0600。诊断只在内存保留最近 50 条正文，去重索引只保存摘要。

## 范围

SQLite3 Multiple Ciphers 是 Ulrich Telle 维护的 MIT 开源项目，不是企业微信官方 SDK。构建锁定 v2.7.0 的源码归档及 SHA-256，应用不修改上游源码。消息类型映射参考 MIT 项目 tzwkb/wecom-agent。

当前适配 `message_table` 的既有 schema，Mini 的 macOS 企业微信 5.0.13 已验证真实只读导入和重启恢复。它读取客户端已落库的消息，不受机器人 @ 提及条件限制；不保证客户端未同步或已清理的历史存在。

正文保留 `raw_content` Base64，文本沿用 protobuf 叶子与 XML 启发式解析，不是完整官方协议解码。`local_id`、`server_id`、`sequence`、发送者 ID 用字符串保留 64 位精度。保留发送方向、原始类型、稳定会话 ID；群聊与私聊均记录为 `room_type: unknown`，不推断成员关系。图片、语音、文件等仅保留原始载荷和元数据，不读取媒体正文。

主库和 WAL 各限 128 MiB，单条原始正文限 8 MiB，每批至多 500 条及 32 MiB 原始正文。读取失败、结构不兼容或超限不推进游标。当前仅支持文本发送，不提供媒体发送、Agent 自动触发、联系人名称补全、自动找库或自动获取密钥。

## MCP 发送文本

Agent 调用 aicat 的 `messages_send`，选择 `channel: "wecom-local"`。扩展与 BOT 的账号和发送通道独立；不会自动把 Agent 的 commentary 或最终回答转发出去，也不会从消息历史自动启动任务。

```json
{
  "channel": "wecom-local",
  "event": {
    "specversion": "1.0",
    "id": "reply-request-unique-id",
    "source": "aicat-agent://mcp",
    "type": "org.aicat.message.send.requested.v1",
    "datacontenttype": "application/json",
    "data": {
      "action": "send",
      "roomid": "original-conversation-id",
      "search_name": "可唯一搜索的会话名称",
      "msgtype": "text",
      "text": {"content": "回复正文"}
    }
  }
}
```

`roomid` 必须是此账号已读入 Room 的真实会话；`search_name` 由调用方明确提供，不从数字 ID 猜测。必须使用能唯一搜索到目标的名称，脚本不负责消除同名搜索结果。单次正文最多 4000 UTF-8 字节，空文本、不支持类型和缺失目标在脚本执行前拒绝。

macOS 随包的 `send.applescript` 沿用会话搜索、粘贴和回车流程。请求 JSON 通过 stdin 传递，不把正文放入进程参数。发送会唤起企业微信、切换会话并使用剪贴板，需要登录客户端及辅助功能权限；锁屏或无可用桌面时可能无法完成。

发送串行执行。调用脚本前，0600 回执和 Room 事件流水先记录 pending。脚本结束后，从 DB/WAL 回读同一会话中新出现、正文一致且标记自发的消息；只有唯一匹配才记 sent，返回 `result.confirmation: "local_database"` 和本地消息 ID。这表示客户端已落库，不表示服务器送达、对端收到或已读。脚本异常、超时、确认缺失或歧义记 unknown。

诊断页在本次运行中展示发送请求的 pending、sent 或 unknown；同一请求的状态更新在原行，不追加重复行，已展开正文保持展开。

同一来源和请求 ID、相同完整内容返回原回执，内容变化拒绝；pending 在恢复时转 unknown，unknown 不自动重发。修改搜索名也属于不同内容。出站请求与状态保存在 `messages/receipts/` 和同一 Room 的事件流水；聊天上下文只由数据库读取器追加实际自发消息，避免发送确认与读取回流各追加一次。

## 验证

仅运行受影响流程，全部使用后台方式：

```sh
make cli channel-check
python3 e2e/wecom-local.py
python3 e2e/wecom-local-send.py
make e2e-build
node e2e/wecom-local-ui.cjs bin/aicat-e2e build/wecom-local-e2e-<run>
```

`wecom-local.py` 使用独立 Python/OpenSSL 合成加密主库和 WAL，启动真实 CLI、Bun 读取器及 Room 服务。覆盖停用不读取、WAL 增量、64 位 ID、旧时间消息、自发消息、媒体元数据、Room 写入失败、确认重放、崩溃恢复、错误密钥与非法 Base64、损坏 WAL、撕裂尾部和数据库锚点变化。工件包含报告、应用日志、合成数据库、私有合成凭据、游标和 Room，可通过 `--output <新目录>` 固定位置。没有单元测试或真实消息发送。

`wecom-local-send.py` 通过真实 HTTP MCP、Bun、加密 DB/WAL 与 Room 服务验证鉴权、目标校验、脚本提交、正文回读确认、请求幂等、失败与崩溃恢复、上下文无重复。仅在脚本执行边界替换 native UI，不弹出窗口，不以此代替真实客户端发送验收。

运行错误保留在服务状态中，界面将数据库读取、完整性、进度与组件等错误转换成简短的本地化提示；文件恢复后读取器自动重试，原进度与消息去重保持不变。

配置草稿仅在已保存的本地配置发生变化时同步；主题、语言或运行状态刷新不会覆盖未保存的账号、目录和密钥。

独立界面验收使用固定夹具，包含普通收发、媒体元数据和多行长文本，不依赖读取恢复场景结束时的数据库状态：

```sh
python3 e2e/wecom-local-fixture.py --output build/wecom-local-ui-fixture-<run>
node e2e/wecom-local-ui.cjs bin/aicat-e2e build/wecom-local-ui-fixture-<run> build/wecom-local-ui-<run> review
```

`error-review` 场景使用私有夹具副本暂时移走数据库，核对中英文错误说明、文件恢复后的自动续读和无重复消息。

`review` 场景核对无关刷新保留草稿、收发方向、媒体说明、预览展开后的完整原文和英文深色界面。将最后一个参数改为 `send-review`，额外通过真实 MCP 与 Bun 验证发送中到结果不明的原行更新、展开状态及幂等重查，并用私有夹具副本的加密本地回流验证已发送状态；仅替换原生脚本边界，不操作真实客户端。

`wecom-local-ui.cjs` 使用合成夹具，headless Chromium 与 `AICAT_E2E_BACKGROUND=1` 验收配置验证、独立入口、启停和诊断，保存浅深色截图、日志和报告。不覆盖真实 WKWebView 指针事件或原生窗口显示。

真实本地库验收在数据库所在节点运行，输出目录必须全新且私有：

```sh
python3 e2e/wecom-local-live.py \
  --binary /absolute/path/to/bin/aicat-cli \
  --account your_account_id \
  --data-dir /absolute/path/to/account/Data \
  --key-stdin \
  --output /absolute/path/to/new-private-artifacts
```

`--key-stdin` 从标准输入接收一行 Base64，不在命令参数中传密钥；也可以用 `--key-file` 读取只含 Base64 的私有文本文件，或字段为 `key` 的 JSON 文件。真实验收结束后清除隔离 Home 中复制的密钥。真实 Room 正文仅留在该节点的隔离工件目录。报告只输出条数和检查结果，不打印正文或密钥；只验证读取、落盘和重启，若期间无新消息，不把它称为真实新消息增量验收。

真实客户端发送验收在本地完成实现和后台验收后，部署到 Mini 再执行：

```sh
python3 e2e/wecom-local-send-live.py \
  --home /Users/fish/.aicat \
  --room original-conversation-id \
  --search-name '用户指定的唯一测试会话名称' \
  --output /absolute/path/to/new-private-artifacts
```

该入口只用于用户已明确授权的真实 UI 与发送测试。它通过正式 MCP 发送一条带唯一标记的短文本，再用同一请求查询回执，核对本地自发消息与 Room 上下文只出现一次。操作会使用 Mini 的企业微信窗口和剪贴板；不在开发本机执行真实按键。工件包含私有请求、回执和不含正文的报告；客户端落库确认与收件端实际收到分别验收。
