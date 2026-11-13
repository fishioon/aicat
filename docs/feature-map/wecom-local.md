# 企业微信客户端

内置的只读客户端扩展，ID 为 `wecom-local`，默认停用，与可安装的[企业微信 BOT](wecom.md)分别配置和启停。应用内 Bun 执行 TS 读取器，从客户端本地 `message.db` 和 WAL 增量读取消息，经 stdio CloudEvent 写入现有 Room 服务。原型不发送消息、不自动执行 Agent、不扫描进程内存或提取密钥。

## 使用

扩展 → 已安装 → 企业微信客户端 → 配置，填写本地账号 ID、含 `message.db` 的数据库目录和已有 `keys.json` 的绝对路径。保存时验证密钥、数据库完整性与消息表结构；验证不导入消息。启用后开始导入本地历史，之后每秒检查增量。诊断页显示本次运行最近 50 条消息，区分收到、发出及媒体元数据，正文默认简短预览，可展开阅读原文，并提供打开 Room 日志入口。停用回收专用 Bun 宿主，保留配置、游标和 Room。

也可以在停用时编辑唯一行为配置 `aicat.yaml`，重启后生效：

```yaml
extensions:
  wecom-local:
    enabled: true
    local:
      accountId: your_account_id
      dataDir: /absolute/path/to/account/Data
      keyFile: /absolute/path/to/keys.json
```

密钥文件使用 Mini `wecomdb` 的 v1 格式，路径、账号、库名必须匹配，不接受重复条目：

```json
{
  "version": 1,
  "entries": [{
    "path": "/absolute/path/to/account/Data/message.db",
    "account": "your_account_id",
    "name": "message.db",
    "key": "00112233445566778899aabbccddeeff"
  }]
}
```

示例密钥仅用于说明格式。真实密钥继续留在用户已有文件中，不复制进 YAML、Room 或进程参数。扩展不负责密钥获取。

## 数据与恢复

- 读取器两次抓取主库、WAL 和密钥文件，并比较文件身份、时间及内容；变化期间保留游标重试。wxSQLite3 AES-128-CBC 解密、WAL 重放与 SQLite 完整性校验均在内存完成，不生成明文数据库。
- WAL 校验覆盖密文、salt 与帧 checksum，只重放最后一次有效提交之前的帧。未提交或撕裂尾部不作为新消息。
- 游标按账号和数据目录隔离，使用本地 `message_id`，与消息时间无关；首次从 0 导入已有历史。Room 日志和纯消息上下文同步落盘后才确认，读取器收到确认后原子保存游标。
- 事件 ID 由来源和本地消息 ID 确定。Room 以持久事件和消息 ID 校验重放，补齐缺失上下文，避免确认丢失或崩溃恢复造成重复追加。同一标识内容变化时拒绝接收。
- 重启校验最后消费消息的指纹；最大 ID 倒退、锚点丢失或被替换时停止推进，不自动清除进度。更换账号或数据目录使用独立来源与游标；BOT 的配置、宿主和 Room 命名空间不共用。
- `state/wecom-local/` 保存专用宿主锁和 `cursor-<identity>.json`；`rooms/*/messages.jsonl` 保存事件，`rooms/*/context/messages.log` 保存消息，文件权限为 0600。诊断只在内存保留最近 50 条正文，去重索引只保存摘要。

## 范围

TS 页算法来自仓库历史提交 `1fad429` 的 `internal/messaging/wecomdb`，对照 Mini 的独立 `wecomdb` 工程。页算法源自 SQLite3MultipleCiphers 的 MIT 实现，消息类型映射参考 MIT 项目 tzwkb/wecom-agent。

当前适配 `message_table` 的既有 schema，Mini 的 macOS 企业微信 5.0.13 已验证真实只读导入和重启恢复。它读取客户端已落库的消息，不受机器人 @ 提及条件限制；不保证客户端未同步或已清理的历史存在。

正文保留 `raw_content` Base64，文本沿用 protobuf 叶子与 XML 启发式解析，不是完整官方协议解码。`local_id`、`server_id`、`sequence`、发送者 ID 用字符串保留 64 位精度。保留发送方向、原始类型、稳定会话 ID；群聊与私聊均记录为 `room_type: unknown`，不推断成员关系。图片、语音、文件等仅保留原始载荷和元数据，不读取媒体正文。

主库、WAL 和合并内存镜像各限 128 MiB，密钥文件限 1 MiB，单条原始正文限 8 MiB，每批至多 500 条及 32 MiB 原始正文。读取失败、结构不兼容或超限不推进游标。当前不提供发送、Agent 触发、联系人名称补全、自动找库或自动获取密钥。

## 验证

仅运行受影响流程，全部使用后台方式：

```sh
make cli channel-check
python3 e2e/wecom-local.py
make e2e-build
node e2e/wecom-local-ui.cjs bin/aicat-e2e build/wecom-local-e2e-<run>
```

`wecom-local.py` 使用独立 Python/OpenSSL 合成加密主库和 WAL，启动真实 CLI、Bun 读取器及 Room 服务。覆盖停用不读取、WAL 增量、64 位 ID、旧时间消息、自发消息、媒体元数据、Room 写入失败、确认重放、崩溃恢复、错误/重复密钥、损坏 WAL、撕裂尾部和数据库锚点变化。工件包含报告、应用日志、合成数据库、游标和 Room，可通过 `--output <新目录>` 固定位置。没有单元测试或真实消息发送。

配置草稿仅在已保存的本地配置发生变化时同步；主题、语言或运行状态刷新不会覆盖未保存的账号、目录和密钥路径。

独立界面验收使用固定夹具，包含普通收发、媒体元数据和多行长文本，不依赖读取恢复场景结束时的数据库状态：

```sh
python3 e2e/wecom-local-fixture.py --output build/wecom-local-ui-fixture-<run>
node e2e/wecom-local-ui.cjs bin/aicat-e2e build/wecom-local-ui-fixture-<run> build/wecom-local-ui-<run> review
```

`review` 场景核对无关刷新保留草稿、收发方向、媒体说明、预览展开后的完整原文和英文深色界面。

`wecom-local-ui.cjs` 使用合成夹具，headless Chromium 与 `AICAT_E2E_BACKGROUND=1` 验收配置验证、独立入口、启停和诊断，保存浅深色截图、日志和报告。不覆盖真实 WKWebView 指针事件或原生窗口显示。

真实本地库验收在数据库所在节点运行，输出目录必须全新且私有：

```sh
python3 e2e/wecom-local-live.py \
  --binary /absolute/path/to/bin/aicat-cli \
  --account your_account_id \
  --data-dir /absolute/path/to/account/Data \
  --key-file /absolute/path/to/keys.json \
  --output /absolute/path/to/new-private-artifacts
```

真实 Room 正文仅留在该节点的隔离工件目录。报告只输出条数和检查结果，不打印正文或密钥；只验证读取、落盘和重启，若期间无新消息，不把它称为真实新消息增量验收。
