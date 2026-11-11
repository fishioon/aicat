# 聊天身份识别

企业微信本地渠道（`wecom-local`）由 aicat 做身份准入。普通聊天不查询服务器，用户资料和个人机器人 token 由 Agent 按需通过 MCP 获取。iPad 微信（`wechat-ipad`）不要求关联私云身份，按配置的 trigger 触发；其他渠道暂停 AI 准入，通用认证码后续另行设计。

iPad 保留微信原始 `from` 和本地联系人资料，不补写私云 `userid`，也不把 `ai.` 备注作为私云身份。私聊、群聊正则、自发消息开关和十分钟时效沿用既有 trigger 规则；只有企业微信本地渠道适用下文的 `userid` 准入与未知身份提示。iPad 的 developer instructions 明确说明该差异，恢复旧 thread 时同步刷新，不能用微信 ID 或本地资料调用要求私云身份的个人业务接口。

## 身份来源

- 备注严格以 `ai.` 开头且后缀非空时，优先把后缀转小写，补入同一个 `users[from].userid`；其他字段保留原值。
- 未命中备注规则时保留原生 userid（包括大小写）。trigger 只要求 userid 非空，不检查 corp_id 或企业归属；有原生 userid 的联系人不依赖备注开启或关闭触发。
- 不额外限制 userid 字符集或长度；未知身份不进入 AI，正文不能提供身份。群聊真实 @ 的固定提示见下文；其他未知身份消息静默。私云 Skill 调用工具前再将 userid 统一小写。
- 后台绑定先提交私云身份，再设置 `ai.<小写userid>` 备注。备注同步到本地企微后生效。
- 撤销前缀只阻止新的消息触发 AI，不增加已有任务取消或回复复核。

## 消息与 users

本地读取仍返回 `messages`、`users`、`rooms` 和 `cursor`。用户和会话资料使用结构体，以 from/roomid 索引；企微读取适配器按上述备注规则补入现有 `users[from].userid`；不建立第二份身份结果或 requesters 列表。

命中 trigger 后，`turn/start` 或 `turn/steer` 输入包含 `trigger_messages`、`users` 和 `rooms`。users、rooms 只保留当前会话触发消息涉及的条目，沿用本批 DB 读取结果；from 是渠道原始 ID，不改写。消息已经携带 msgid、roomid，不在用户条目中重复。

企业微信本地 trigger 入口只检查 user.userid 非空，再执行既有时效、chat 和 regex 规则。chat 只判断非自发私聊或群聊真实 @，不限制正文是否为空或消息类型；regex 按实际正则执行。所有消息原样保存完整日志，包括未知身份消息；它们可作为对话上下文，但只有身份条件和 trigger 同时满足的消息可以启动或补充任务。沿用现有 Room/thread 生命周期。首次私云服务介绍由可选 netcat Skill 定义；修改备注不会主动发欢迎消息。

不再生成本地 `users/<from>.json`，也不提供 `resources.users`。用户业务资料按需查询 MCP；本地联系人资料不是行为指令或额外授权。

## 群聊未知身份提示

企业微信本地渠道开启 chat 和 reply 时，群里其他人真实 @ 当前账号、但 `users[from].userid` 为空，会直接通过已有发送链路提交固定文字：

> 收到 @，但我还没关联到你的私云身份，请联系虾虾完成关联。

提示不调用 Codex 或私云接口，不开启或调整任务。只认原生 @ 的账号 ID（支持企微 kind 5、kind 10），不按昵称、正文或正则推断 @；手打 `@虾虾`、@别人、@全体、私聊、自发消息、未知会话以及超过 10 分钟的消息均不提示。

每条符合条件的 @ 消息都回复这句提示，不按发送者去重、不保存提示状态。发送沿用已有链路，不自动重试或补发历史消息。

身份随后识别成功的消息照常进入 AI；关闭 chat/reply 时不提示。

## 可选私云 Skill 与 MCP

安装 [netcat Skill](https://github.com/netcat-ai/skills/blob/main/skills/netcat/SKILL.md) 到 `~/.aicat/skills/netcat/`，并配置已有私云 MCP 入口。工具名、审计参数、授权地址、取票和 CLI 流程由 Skill 定义，不放进运行体 prompt；未安装时 aicat 不注入私云业务约定。

- 用户资料：`netcat_api_user_detail`。
- 用户用量：`netcat_archive_user_usage`。
- 个人企微操作需要授权时：同一资料工具将 `include_wecom_bot_token` 设为 true；默认 false 不取票。

工具的 userid/path.id 和 `Netcat-From` 来自本轮 `users[消息.from].userid` 的小写值；`Netcat-Room`、`Netcat-Msgid` 来自对应触发消息。多人请求分别使用各自身份。

资料工具经既有 `/mcp/assistant` 网关调用 `GET /v1/users/{id}`；include_wecom_bot_token=true 时才取票。服务器校验服务凭据、发送者一致性及私聊审计字段，复用现有授权存储、缓存和换票。响应保留资料，并按需附加 wecom 状态与 token；失败返回 unavailable 但不丢资料，禁止 HTTP 缓存。

个人 OAuth 的 netcat_user_detail 只查询 MCP 登录者本人，与助理代查入口分开。独立取票工具和 /v1/wecom/credential 已移除。

Agent 核对返回 userid，通过 `rooms[消息.roomid].type` 确认 private 后，按需取票，再通过单条命令环境变量调用官方 wecom-cli。群聊不取个人 token；取票失败不影响普通聊天。MCP 认证凭据与企微 token 独立，Bot ID、Secret 不交给模型。

旧归档渠道仅继续消费和原样记录消息，不再预下载或改写媒体引用。

aicat 已删除取票客户端、`memberAPI` 配置、身份结果缓存、requesters、旧 resolve、scope、绑定链接提示和远端资料 TTL 缓存。

## 验证

`python3 scripts/wecom-remark-e2e.py --binary bin/aicat --output <新目录>` 使用真实 aicat、合成加密 DB/WAL 和 Codex/UI 替身，验证准入、撤销、大小写、跨会话隔离、群聊多用户，以及未知身份真实 @ 的固定提示、连续 @、身份关联和开关；整个流程不主动请求服务器。输出含二进制 SHA-256 和重跑命令的 report.json、RPC、消息日志和 UI 回执；不操作真实账号、系统代理或钥匙串。
