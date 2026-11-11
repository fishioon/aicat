# 本地消息收发

跟踪：[消息读取 #21](https://github.com/netcat-ai/aicat/issues/21)，[后续 UI 发送 #20](https://github.com/netcat-ai/aicat/issues/20)。

`wechat-ipad` 和 `wecom-local` 是随 aicat 编译的可选适配器，由同一运行进程内的独立 worker 轮询。
只在显式 `enabled: true` 时初始化、读取文件；不需要额外服务、CLI 或 runtime 二进制。
协议差异留在 `internal/channels/`，CLI 和 Claw 共用 `channels.ReadUpdates` 接收统一批次和不透明游标。

消息进入既有 Room Context Log，落盘同步后提交游标，再按既有 trigger 调用 Codex start/resume/steer。
`reply.enabled: false` 仅阻止发送中途回复、最终回复和附件，不阻止 Agent 执行任务或操作。
本地适配器回复默认关闭；显式配置 `reply.enabled: true` 后可通过 UI 发送文字和整组图片。已有 Webox / wecom-archive 配置默认行为保持不变，
也可以显式关闭回复。读取不操作 UI；发送使用当前 macOS GUI 会话，不提取密钥。
桌面设置可手动开启[虚拟工作屏](work-screen.md)搬移两个微信窗口；它仍与实体屏共享输入和焦点。

## 配置

仅编辑 `~/.aicat/aicat.yaml`，修改后重启 aicat。下面片段合并到现有 `claw`；不要覆盖已有配置。

```yaml
claw:
  agents:
    - id: ipad
      kind: wechat-ipad
      enabled: true
      reply:
        enabled: false
      local:
        dataDir: /Users/yourname/Library/Containers/com.tencent.xin/Data
        appDir: /Users/yourname/Library/Containers/io.playcover.PlayCover/Applications/com.tencent.xin.app
        accountID: wxid_your_account
      trigger:
        regex:
          enabled: true
          pattern: '^虾虾'
        chat:
          enabled: false
    - id: work-local
      kind: wecom-local
      enabled: true
      local:
        dataDir: /absolute/path/to/WeCom/account/database
        accountID: your_wecom_account
        keyFile: /absolute/path/to/keys.json
      trigger:
        regex:
          enabled: true
          pattern: '^虾虾'
```

所有路径必须为绝对路径，不展开 `~` / 环境变量。`local.accountID` 必填，用稳定账号身份，不用备注名。
本地适配器不需要 `url`、`tokenFile`；默认使用内嵌 AppleScript，`sendScript` 可用绝对路径显式覆盖；`sharedDir` 可省略，默认为该 Agent Directory。
`id` 是状态命名空间：换账号、换数据根目录时使用新的 `id`，不要复用旧游标。
`enabled` 为 false 或本地适配器未写 `enabled` 时，即使本地路径未配置也不会访问该来源。

### iPad 微信

适配 PlayCover 下现有 iPad 微信：`Documents/LocalInfo.data` 验证账号，
`Documents/<md5(accountID)>/DB/WCDB_Contact.sqlite`、`session/session.db` 解析会话身份，
`DB/message_*.sqlite` 读取消息。SQLite 用只读连接读取当前已提交 WAL，不复制主库后忽略 WAL。
消息库文件 inode 更换会重开；本地序号倒退会报错，禁止静默重置游标。

压缩消息按 WCDB 标记解码；字典来自 `appDir` 下五个安装资源 `*Dict.zstd`，在首次遇到压缩消息时才加载。
`appDir` 可省略以使用上述当前用户默认路径。这里使用应用已有的固定字典格式，不扫描进程或获取数据库密钥。
可选 `wechatID` 用于校验可见微信号；长期游标绑定 `accountID` 与 `dataDir`。
群成员从消息前缀识别，私聊可使用既有 `trigger.chat`。

### 企业微信

`dataDir` 直接指向含 `message.db` 和 `message.db-wal` 的账号目录。
只读取用户已有密钥文件，兼容 Mini 上 wecomdb 的 `keys.json` v1：

```json
{
  "version": 1,
  "entries": [{
    "path": "/absolute/path/to/WeCom/account/database/message.db",
    "account": "your_wecom_account",
    "name": "message.db",
    "key": "32位十六进制原始AES密钥",
    "missing": false
  }]
}
```

文件由用户自行准备，建议权限 `0600`；路径、账号和库名必须匹配，重复 key entry 被拒绝。
不在 YAML、日志、Issue 或测试报告中保存真实密钥。
`message.db`、`session.db`、`user.db` 各按需打开并复用一个持久只读连接，随 Source 关闭统一释放。
读取使用 `github.com/sjzar/go-sqlite3mc` 固定版本，直接导入已有 16 字节 raw key，
配置 AES-128-CBC 和 `mc_legacy_wal=0`，由 SQLite 按页解密、管理页缓存、WAL 校验和事务可见性。
每条查询结束即释放语句及读事务，下一次查询可看到客户端的新提交；不再全库解密、手工回放 WAL、
重建内存数据库，也不生成明文快照。普通 DB/WAL 更新及 checkpoint 不重开连接；
主库文件被替换或密钥文件变化时仅重开受影响库的连接，查询期间发生替换则丢弃结果、保留游标重试。
这项读取能力需要 macOS/Linux 的 cgo 构建；正式桌面包默认具备。
`make cli` 的纯 Go 版本保留代理和 iPad 能力，企业微信本地读取会明确报错。
需要带企业微信读取的无界面二进制时使用 `CGO_ENABLED=1 go build -tags nogui -o bin/aicat-cli ./cmd/aicat`。

沿用已验证的 `message_table` schema，类型目录已按企业微信 5.0.13 / 95049 客户端重新核验。
会话 ID、sender ID 保留稳定值，原始 body 和 extra 分别以 `raw_content`、`raw_extra_content` 保留。
分类、结构视图、邮件、附件和模型原始数据交付见 [企业微信消息协议](wecom-message-protocol.md)。
已识别的文字 frame 按片段顺序还原完整正文，保留空白、换行和 `@显示名`；
`mentions: [{"id":"账号ID","name":"显示名"}]` 单独保留真实提及，身份判断使用 ID，不使用昵称。
`@全体成员` 保存为 `{"type":"all","name":"全体成员"}`，不包含具体账号 ID。
未识别或不完整的 frame 保留结构、原始字节和明确解析状态，不再从最长可打印片段猜正文或提及；普通文字 `@名字` 不等于真实 @。
文字解析先验证完整 protobuf frame，并优先递归读取内层字段，避免把长度字节当作正文；保留正文首尾空白。
消息方向优先比较 `sender_id` 与配置的 `accountID`，仅在发送者缺失时回退到旧版 flag 标记。
`S:账号_对方ID` 是私聊（必须包含配置的账号），`R:会话ID` 是群聊；未识别格式保持 unknown。
`trigger.chat.enabled: true` 对已识别的私聊，以及群聊中真实 @ 当前账号的消息触发；
仍忽略自己发出的消息和超过 10 分钟的消息。只 @ 别人、@全体成员或未知会话不因 chat 触发。
私聊除正文和图片外，也接受链接（`link`）、视频号（`sphfeed`）及本地企微带 `link` 的 `card` 分享；
本地企微 `content_type=13` 的视频号预览分享按完整 protobuf 顶层字段解析为 `sphfeed`，
保留作者、描述，以及 `link` 中的原始预览 URL（含 `eid`）和封面 URL；原始 body 仍保存在 `raw_content`。
仅识别 `channels.weixin.qq.com/finder-preview/pages/feed` 且含唯一有效 `export/` eid 的链接；
其他或损坏的混合消息保持 `mixed`，不因卡片文案命中触发词而启动任务。
名片、表情、系统通知等不因此触发，群聊不增加分享卡片触发入口。
只分享且没有明确用途时，助手先询问文章总结/收录或视频下载并存微盘，再根据用户的自然语言选择执行。
此流程复用 Room 的持久 Codex 会话和聊天日志，不新增待处理状态或消息去重保证。
regex 独立匹配完整正文；关闭 chat 不关闭 regex，正文（含还原的 @ 显示名）命中已开启的正则时仍可触发。
独立读取、Room 日志及 Codex 的 `trigger_messages` 都保留相同的完整正文和 `mentions`，包括 active turn 的 steer。
本地密钥条目的 account 是用户声明，不代表从加密库额外认证出了账号身份。

企业微信引用从 `extra_content` 的 protobuf 引用字段解析，沿用 `msgtype: reply` 和
`reply.title + reply.parent`：title 仅为当前正文，parent 只保存直接父消息的 ID、发送者、类型和毫秒时间，
不增加 `quote`，不嵌入父消息正文。引用携带的 `client_id` 在同一 Room 内查询唯一的更早消息，
映射为原消息已有的 `wecom-local:<message_id>`；模型按此 ID 从 Room 日志查找原文。
当前正文的真实 @ 仍保留在 `mentions`，引用显示文字中的 @ 和触发词不参与触发。
缺失原消息、ID 重复或 protobuf 损坏时保留普通正文，不猜测引用目标。读取或查询失败不推进游标。


## 入站图片

`wechat-ipad` 与 `wecom-local` 会将当前账号已缓存且可解码的图片原子写入
`sharedDir/rooms/<roomid>/inbox/`（`sharedDir` 未配置时使用所属 Agent Directory），
在消息的 `image.sdkfileid` 中返回 `inbox/...` 相对路径。CLI 读取与 Claw 使用相同流程；
表情保持 `msgtype: emotion`，图片路径位于 `emotion.sdkfileid`。不请求客户端自动下载，不访问 CDN。

缓存定位由具体适配器负责；`channels.ReadUpdates` 在批次层统一完成短时重试和消息状态填充。
目录、路径校验、文件解码和原子落盘由 `internal/media` 提供，并与 Claw 的回复附件解析共用同一 `Store`。

成功消息带有 `media_status: ready`、`media_format`，以及 `media_quality`：
`original` 为高清缓存，`standard` 为普通缓存，`thumbnail` 为缩略图；图片 body 同时保留
实际落盘字节的 MD5、大小、宽高。Codex 按消息路径在 `resources.media` 下打开图片查看，
触发消息和 Room 日志均保留这些字段。图片是否开启任务仍由既有 chat/regex 和身份规则决定。

- iPad 微信按账号、会话表哈希和 `MesLocalID` 定位 `ImgV2/.../*.pic_hd`、
  `Img/.../*.pic`、`Img/.../*.pic_thum`，依次优先读取可解码的高清、普通图和缩略图。
  macOS cgo 构建通过系统 ImageIO 将 HEIC 等本机可解码格式转换为 PNG，不调用额外 runtime 二进制。
  WXGF 原图当前不支持；有可读缩略图时明确以 `thumbnail` 交付。
- 企业微信先核对 `Documents/Profiles/setting.json` 的 `CurrentProfile` 与该 Profile
  `io_data.json` 中 `user_info` 的账号 ID，仅索引相符账号的 `Images/Emotions` 缓存。
  当前版本按有效 protobuf 结构识别 `content_type=101` 图片和 `29` 表情；其它结构沿用原分类。
  缓存文件名不作为内容身份，实际字节 MD5 必须与消息引用匹配。

10 秒内的新消息在一个批次内最多短时重试 3 秒，给图片落盘或高清图替换缩略图留出时间；
历史消息的缺失缓存不等待。缓存缺失、损坏、账号不符或格式不支持时，消息仍正常消费，
标记 `media_status: unavailable` 并给出 `media_error`，不填图片路径，不阻塞后续消息。
此次交付后客户端才补齐的缓存，需要显式重读原游标；不会自动重开 Codex 任务。
媒体路径限制在所属账号和 Room 内，文件大小及解码像素数有界；重读相同内容使用稳定文件名。

## 独立文字收发

`messages` 命令不启动代理或 Codex，不提交 Claw 的消费游标，使用同一份 `~/.aicat/aicat.yaml`。
先配置本地 Agent 的 `enabled: true`；发送还需要 `reply.enabled: true`。

```sh
# 检查真实数据库读取和当前启动入口的辅助功能权限；不发送消息。
aicat messages doctor --agent ipad
# 每次最多读取 500 条；用返回的 cursor 继续读。--room 过滤本批，但游标仍跨越全批。
aicat messages read --agent ipad
aicat messages read --agent ipad --cursor '<上次返回的 cursor>' --room '<roomid>'
# 通过标准输入传入原样文字；也支持 --text。
printf '%s' '文字收发测试' | aicat messages send --agent ipad --room '<roomid>' --search-name '测试群' --stdin
```

企业微信把 `--agent` 换成对应 ID。`--room` 必须使用读取结果中的稳定 `roomid`，不使用昵称。

企业微信可以只传 `--search-name` 搜索发送，不要求已有本地会话；例如将已确认的企业邮箱作为搜索词：

```sh
printf '%s' '请在企业微信中完成授权：<授权链接>' | aicat messages send --agent work --search-name 'member@example.com' --stdin
```

不传 `--room` 时仅支持已启用回复的 `wecom-local` Agent，搜索词原样交给现有企业微信发送脚本。
仍返回 `submitted_ui`：只表示 UI 脚本执行完毕，不是服务端投递确认；失败或结果不确定时不要自动重试。

`python3 scripts/wecom-search-send-e2e.py --binary bin/aicat --output <新目录>` 用真实 CLI 和记录用 AppleScript 验证搜索发送，生成 `report.json` 与消息记录；不发送真实聊天。可加 `--sandbox-cli <codex绝对路径>` 验证实际 Room 权限。

两端 `messages read` 使用相同批次结构，消息体保持原样，资料集中放在顶层：

```json
{
  "messages": [{"msgid":"wecom-local:123","roomid":"S:1001_2002","from":"2002","msgtime":1792478707000,"msgtype":"text","text":{"content":"你好"},"outgoing":false}],
  "rooms": {"S:1001_2002":{"type":"private","name":"测试用户","remark":"测试备注"}},
  "users": {"2002":{"name":"测试用户","remark":"测试备注","userid":"sample-member","email":"member@example.invalid"}},
  "cursor": "<不透明游标>"
}
```

`rooms` 的键对应本批消息的 `roomid`，`users` 的键对应 `from`，在各 Agent 内唯一；不增加平台前缀或改写原始 ID。
先过滤 `--room`，再去重查询本批用到的资料。空批返回 `messages: []`、`rooms: {}`、`users: {}`；过滤后的游标仍跨越原始整批。
会话类型为 `private`、`group` 或 `unknown`；`name` 保留会话名称，`remark` 单独保存备注，不用备注覆盖名称。私聊会话名称为空时，使用会话中另一方（非配置的当前账号）的用户名称，即使本批只有本人发出的消息也一样。用户提供名称和备注，缺失联系人保留对应键与 `{}`，不丢消息，也不编造身份。
企业微信的 `users[from]` 额外提供可选 `corp_id`、`userid`、`email`：corp_id 来自同一次用户查询的数字列（以字符串输出），userid/email 从 `pb_content` 顶层字段 32、84 解析（当前 Mini 客户端已核验），用于后续身份关联；`from` 和字典键仍是本地数字 ID。仅补非空 UTF-8 字符串；字段缺失、NULL 或 protobuf 损坏时不补 PB 身份字段，保留 corp_id、姓名、备注及原有消息处理，不从姓名、备注、基础 `account/email` 列或文本碎片猜测身份。微信端结构不变。
这不是全量通讯录或群成员列表，不自动查询私云，不增加 `unionid` 或外部联系人 `wmg-...` ID；外部联系人缺少这些 PB 字段时只返回已有名称、备注。

企业微信每批分别对 `session.db`、`user.db` 做一次批量查询，外部联系人备注联结 `external_user_relation_v3`，优先 `real_remarks`，其次 `remarks`；
私聊备注来自对方资料，无会话名时使用对方名称。微信沿用已有 Friend 解析，昵称与备注分别对应 `name`、`remark`，增加一次针对本批 ID 的查询。
查询均复用来源已有的持久连接，不新增跨批资料缓存；下一批重新查询当前资料。缺记录返回空资料，库、密钥或查询错误则整批失败并保留旧游标。

Claw 在既有触发执行前更新 Room 的 `name`、`remark`、`type`；当前用户资料直接通过本批 users 传递，不再落盘为用户缓存或提供 resources.users。
iPad 微信不要求关联私云身份，按配置的 regex/chat 规则触发 AI；微信原始 `from` 不作为私云 `userid`，本地备注也不用于私云身份认领。企业微信本地渠道按已识别身份进入 AI：有效 ai.<userid> 备注优先补入小写 userid；其他资料保持原值，无此备注时保留原生 userid。用户和会话资料使用结构体，trigger 仅要求 userid 非空，不判断企业归属或消息类型。身份不明时，开启 chat/reply 的群聊真实 @ 每次都会给固定关联提示，其他情况静默。上下文沿用 users 和 rooms，消息保留原始 from、msgid、roomid。aicat 不预取票或缓存业务资料，Agent 按需通过 MCP 查询资料和私聊个人机器人授权。详见 [聊天身份识别](identity-binding.md)。
Room 的人工 `search_name` 和既有 `thread_id` 保留；会话类型进入同一套群聊正则、私聊及 @ 当前账号触发与回复流程，UI 脚本不变。

自动回复优先从 `~/.aicat/agent/<id>/rooms/<roomid>/room.json` 读取人工维护的 `search_name`，可用于覆盖自动解析：

```json
{"search_name":"测试群"}
```

保留文件已有的 `thread_id` 等字段。手动发送可用 `--search-name` 临时指定，不改写映射。
iPad 微信先用本地联系人检查搜索词确实匹配目标、且不匹配其他联系人；模糊或缺失映射会在操作 UI 前失败。
企业微信按 `--room` 发送时要求已知本地会话。两端都优先使用人工 `search_name`；未配置时查询当前 Room 资料，先取非空 `remark`，再取 `name`，不再优先使用 email 或 account。企业微信的私聊 `name` 为空时使用对方用户名称。只传 `--search-name` 时直接搜索，不读取 Room 的人工映射。
企业微信两份资料库沿用同一个已有密钥文件内各自匹配的 key entry，复用各自的持久只读连接；不扫描进程。
名称缺失或重名时须人工配置唯一可搜索的 `search_name`。CLI 和自动回复使用相同的解析与发送流程。

发送需要已登录且可交互的桌面，以及运行入口的“辅助功能”和控制 System Events 的“自动化”权限。
SSH 终端和通过 Finder / `open` 启动的 aicat.app 是不同授权入口，以 `messages doctor` 的实际结果为准。
运行期间不要操作键盘、切换窗口或修改剪贴板。aicat 的两个渠道和归档脚本共用跨进程 GUI 锁。

iPad 微信发送纯文字。企业微信还支持图片，每张须是可读取的本地普通文件，至多 100 MiB。文字须为非空 UTF-8，每条至多 5000 字节，一批最多 32 条消息。文件通过在线链接提供。

多图使用重复的 `--image` 参数：

```sh
aicat messages send --agent work --room '<roomid>' --image a.png --image b.jpg
```

企业微信先发送正文，再把整组图片一次写入剪贴板、一次粘贴，按回车提交。
文字、图片或文字加图片均在全部发送脚本正常结束后返回 `submitted_ui`（已提交），不返回 `msgid`，不进行发送后的数据库回读或等待；该状态不表示对方已收到或已读。
发送前仍检查目标会话、联系人及搜索名称，保留内容和图片校验、回复开关、GUI 锁与账号检查。脚本失败或超时会报错，不自动重试，避免重复发送。
送达情况在端到端验收中由另一客户端独立核对。
文字脚本参数为 iPad 微信 `bundleID searchName text` 和企业微信 `searchName text`。企业微信整组图片参数为 `searchName --images path...`。

## 读取边界与恢复

- 首次启用从已有历史开始，默认每秒一批、每批最多 500 条；先前已存在但不超过 10 分钟的匹配消息也可能执行。
  更早消息仅落盘，不触发。自己发的消息默认不触发，沿用 `regex.self` 开关。
- iPad 游标按库文件和 Chat 表的本地序号推进；企业微信按本地 `message_id` 推进，不依赖可能晚到的消息时间。
- iPad 的 Chat 表暂时无法从联系人库或会话列表解析 roomid 时，暂缓该表，不推进它的游标，也不占用批次额度；其他会话继续读取。映射恢复后自动补读，历史消息仍受十分钟触发时效限制。
- 读取/解码/落盘失败不推进游标；独立 worker 退避重试，日志记录 `agent_id`、连接成功或错误。
  关闭适配器后释放 SQLite 连接和字典。
- 重启从 `~/.aicat/agent/<id>/cursor` 续读。沿用 Claw 既有交付语义：落盘与游标提交不是同一个事务，
  崩溃可能重放日志；游标提交后、Codex handoff 前崩溃可能漏触发。此版本不承诺 exactly-once 执行。
- 图片与表情按上述流程交付；本地企微文件、语音、视频和混合/转发内已识别文件，按已确认的 MD5 匹配当前账号缓存并交付 inbox 路径。无可验证缓存时保留元数据和 unavailable 原因，不获取远程附件；不包含转写和视频理解。
- 企业微信单个主库和 WAL 各限 128 MiB，密钥文件限 1 MiB；查询按页读取，不再分配全库明文镜像。
  iPad 一批原始 body 合计限 32 MiB、单条限 8 MiB，zstd decoder 限 64 MiB。
  超出限制会明确报错，不继续推进；这些是文件/批次上限，不是进程 RSS 硬上限。
- 表重建、本地序号倒退、账号或数据目录改变时拒绝原游标。确认需要重新导入后使用新的 Agent ID；
  不自动删除已有 Room 或覆盖消费进度。

## 验证

```sh
make verify
CGO_ENABLED=1 go build -tags nogui -o bin/aicat-cli ./cmd/aicat
python3 scripts/local-messages-e2e.py --binary bin/aicat-cli
python3 scripts/wecom-connections-e2e.py --output build/wecom-connections-e2e
```

E2E 使用 Python 标准库、OpenSSL CLI、Go 生成的合成 WCDB 字典，启动真实 `aicat serve`，
Codex app-server 用确定性替身记录 RPC 并写实际动作工件。不调用真实模型、不向真实账号发送消息。macOS 下发送用例经真实 CLI / osascript 调用只记录操作、不写出站 DB 的合成脚本，验证已提交状态、目标检查和脚本错误；这不能替代客户端 UI 验收。
它在临时 HOME 下关闭 gateway，不修改系统代理或用户 Keychain；输出 `report.json`、
Room 日志、RPC、动作文件、加密库/WAL 和二进制 SHA-256，可用 `--output <新目录>` 保存证据。
`wecom-connections-e2e.py` 额外验证三份库独立密钥、持久句柄复用、元数据 WAL 更新、回滚、
checkpoint(TRUNCATE/RESTART)、主库替换、密钥轮换及关闭释放。初始加密库/WAL 由标准 SQLite 和
OpenSSL 生成；后续更新由独立 SQLite3MC 写进程提交，避免直接改写 WAL 绕过共享内存索引。
测试数据及密钥全部为合成；结果证明进程接线和数据格式行为，不替代真实账号/具体客户端版本验收。

### 真实互通群验收

在 macOS 上，先用两种客户端登录同一个微信/企业微信互通群，分别配置它们的 Room `search_name`。
运行下列命令会各发送一条带唯一编号的中文多行测试文字，经另一客户端的生产读取器核对原文：

```sh
python3 scripts/local-text-live-e2e.py \
  --app /absolute/path/to/aicat.app \
  --test-home /absolute/path/to/isolated-home \
  --output /absolute/path/to/new-evidence-directory \
  --ipad-room '<微信 roomid>' \
  --work-room '<企业微信 roomid>'
```

`test-home/.aicat/aicat.yaml` 必须已经配置 `ipad` 和 `work` 两个启用文字回复的本地 Agent，
可通过 `--ipad-agent` / `--work-agent` 更改 ID。使用隔离 HOME，不启动 Codex 或接管系统代理。
验收从应用入口启动命令，先检查实际辅助功能权限；发送只尝试一次，对端读取最多等待 30 秒。
`report.json` 保存二进制摘要、双方消息 ID 与原文摘要，原始日志和测试文字保留在同一私有目录。
它证明真实客户端的文字收发，不覆盖图片、附件或 AI 自动回复。

针对 iPad 未映射会话隔离、补读、重启、“虾来”触发及回复提交的进程 E2E：

```sh
python3 scripts/ipad-trigger-e2e.py --binary bin/aicat --output <新目录>
```

该验证只使用合成 SQLite/WAL、隔离 HOME、Codex/UI 记录替身，工件包含 `report.json`、二进制 SHA-256、重跑命令、消息日志、RPC 和 UI 回执。
