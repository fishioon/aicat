# 企业微信客户端

内置的客户端扩展，ID 为 `wecom-local`，默认停用，与可安装的[企业微信 BOT](wecom.md)分别配置和启停。应用内 Bun 执行 TS 读取器，从客户端本地 `message.db` 和 WAL 增量读取消息，经 stdio CloudEvent 写入现有 Room 服务。Agent 通过 aicat 的 MCP 请求客户端脚本发送文本；可按明确配置的会话自动触发 Codex，不扫描进程内存或提取密钥。

## 使用

扩展 → 已安装 → 企业微信客户端，在详情中填写本地账号 ID、含 `message.db` 的数据库目录，以及手动获取的 Base64 原始密钥。密钥解码后必须为 16 字节，是企微传入数据库的派生前输入；不是旧 `keys.json` 中的最终 AES 密钥。旧配置需要重新填写这个输入。

保存时使用未修改的 SQLite3 Multiple Ciphers 2.7.0 验证密钥、数据库完整性与消息表结构，验证不导入消息。密码式输入框不回显已保存密钥；留空只保留同一账号和数据库目录的密钥。更换账号或目录需重新填写，错误密钥不覆盖已有有效配置。

启用后导入本地历史，之后每秒检查增量。诊断页显示本次运行最近 50 条消息，并提供打开 Room 日志入口。停用回收专用 Bun 宿主，保留配置、游标和 Room。

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
- `state/wecom-local/` 保存专用宿主锁、`cursor-<identity>.json` 和 `message-format-<identity>.json`。格式状态固定升级时的本地 ID 分界，旧消息重放保留原有正文及 unknown 元数据，新消息使用结构化提及解析，避免既有 Room 和任务内容冲突；`rooms/*/messages.jsonl` 保存事件，`rooms/*/context/messages.log` 保存消息，文件权限为 0600。诊断只在内存保留最近 50 条正文，去重索引只保存摘要。

## 范围

SQLite3 Multiple Ciphers 是 Ulrich Telle 维护的 MIT 开源项目，不是企业微信官方 SDK。构建锁定 v2.7.0 的源码归档及 SHA-256，应用不修改上游源码。消息类型映射参考 MIT 项目 tzwkb/wecom-agent。

当前适配 `message_table` 的既有 schema，Mini 的 macOS 企业微信 5.0.13 已验证真实只读导入和重启恢复。它读取客户端已落库的消息，不受机器人 @ 提及条件限制；不保证客户端未同步或已清理的历史存在。

正文保留 `raw_content` Base64，新消息优先按已识别的文本和提及分段拼接可见正文，不兼容的格式再递归解析 protobuf 叶子及 XML；这不是完整官方协议解码。`local_id`、`server_id`、`sequence`、发送者 ID 用字符串保留 64 位精度。按发送者 ID 是否等于配置账号判断自发方向，保留原始 flag、类型和稳定会话 ID；已识别的 `R:<数字>` 会话为 group，包含当前账号的 `S:<账号>_<账号>` 会话为 direct，其余为 unknown。文本 protobuf 的分段类型 5 提供数值提及 ID，ID 0 表示所有人；按结构保留可见提及文本并输出 `mention_ids`、`mention_all` 和 `mention_status`，不靠正文中的显示名判断提及，也不推断成员关系。图片、语音、文件等仅保留原始载荷和元数据，不读取媒体正文。

主库和 WAL 各限 128 MiB，单条原始正文限 8 MiB，每批至多 500 条及 32 MiB 原始正文。读取失败、结构不兼容或超限不推进游标。当前仅支持文本发送，不提供媒体发送、联系人名称补全、自动找库或自动获取密钥。

## Room 新消息自动处理

先停用扩展，在配置中添加自动处理会话的 Room ID 和唯一搜索名称，保存数据库配置，再开启 Codex 自动处理并保存。Codex 路径留空使用本机已安装、已登录的 Codex；设置保存在 `extensions.wecom-local.agent`，会话映射保存在 `local.replyRooms`。只处理明确列出的会话。每个会话独立选择私聊新文本、群聊 @ 当前账号、群聊关键词或所有新文本（测试）；任一条件满足即触发，同一消息只创建一个任务，未命中条件的消息仍进入 Room。新配置缺省开启私聊与群聊 @ 当前账号，关闭 @所有人和全量触发。已有 `replyRooms` 没有对应触发规则时保留原有全量行为，界面显示为测试选项。

```yaml
extensions:
  wecom-local:
    enabled: true
    local:
      accountId: your_account_id
      dataDir: /absolute/path/to/account/Data
      replyRooms:
        "R:conversation-id": "唯一会话搜索名称"
      triggers:
        "R:conversation-id":
          direct: true
          mention: true
          mentionAll: false
          all: false
          keywords: ["虾群助手"]
    agent:
      enabled: true
      binary: /absolute/path/to/codex
```

首次启用以连接时的本地消息最大 ID 作为自动处理起点，保存在 `state/wecom-local/automation-*.json`；起点以前的历史只导入 Room，不触发任务。重启和重连保留起点，停用自动处理后重新开启会重新选取起点。只有起点之后、十分钟以内、发送者明确且不是自己的非空文本触发任务；未来超过一分钟、媒体和未配置会话不触发。

群聊提及必须来自可解析的 protobuf 元数据并匹配当前账号 ID；手打 `@名字`、提及其他账号以及默认关闭的 `@所有人` 不满足个人提及条件。关键词按正文包含匹配，忽略大小写，不使用正则，每会话最多 32 项、每项最多 128 字节。未知会话类型不满足私聊、群提及或群关键词规则；明确选择全量测试模式时仍可触发。任务诊断显示触发原因。

客户端复用 messages 的持久任务调度，每个 Room 独占持久 Codex thread，空闲时 turn/start，运行中的新消息使用 turn/steer，完整消息通过 trigger_messages 交付。Room 及待处理任务保存成功后才确认读取；相同记录不会把 completed/failed/unknown 任务改回 pending。未完成 running 在恢复时转 unknown，不自动重跑。任务状态在客户端诊断中展示。

Room Agent 调用 `messages_send({event:{text:"回复正文"}})`，只提供正文。工具适配器绑定当前 Room、客户端渠道及配置的搜索名，按持久任务 ID 与工具调用 ID 生成稳定请求 ID，组装完整 CloudEvent，再调用主程序的 HTTP MCP；MCP 凭据不交给模型。已有会话仍可提交旧版完整事件，目标和正文校验后使用宿主生成的 ID；非法参数使任务失败，不执行发送。客户端在 `agent.json` 保存指令摘要，恢复旧会话时通过 Codex 指令注入接口追加已变更的开发者指令，保留会话历史，同一版不重复追加。外部 MCP 调用仍使用下文的 CloudEvent 接口。回复仍通过已有的 MCP 发送与回执能力完成，普通 commentary/final_answer 不自动转发。自己的数据库回流不会再次触发 Agent。发送不确定时任务转 unknown，当前 turn 停止进一步发送，不换 ID 重试。

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

发送串行执行。调用脚本前，0600 回执和 Room 事件流水先记录 pending。脚本最多等待 12 秒，正常完成回车发送后立即记 sent，返回 `result.confirmation: "client_script"`、会话 ID 和渠道，不返回数据库消息 ID，不等待 DB/WAL 回流。这表示客户端脚本已执行发送动作，不表示服务器送达、对端收到或已读。脚本异常、超时或宿主中断记 unknown。

同一来源和请求 ID、相同完整内容返回原回执，内容变化拒绝；pending 在恢复时转 unknown，unknown 不自动重发。修改搜索名也属于不同内容。出站请求与状态保存在 `messages/receipts/` 和同一 Room 的事件流水；聊天上下文只由数据库读取器追加实际自发消息，避免脚本发送与读取回流各追加一次。

## 验证

自动触发的定向验证使用 `python3 e2e/wecom-local-agent.py`。该场景使用真实 DB/WAL、Room、任务和 HTTP MCP，以协议夹具替代模型、以脚本边界夹具替代 native UI；覆盖历史/自发/过期过滤、自动回复、重放、重启、turn/steer 和 MCP 鉴权失败，以及自发消息尚未落库时任务完成、后续回流不重复触发。

仅运行受影响流程，全部使用后台方式：

```sh
make cli channel-check
python3 e2e/wecom-local.py
python3 e2e/wecom-local-send.py
python3 e2e/wecom-local-triggers.py
make e2e-build
node e2e/wecom-local-ui.cjs bin/aicat-e2e build/wecom-local-e2e-<run>
```

`wecom-local.py` 使用独立 Python/OpenSSL 合成加密主库和 WAL，启动真实 CLI、Bun 读取器及 Room 服务。覆盖停用不读取、WAL 增量、64 位 ID、旧时间消息、自发消息、媒体元数据、Room 写入失败、确认重放、崩溃恢复、错误密钥与非法 Base64、损坏 WAL、撕裂尾部和数据库锚点变化。工件包含报告、应用日志、合成数据库、私有合成凭据、游标和 Room，可通过 `--output <新目录>` 固定位置。没有单元测试或真实消息发送。

`wecom-local-send.py` 通过真实 HTTP MCP、Bun、加密 DB/WAL 与 Room 服务验证鉴权、目标校验、脚本成功即返回、无 DB 回流仍成功、请求幂等、脚本失败/超时与崩溃恢复、上下文无重复。仅在脚本执行边界替换 native UI，不弹出窗口，不以此代替真实客户端发送验收。

`wecom-local-triggers.py` 使用加密 DB/WAL、真实 Room/任务和 HTTP MCP，验证私聊、账号 ID 提及、关键词、@所有人开关、同条消息去重、未命中入库、会话隔离、未知元数据、历史过滤及格式升级重放；模型与客户端 UI 仅在协议边界使用夹具。

`wecom-local-ui.cjs` 使用读取验收的合成夹具，headless Chromium 与 `AICAT_E2E_BACKGROUND=1` 验收配置验证、独立入口、启停和诊断，保存浅深色截图、日志和报告。不覆盖真实 WKWebView 指针事件或原生窗口显示。

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

该入口只用于用户已明确授权的真实 UI 与发送测试。它通过正式 MCP 发送一条带唯一标记的短文本，再用同一请求查询回执，核对本地自发消息与 Room 上下文只出现一次。操作会使用 Mini 的企业微信窗口和剪贴板；不在开发本机执行真实按键。工件包含私有请求、回执和不含正文的报告；脚本完成、后续数据库回流与收件端实际收到分别验收。

自动回复的真实验收使用只读观察入口，不主动调用发送：

```sh
python3 e2e/wecom-local-agent-live.py \
  --home /Users/fish/.aicat \
  --room 'R:conversation-id' \
  --output /absolute/path/to/new-private-artifacts
```

先启动观察，再从其他账号向已配置会话发一条新文本。观察入口以启动时读取游标为验证起点，关联新任务、MCP sent 回执和唯一自发回流，并确认自己的回复没有创建任务。`--after-position` 可明确指定已有验证的起点；`--duration` 设置观察时长。报告只包含状态和标识，不输出正文或密钥。模型失败或发送 unknown 会停止验收，不自动重跑。
