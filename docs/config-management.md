# aicat 配置管理约定

## 配置职责

只有一份持久配置：

```text
~/.aicat/aicat.yaml
```

它由用户和自动化 agent 编辑，是 aicat 行为的唯一持久 source of truth：Capability Switch（内置网关的
开关与监听地址）、Claw 的 Webox/Codex 参数、File Server 配置都在这里。不再有第二份 runtime 配置，
也没有外部 runtime 二进制需要安装或校验。

## Bootstrap

默认 YAML 通过 Go `embed` 编入 `aicat` binary。`aicat serve` 仅在文件不存在时以 `0600`
创建：

```text
missing  -> copy embedded default
existing -> preserve exactly as written
```

默认 CA 路径相对 `~/.aicat/`，所以不依赖用户从哪个目录执行 `aicat serve`。

`aicat.yaml` 通过 `version` 管理格式版本，并容忍当前版本不认识的字段，方便较新配置被较旧 binary 读取；
已知字段的类型和值仍会正常校验。当前格式为：

```yaml
version: 1
gateway:
  enabled: true # 默认开启；false 表示 serve 不启动网关
  addr: 127.0.0.1:9981 # 必须是带端口的明确非 wildcard IP
claw:
  agents:
    - id: personal
      project:
        name: aicat-personal # 默认 aicat-<agent.id>
        path: /Users/you/.aicat/agent/personal/rooms # 默认该 agent 的 rooms/，覆盖时须为绝对路径
      model: "" # 未配置或为空时使用 claw.codex.model
      wechatID: jlyfish
      url: http://127.0.0.1:38080
      tokenFile: /absolute/path/to/webox/state/weagent/api-token
      sharedDir: /absolute/path/to/webox/state/shared
      # wecom-archive agent 必填
      sendScript: /absolute/path/to/aicat/scripts/wecom-send.applescript
      prompt: ""
      trigger:
        regex:
          enabled: true
          pattern: '^\s*虾虾'
        chat:
          enabled: false
  codex:
    binary: codex
    clientName: aicat_claw
    model: ""
    reasoningEffort: medium
    deepReasoningEffort: xhigh
    deepReasoningRegex: '深度思考 '
fileServer:
  host: ""
  dir: ""
  url: ""
```

`clientName` 对应 Codex App Server 的 `clientInfo.name`，默认是 `aicat_claw`。该值可能影响服务端
路由及合规日志中的集成身份，只应在诊断客户端身份差异时临时覆盖。
`reasoningEffort` 是普通新 turn 的推理强度，省略时默认为 `medium`。同时配置 `deepReasoningRegex`
和 `deepReasoningEffort` 才会启用深度推理覆盖；一批 `trigger_messages` 中任一消息正文匹配正则时，
新 turn 改用深度强度。示例正则的末尾含半角空格。active turn 的后续消息通过 `turn/steer` 交付，
不能改变该 turn 已经确定的推理强度。两个 effort 支持的具体值均由所选 Codex 模型决定；配置不完整
或正则非法会使 aicat 在启动时拒绝整份配置。

`wecom-archive` agent 回复开启时，`sendScript` 必须是绝对路径。aicat 直接使用 `/usr/bin/osascript` 执行该脚本，
按参数传入目标名称及文字/图片，不经过 shell；所有企业微信脚本执行串行，避免多个 Room 同时操作 GUI。

`agents` 没有启用项时 Claw 关闭，有启用项时 `aicat serve` 自动启动 Claw。以下连接字段针对旧的 Webox / wecom-archive；新增 `wechat-ipad` / `wecom-local` 使用独立的 `local` 字段，配置与边界见 [本地消息读取](local-messages.md)。旧类型的每个元素的 `id`、`wechatID`、`url`、
`tokenFile` 和 `sharedDir` 均须显式配置；agent `id` 在列表中唯一并匹配
`^[a-z][a-z0-9_-]{0,63}$`。`tokenFile` 保存宿主机文件路径，不保存
Webox token；`sharedDir` 是 Webox `/webox/state/shared` 在宿主机上的共享根目录，实际媒体按
`rooms/<roomid>/inbox|outbox` 隔离。两个路径必须是绝对路径，
不展开 `~`、`$HOME` 或其他环境变量，也不从彼此推导。不同实例可以配置同一个 `sharedDir`。可选的
`prompt` 属于 agent，只影响该 agent 下的 Codex thread。

每个启用的 agent 对应一个 Codex project。`project.name` 和 `project.path` 可分别省略，默认分别为
`aicat-<agent.id>` 与 `~/.aicat/agent/<agent.id>/rooms/`（由运行体使用真实用户主目录计算）。
启动时完成配置校验和目录准备后，aicat 启动共享 Codex app-server，为各 agent 幂等创建或复用 project；
名称和路径仅在首次创建时生效，之后只复用返回的 ID，不查询、比对或修改已有 project。不因 project 初始化创建 thread 或启动任务。
Project 路径不替代 Room 的工作目录，也不追加到 `runtimeWorkspaceRoots`；每个 Room 保留自己的 cwd、
`aicat-room` permission profile、媒体目录和已有额外可写目录。

历史 Room thread 用一次性脚本补关联（对每个 agent 执行一次；覆盖配置时同时传 `--name` / `--path`）：

```sh
python3 scripts/room-project-migrate.py --agent personal --codex /absolute/path/to/codex --output <新目录>
```

脚本只扫描该 agent 的 `rooms/*/room.json`，调用 Codex 的 metadata API 更新 project 归属；
不恢复 thread、不执行任务。`report.json` 保存原 project ID，通过 Codex 分页接口核对历史内容摘要，
并校验 thread ID、cwd、模型设置及 Room 配置未改变，不依赖 Codex 内部的历史文件布局。

所有 Room 都使用各自 Room 目录作为 Codex workspace；
命中该数组的 Room 将同一 agent 的整个 `rooms/` 加入 runtime workspace roots。它保留原有 trigger、持久 thread 和
回复目标，但可读写同一 agent 的全部 Room Directory，并保留自己的媒体与 File Server 权限。权限不跨 agent，配置修改需重启
`aicat serve` 生效。

每个 agent 的 trigger 独立生效，trigger 内的 `enabled` 未配置等价于 `false`。`regex` 与 `chat` 是两个独立开关，任一
命中即触发 Codex。两种触发都处理 `msgtype=text` 的正文和 `msgtype=reply` 的 `reply.title`；链接、文件、图片、视频号和 mixed 消息只写日志。
缺失 `from` 的消息在写入前直接丢弃。正则触发默认忽略当前微信账号自己发出的消息，配置
`regex.self: true` 后自己的消息同样可以触发；聊天触发只接受对方的私聊文本，不受 `self` 影响。只有
`msgtime` 距接收时刻不超过 10 分钟的消息才参与 trigger 判断；时间缺失或更早的同步消息仍写入 Room Context Log 并推进 Room Sequence，但不会启动或 steer Codex。两个开关同时关闭时所有消息只写 Room Context Log。非法或空的已启用正则会使
aicat 在启动时拒绝整份配置。

trigger 消息只有触发词而没有实际内容时仍会唤醒 Codex；此时内置 developer instructions 让 Codex 检查 Room
Context Log 最近消息中是否有用户漏加触发词的待处理请求，有则直接处理，没有则只简短回复。

当前版本不解析或猜测群聊 @；配置修改后需要重启 `aicat serve` 生效。旧的
`AICAT_WEBOX_URL`、`AICAT_WEBOX_MEDIA_DIR` 和 `AICAT_CODEX_BIN` 覆盖已删除；Codex binary 仅由
`claw.codex.binary` 配置，留空默认 `codex`。

`fileServer` 三个字段均为空时关闭，配置任意字段时必须完整配置。`host` 必须是带端口的明确非 wildcard
IP，通常绑定 loopback 或 Tailnet IP；`dir` 必须是绝对路径。`url` 必须是以 `/` 结尾、无凭据、query 或
fragment 的绝对 HTTPS 目录 URL，它的路径前缀映射到 `dir`，并作为 Claw Codex 回复链接的基址：网页结果和超过约十
行的长结果默认写入该目录，回复只给摘要和链接。aicat 会
将该目录作为额外 writable root 传给所有 Claw turn，因此其中内容应视为可能被公网读取。aicat 不负责
DNS、公网 TLS 或外部反向代理。服务直接使用 Go 标准 `http.FileServer`；目录没有 `index.html` 时会生成
目录列表，并遵循 `http.Dir` 的符号链接行为。

Claw 启动后，每个 agent worker 独立调用受 token 保护的 `GET /ilink/bot/userinfo`，获取真实 `account_id`
并校验返回的 `wechat_id` 是否等于配置的 `wechatID`。接口失败、返回值无效或微信号不一致时，该 agent
记录错误并按退避策略 sleep 后重试，不阻塞 aicat 或其他 agent。校验成功后 `account_id`
只保存在当前进程内，worker 进入 polling 且不再调用 `userinfo`。worker 从各 Room
`context/messages.log` 末条记录恢复 `seq`，并将 `roomid -> seq` 传给 `getupdates`。响应中的消息必须与
外层 Room 一致且 `seq` 严格递增；校验失败时记录日志并使用原 seq map 重试。消息追加并同步成功后才推进该
Room 的内存 `seq`，不保存独立 checkpoint。`tolist` 由 Webox 固定为空，aicat 不用它路由或校验会话。

## Serve Session

`aicat serve` 先准备 `~/.aicat/`、默认配置和 CA identity，再按配置启动：

- **内置网关**（`gateway.enabled`，默认开）：本体是 go-mitmproxy 的装配，只监听配置里的地址（默认
  `127.0.0.1:9981`），承担代理入口与 origin 入口；业务一律是 addon；
- **File Server**（`fileServer` 三个字段完整配置时）；
- **Claw**（`claw.agents` 非空时）。

三者共享同一次 Serve Session 的生命周期：任一异常退出、或 aicat 收到终止信号，都会结束本次 Serve
Session。三项都没启用时，`aicat serve` 立即返回，不留后台进程。Claw 的约定是：

- Claw 从 `~/.aicat/agent/` 启动共享 Codex app-server child，每个 Room 的 Codex thread cwd 则固定为
  对应 Room Directory；
- `claw.agents[].model` 优先于全局 `claw.codex.model`；未配置或为空时使用全局值，两者均为空时使用 Codex 默认模型。有效模型用于 thread 新建、恢复及每个 turn；
- 每个 agent 的自定义 `prompt` 追加在内置安全指令之后，并用于该 agent Codex thread 的
  `thread/start` 和 `thread/resume`；不同 agent 可使用不同 prompt。

## CA

CA 位于：

```text
~/.aicat/certificates/current/aicat-ca.pem
~/.aicat/certificates/current/aicat-ca-key.pem
```

两者都不存在时，aicat 使用 Go 标准库生成一套 ECDSA P-256、自签、825 天有效的 CA identity，并原子
安装 cert/key。内置网关用同一份 CA 为被 MITM 的 host 签叶子证书。把该 CA 作为受信任根证书写进
macOS 钥匙串由 aicat 负责（[0017](adr/0017-builtin-gateway-for-codex-wiring.md)），自动安装尚未实现，
当前仍由用户显式执行 `security add-trusted-cert`；手机等其它设备同样由用户自行安装并信任。

两者都存在时验证 CA 属性、有效期和 public/private key 匹配并复用。只存在一个文件、pair 无效或证书
失效时 fail closed。

## 内置网关的默认数据面

默认配置固定：

- 监听 `127.0.0.1:9981`，不接受 wildcard 地址；
- 拦截白名单只有 Codex 自己的 backend host，其它 host 直接隧道转发、不解密；
- WebSocket 升级回 `426`，让 Codex 回落到 HTTP/SSE；
- origin 形态的转发直连上游，不继承环境里的 HTTP 代理（否则会指回 aicat 自己）；
- 请求或响应体超过 32MB 时转为流式，addon 不再看到完整 body。

网关没有认证：它只监听用户显式配置的地址，对外开放（Tailnet 等）时必须由外层负责准入。

## 本地状态和权限

```text
~/.aicat/                              0700
├── aicat.yaml                         0600
├── certificates/                      0700
│   └── current/                       0700
│       ├── aicat-ca.pem               0644
│       └── aicat-ca-key.pem           0600
├── data/                              0700
└── agent/                             0700
    └── <id>/                          0700
		└── rooms/<roomid>/            0700, 目录名为原始 Webox roomid
            ├── room.json              0600, thread_id；企业微信可选人工填写 search_name（私聊缺省回退对方 search_name）
			└── context/messages.log   0600, 每条消息保留 seq
```

aicat 拒绝以 symlink 代替 owned directories、配置、certificate 或 private key。V1 不创建 PID file、
Serve Session Lock、persistent text log 或 system proxy snapshot。
