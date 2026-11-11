# 企业微信会话存档通过 Webox 实时投影到 Claw

状态：superseded by ADR-0012

aicat 需要消费企业微信会话内容存档，但 Finance SDK 依赖 Linux、CGO 和官方动态库，不适合链接进 macOS CLI。企业微信存档因此集成到 Webox 的 Linux `weagent` 中；Webox 实时拉取并规范化消息，对外继续提供现有 iLink HTTP interface，aicat 不感知底层消息来自微信本地数据库还是企业微信会话存档。

## Decision

### 部署与职责

- Finance SDK、企业 ID、会话存档 Secret 和 RSA 私钥只存在于 Webox Agent 的 Linux 运行环境。
- aicat 仍是消息消费端，继续管理各 Room 的消费进度、Room Context Log、trigger、Codex thread 和回复编排。
- Webox Agent 不为企业微信存档建立本地消息数据库，不把 Finance 全局消息流物化为持久 Room 数据源，也不保存独立的消费 cursor。
- `GetChatData` 在每次 `getupdates` 请求期间实时调用；解密后的结果只在内存中转换、过滤和按 Room 分组。
- 下载完成的媒体文件继续写入 Webox Shared Directory。媒体文件属于现有共享媒体交换机制，不视为本地消息源。

### HTTP interface 保持一致

企业微信存档来源继续复用现有路径、鉴权方式和请求/响应信封：

```text
GET  /ilink/bot/userinfo
POST /ilink/bot/getupdates
POST /ilink/bot/sendmessage
```

`getupdates` 请求仍由调用方提交多个 Room cursor：

```json
{
  "rooms": {
    "room-a": {"seq": 120},
    "room-b": {"seq": 95}
  }
}
```

响应仍按 Room 返回现有 `github.com/netcat-ai/webox/wecom.Message`：

```json
{
  "ret": 0,
  "rooms": {
    "room-a": {"messages": []},
    "room-b": {"messages": []}
  },
  "longpolling_timeout_ms": 35000
}
```

不增加 Finance 专用 HTTP 路径、全局 cursor 字段、确认接口或第二套消息模型。Webox 内部可以调整消息来源 seam，使微信数据库 Adapter 与企业微信存档 Adapter 分别实现最自然的拉取方式，但该差异不得泄漏到 HTTP interface 或 aicat。

### 实时拉取与 seq

企业微信存档 Adapter 对一次 `getupdates` 请求执行以下流程：

1. 请求没有 Room cursor 时，使用配置的初始 `seq`；未配置时使用 `0`。
2. 请求包含 Room cursor 时，取其中最大的 `seq` 作为 `GetChatData` 的起点。
3. 调用一次 Finance SDK 实时拉取消息，单次返回量使用现有 `getupdates` 上限；下一次请求自然从 aicat 新提交的最大 Room `seq` 继续。
4. 解密每条消息，推导 Room，转换成现有 `wecom.Message`，再按 Room 分组。
5. SDK 的全局 `seq` 直接写入消息公开 `seq`。它在单个 Room 内仍严格递增，允许存在空洞。
6. 对请求中已有的 Room，只返回消息 `seq` 大于该 Room cursor 的消息；新发现的 Room 接受本次查询起点之后的消息。
7. aicat 按现有规则在 Room Context Log 同步成功后推进该 Room 的内存 `seq`，重启时仍从日志末条消息恢复。

选择最大 `seq` 是明确接受的 best-effort 取舍。如果不同 Room 的 cursor 已经分叉，或者某个 Room 在其他 Room 成功后落盘失败，后续请求可能跳过落后 Room 的历史消息。第一版接受这种丢失，不增加重放、补偿、事务确认、全局 watermark、本地索引或 cursor divergence 日志。

只记录实际发生的错误，包括 SDK 调用失败、消息解密失败、Room 推导失败、消息转换失败和媒体下载失败；不为 Room cursor 差异增加 WARN 或额外统计字段。

### Room 映射与身份

- `userinfo.account_id` 表示该企业微信存档 Agent 配置的企业成员 userid；`userinfo.wechat_id` 使用该 Agent 对外声明并由 aicat 校验的稳定配置值。
- 群聊消息保留 Finance 明文中的非空 `roomid`，不再添加 `@chatroom`；aicat 根据 `wrg-` 前缀识别企业微信群聊，仍兼容微信原生的 `@chatroom` 后缀。
- 单聊消息的 Finance `roomid` 为空，由已配置的 `account_id`、`from` 和 `tolist` 推导对端，并以稳定、安全的对端标识作为公开 `roomid`。
- 当前成员是发送方时，从 `tolist` 中选择排除当前成员后的唯一对端；当前成员不是发送方时使用 `from`。
- 无法得到唯一对端、消息不涉及当前成员、Room ID 不安全或必要身份字段缺失时，记录实际错误并跳过该消息。
- 外层 Room key 必须与每条消息的 `roomid` 一致；同 Room 消息必须按 `seq` 严格递增，继续由 aicat 执行现有校验。

### 消息格式

Webox 继续使用企业微信会话存档形状的公开消息信封：`msgid`、`action`、`from`、`tolist`、`roomid`、`msgtime`、`msgtype`、对应消息体以及 `seq`。

Finance Adapter 负责消化来源差异：

- 保留企业微信的真实 `from`、`tolist`、`action` 和 `msgtime`。
- 保留 Finance 群聊的原始 `roomid`，将单聊的空 `roomid` 转换为 Webox 所要求的非空稳定 Room ID。旧版本生成的 `wrg-...@chatroom` 会话与媒体目录在停服后一次性改名，保留已有文件和 thread 绑定，不在运行时维护迁移分支。
- 将 SDK 原始媒体 ID 转换为 Webox Shared Directory 中的相对路径，不向 aicat 暴露不可直接访问的 Finance `sdkfileid`。
- 将支持的 Finance 消息类型映射到现有 `wecom.Message`；暂不支持的类型记录实际转换错误并跳过，不引入临时私有字段。

消息进入 aicat 后继续完全复用现有 Room Context Log、文本提取、trigger、深度推理选择、Codex start/resume/steer 和 Room 回复目标逻辑。

### 媒体

图片、文件及后续支持的其他媒体沿用现有 Shared Directory contract：

```text
<sharedDir>/rooms/<roomid>/inbox/<filename>
```

Finance Adapter 使用 `GetMediaData` 分片下载原始 `sdkfileid`，完成后校验可用的 MD5 与大小，并通过现有 Shared Directory 写入对应 Room 的 `inbox/`。返回给 aicat 的 `sdkfileid` 是相对于 Room 目录的 `inbox/...` 路径；同一消息使用稳定目标路径。

媒体下载失败时直接返回错误，不等待、降级或补偿；不得把 Finance 原始媒体 ID 当作本地路径返回。路径清理、目录逃逸防护和普通文件检查继续由现有 Shared Directory 规则负责。

该方案要求 Webox Agent 与 aicat 看到同一个宿主机 Shared Directory。Webox 在本机 Linux 容器运行时使用 bind mount；若未来改为远端 Linux 主机且无法共享同一目录，必须另行设计媒体传输 seam，本 ADR 不以远端服务器本地路径冒充 macOS 本地路径。

### 发送

Finance SDK 只提供会话存档读取和媒体获取，不提供企业微信消息发送。接收侧可以完全复用 `getupdates`，但要让 Claw 的 Codex 输出回到原企业微信会话，仍需一个满足现有 `messageSender` 语义的企业微信发送 Adapter。

第一阶段允许只完成接收、Room 记录和 trigger 验证；此时 `sendmessage` 必须明确返回不支持或实际发送错误，不得伪造成功，也不得误用个人微信 UI Sender。企业微信发送方式确定后，继续复用 `/ilink/bot/sendmessage` 的现有请求与成功语义。

### 平台与配置

- 当前使用的 WeWorkFinanceSDK 只在 `linux && cgo` 下提供实现，并依赖 `libWeWorkFinanceSdk_C.so`。
- 当前依赖附带的动态库是 Linux x86-64；第一版企业微信存档功能仅承诺 `linux/amd64`，不得破坏 Webox 既有 `linux/arm64` 构建。未配置或平台不支持时，微信来源仍应正常构建和运行。
- Finance 配置属于 Webox Agent，不进入 `aicat.yaml` 或 `agentgateway.yaml`。aicat 继续只保存 Webox endpoint、token 文件路径、Shared Directory、期望身份、trigger 和 prompt。
- Secret 和 RSA 私钥不得写入仓库、日志或 HTTP 响应。

## Rejected Alternatives

- **把 Finance SDK 链接进 aicat**：macOS 不受支持，并会把 Linux/CGO/动态库约束带入 CLI。
- **独立的 Linux archive proxy 与新 HTTP schema**：会重复 Webox 已有的身份、鉴权、消息信封、Room batch 和媒体交换 interface。
- **在 Webox Agent 中持久化按 Room 消息库**：可靠性更强，但增加数据库、采集 cursor、事务、保留策略和迁移成本，超出第一版目标。
- **让 aicat 管理独立全局 Finance cursor**：会修改现有 per-Room checkpoint 模型和配置，不符合保持 interface 一致的目标。
- **使用最小 Room seq 拉取**：可以避免因 Room cursor 分叉丢消息，但落后或长期空闲 Room 会造成重复扫描；第一版明确选择更简单的最大 `seq` 和 best-effort 语义。

## Execution Order

1. 在 Webox 中引入可选的 `linux/amd64` Finance SDK Adapter 和配置校验，保持非 Finance 与 arm64 构建不受影响。
2. 调整 Webox 内部消息来源 seam，使现有 WeChatDB 来源和 Finance 实时来源共用同一个 HTTP handler 与公开 `wecom.Message`。
3. 实现最大 Room `seq` 单次拉取、解密、身份过滤、Room 映射、消息转换和按 Room 排序分组。
4. 实现 Finance 媒体分片下载、校验和 Shared Directory 落盘；异常直接返回错误。
5. 用假的 Finance Client 验证多 Room 请求、最大 `seq`、新 Room、稀疏递增 `seq`、过滤、错误和取消；用现有 Webox HTTP contract 测试证明请求与响应格式不变。
6. 运行 Webox 格式化、单元测试、`linux/amd64` Finance 构建以及既有 `linux/arm64` 非 Finance 构建。
7. 用 aicat 现有客户端做真实 contract smoke，确认 Room Context Log、trigger、Codex handoff 和 Shared Directory 媒体均无需新协议。
8. 单独确定企业微信发送 Adapter；完成前不宣称企业微信 Claw 已具备闭环回复能力。

## Completion Criteria

- aicat 不增加 Finance 专用客户端、全局 cursor 或消息模型，现有 `getupdates` 客户端可以消费企业微信存档 Room batch。
- Webox Agent 不保存企业微信消息源或消费 cursor；停止并重启后从下一次请求提交的最大 Room `seq` 继续实时拉取。
- 文本、链接、图片和文件至少能够转换为现有 `wecom.Message`，媒体可由 aicat 通过 Shared Directory 访问。
- 群聊和涉及配置身份的单聊拥有稳定非空 Room ID；响应满足现有外层 Room、`msgid` 和同 Room 递增 `seq` 校验。
- SDK、解密、Room 映射、转换和媒体错误具有明确日志；没有 cursor divergence 日志、补偿队列或隐式持久化。
- Finance 功能不会破坏 Webox 现有微信来源、非 Finance 配置和 arm64 构建。
