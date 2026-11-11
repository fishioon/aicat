# 企业微信消息分类与原始数据

本地企业微信读取采用客户端内置枚举，当前目录来自企业微信 macOS
5.0.13 / 95049 / arm64；`content_type_version` 表示这份枚举目录的核验版本，
不表示运行时探测到了该客户端版本。`content_type_name` 保留原始名称；
统一的 `msgtype` 用于选择文字、图片、表情、语音、视频、文件、链接、
小程序、名片、位置、邮件、混合、转发、卡片、系统通知和事件处理。

## 解析与交付

- 文字和真实 @ 沿用完整 frame 校验，引用仍按同会话的父消息 ID 解析。
- 图片/表情保留已有缓存验证，并识别文件容器中的图片引用。
- 文件容器提取已确认的文件名、MD5、大小；远程引用不充当本地文件路径。
- `FileMessage.1003` 按 `MailCardExtension` 提取标题、来源、摘要、查看链接、
  标识和附件数量；其封面、收件人等字段同时保留在结构视图。
  邮件卡片元数据不代表邮件全文，邮件来源也不能由文件类型 `8` 单独判断。
- 视频号预览继续校验原始 URL 和 eid。普通链接、小程序、业务卡片与通知保留
  完整结构；XML 的实体、CDATA、属性和混合文本顺序由 XML 解析器处理。
- 混合和转发消息中的完整文件容器，按字段位置列入 `attachments`；
  每项保留自己的可用状态，不把部分可读表示为全部可读。

`body` 和 `extra` 是有界结构视图。protobuf 按字段顺序保留 number、wire、
重复字段、精确整数、fixed32/fixed64 字节、嵌套结构和可读 UTF-8；
varint 以十进制字符串表示，避免 64 位整数被 JavaScript 舍入。
未知嵌套结构标为 `protobuf-candidate`，不推断其业务语义。
不可读的叶子保留大小和 SHA-256；二进制和长编码引用不会被评分成正文。
XML、JSON 和纯文字分别提供相应的结构视图。

`parse_status=decoded` 表示识别了当前主体或已验证容器；
`partial` 表示只有结构或仍有未确认语义，`unavailable` 表示结构无法读取。
`decoded` 不声称所有未知扩展都具有已知字段名。
结构视图至多 4096 个节点、12 层递归和 64 KiB 可读文本；达到上限显式标记
`truncated`，不是从来源丢弃消息，也不会阻断消费游标。

原始 `content`、`extra_content` 总是分别以 Base64 保存在
`raw_content`、`raw_extra_content`，不经可打印字符过滤。
CLI、Room 日志与触发输入保留同一份扩展字段；Claw 不再把消息转回旧版
Webox 结构后丢弃未知字段。单个原始字段超过 256 KiB 时，模型输入改带
`raw_content_ref` 或 `raw_extra_content_ref`，指向
`context/messages.log` 中精确的 msgid 和字段；日志保留完整字节。
同一规则适用于正在执行任务时的 steer 和重启后的消息。

## 缓存附件

图片沿用解码、质量选择和账号绑定。文件、语音、视频以消息中已确认的 MD5
匹配当前账号缓存：不按展示文件名猜路径，不访问 CDN，不自动下载。
候选文件再次验证实际字节，原子写入当前 Room 的 `inbox/`，成功才填写
`file/voice/video.sdkfileid`。缺失校验值、缓存未下载、账号不符、文件变化、
符号链接越界或读取失败时，保留元数据，标记 `media_status=unavailable`
并说明原因；不会把远程引用作为已交付附件。

缓存索引按当前账号限制扫描深度、条目数量和哈希预算，单个候选至多 64 MiB；
读取和落盘复用 `internal/media`。同一附件的重读路径稳定，不同 Room 独立落盘。
本次提供附件读取，不引入语音转写、视频理解或文件内容分析服务。

## 枚举证据与边界

`catalog_generated.go` 的 589 个名称和编号来自原客户端初始化器，
名称地址、名称长度、编号共 1767 个值已与 REA 局部反编译交叉核对。
原程序 SHA-256：
`079eabdc95281c62b4daf7f431124dff53caf5d11ccbe1f8715b2b2f56a4daf2`。
调查与复现工件保存在 `build/wecom-protocol-20261103/`。
这里的完整性仅指所定位的枚举表；不声称所有客户端变体和未来版本均已恢复。
未确认业务语义的类型使用原名称和结构视图，不伪造金额、坐标、身份等字段。

## 统一端到端验收

```sh
make verify
python3 scripts/wecom-protocol-e2e.py --binary bin/aicat --output build/wecom-protocol-e2e-<唯一目录>
python3 scripts/inbound-images-e2e.py --binary bin/aicat --output build/inbound-images-regression-<唯一目录>
```

协议验收使用真实 CLI、真实 `aicat serve`、合成加密 SQLite/WAL、隔离 HOME 和
记录式 Codex app-server，覆盖此前实际出现的 35 种内部编号及新增分类、
邮件、原始字段、64 位身份、未知类型、坏帧、结构上限、附件、steer 和重启。
消费端实际打开附件和大型原始字段的日志引用，记录内容摘要。
工件包含 `report.json`、二进制 SHA-256、重跑命令、CLI 批次、Room 日志、
RPC、消费端观察和合成数据库。不使用真实模型、不发送真实消息，
不能将该验证解释为语音转写、视频理解或真实客户端发送验收。

## mini 安装验证（2026-11-03）

已更新 `/Applications/aicat.app`，沿用 mini 原有 `aicat Local Code Signing`
证书，覆盖前后 designated requirement 完全一致。配置和可观察的 TCC 授权记录未变，
微信与企业微信从实际应用入口执行 doctor，读取与辅助功能检查均通过。

生产历史邮件 `wecom-local:4257` 复读为 `mail`，恢复标题、来源和查看链接，
原始 body SHA-256 与调查记录一致。复验同时补齐了邮件显示文件名含
`owner/repository` 的回归：该字段只作显示，不用于寻找缓存或生成实际路径。
修正后统一 E2E 105 项通过；部署验证未发送真实聊天消息，也未重置生产游标。

最终本地未签名二进制 SHA-256：
`cc653d1c72da049db2486e5e733c39ebe0aef72ca68e8fa90376db4fa4fde010`。
mini 已签名二进制 SHA-256：
`0979c1e06e9f4f9e02e428f1d4af135a35580f87a0b0789227f8c4d548443198`。
本地验证记录为 `build/wecom-protocol-deploy-20261103/verification.json`；
mini 私有部署目录为 `~/.aicat/deployments/wecom-protocol-20261103-01/`，
其中 `aicat.previous.app` 为原签名回滚包。临时签名任务和安装副本已清理。
