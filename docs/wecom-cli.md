# 私聊直接调用企微 CLI

用户在私云 `/console/profile` 扫码绑定官方个人机器人。Bot ID、Secret 和加密的
access token 保留在 `auth.account`。企业微信本地私聊按需从用户资料工具取得当前用户可用的企微 token，
模型直接通过单次命令的环境变量调用官方 CLI。不增加业务代理或动态工具。

## 调用流程

以下流程由可选 [netcat Skill](https://github.com/netcat-ai/skills/blob/main/skills/netcat/SKILL.md) 提供。安装到 `~/.aicat/skills/netcat/` 并配置私云 MCP 即可使用，不需要在 agent prompt 中编写私云流程。

aicat 只识别本地身份，通过本轮 `users[消息.from].userid` 传递身份，不调用取票接口。
Agent 需要操作个人企微资源时，按需调用 MCP `netcat_api_user_detail` 并设置 `include_wecom_bot_token: true`，将对应 userid 转小写并携带触发消息的审计字段；通过 rooms 中的 type 判断会话类型。
工具经既有网关查询用户资料，按需附加 wecom.status 和可用 accessToken；默认资料查询不返回或刷新 token；Agent 核对身份后使用。
不返回有效期、Bot ID、Secret 或私云 User.id。群聊禁止取票，授权不可用不阻断普通聊天。

运行 aicat 的 PATH 中需要官方 `@wecom/cli >= 1.3.1`（本地使用 1.3.4）。
aicat 优先从自身 PATH 定位 CLI；macOS 图形启动缺少 Homebrew PATH 时，补查
`/opt/homebrew/bin/wecom-cli` 和 `/usr/local/bin/wecom-cli`。将绝对路径作为 resources.wecom_cli 交给模型，避免登录 shell
改变 PATH 后误用其他版本。模型使用原生 shell，为单次 CLI 进程设置环境变量：

```sh
WECOM_CLI_ACCESS_TOKEN='<本轮 accessToken>' wecom-cli identity whoami
```

不需要 fork、自定义 endpoint、编译开关、请求头或 Keeper 业务代理。先用 `--schema`
查询目标方法，再按用户要求执行。不要全局 export、复用历史 token 或读取其他用户凭据。
可用 `WECOM_CLI_CONFIG_DIR` 指向本次操作的空临时目录，隔离 CLI 缓存；结束后删除。

Bot ID、Secret 不交给模型。access token 会进入模型上下文和原生 shell 调用记录，
这是本版直接调用方案的取舍；不得公开到聊天回复、发布目录或排查报告。
此版本只支持私聊，不提供群聊的凭据隔离保障。

仅个人企微操作需要时由 Agent 调 MCP 取票；普通聊天和历史消息不取票。私云复用未过期 token，必要时用保存的 Bot Secret
换新票。上游未给 expires_in 时不虚构有效期；本地缓存最多复用五分钟。
执行中凭据失效时停止，不自动重放写操作；后续需要授权的操作再按需取票。
这不是任务内主动刷新机制，尚未到本地缓存期限的提前失效 token 不保证立刻换新。
机器人未绑定或换票失败时仍允许普通聊天，不借用其他用户授权。
机器人未绑定时，netcat Skill 使用本轮资料中的企业邮箱，通过现有企微账号私发绑定入口。
文档操作明确要求重新授权并返回链接时，以同一方式私发该链接；普通网络错误和单份文档无访问权限不视为授权过期。
发送完成后当前渠道提示“已在企业微信中发送授权链接，请前往企业微信完成授权”，发送失败不声称已发送，也不自动重试或恢复写操作。

Skill 直接调用 `resources.aicat_cli` 的 `messages send --agent <resources.agent_id> --search-name <本轮企业邮箱> --stdin`，
使用当前企微 Agent，无额外发送脚本或跨账号选择。命令遵守该 Agent 的 enabled/reply 开关。
协议升级会重建旧 Codex thread，刷新模型指令；messages.log 保留。

## 验证失败方式（实现前列出）

- 两个私聊混用 token；同批群聊复用私聊查询缓存而泄露 token。
- 取票失败阻断普通身份准入；身份未关联或机器人未绑定却尝试调用 CLI。
- CLI 继承其他用户凭据；把临时环境变量设成共享 app-server 的全局环境。
- 已过期 token 不换票；用网络错误触发写操作重放；真实 token 出现在验证报告。
- 旧线程残留动态工具；依赖自定义 CLI 构建才能在真实企微执行。
- 只测试假 CLI/假模型却声称真实模型和官方 CLI 已跑通。

本地 E2E 位于私云 `web/e2e/wecom-bot.ts`；使用隔离数据库，报告明确夹具和真实链路。
`make verify` 检查 aicat 格式、现有测试及 release 构建。验证不修改系统代理或真实钥匙串。

## 知识库收录

通过 AI Cat Skill Set 全局共享 [个人知识库](knowledge.md)，使用同一份本轮用户授权。
