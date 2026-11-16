# Agent 查询接口

Agent 从同一代理监听地址读取真实流量记录，通过 HTTP 获取摘要、详情和 body，或通过带 Bearer 认证的 MCP 使用等价查询。当前管理工具服务的是 aicat 自身。

## Sub-features

- `agent-http`：HTTP 摘要分页、筛选、详情与原始 body 前缀。
- `agent-mcp-connect`：从主页接入 Agent，复制 Claude Code、Codex、Cursor 或通用 MCP 指引；按当前 Home 读取令牌，连接 Streamable HTTP、初始化、发现工具。
- `agent-mcp-traffic`：查询记录、四个观测位置、body 完整性与传输错误。
- `agent-access-boundary`：本机入口限制、MCP 认证、Origin 拒绝与目标 authority 检查。

## How to get to it (user POV)

- 主页 → 接入 Agent；选择客户端后复制配置。Claude Code 命令运行时读取令牌；Codex 另提供在启动终端设置 `AICAT_MCP_TOKEN` 的命令；Cursor 和其他客户端提供读取令牌的命令，用其结果替换配置占位内容。令牌路径取当前实例的 Home，不固定为默认目录。
- 从设置 → 代理读取监听地址；默认 HTTP 基地址为 `http://127.0.0.1:9981`。
- HTTP：`GET /api/traffic`、`GET /api/traffic/{id}`、`GET /api/bodies/{id}`。
- Agent 的 MCP 客户端配置 Streamable HTTP 地址 `<基地址>/mcp`，Authorization 为当前 Home 的 `mcp-token` 对应的 Bearer。
- MCP 查询工具为 `traffic_list`、`traffic_get`、`traffic_body`；`rooms_list` 返回稳定Room目标，`messages_send` 要求`target_room_id`并交渠道发送；[定时消息](schedules.md)通过 schedules_set、schedules_list、schedules_delete 管理。

## Driving it with HTTP and MCP

Preconditions:

- 本次隔离实例已就绪、已配置真实代理请求并采集；知道当前监听和 Home，MCP 凭据只留在进程内。以下 `tool` 与 `rpc` 使用索引说明的 MCP harness。

- **接入指引。** `node e2e/connect.cjs bin/aicat-e2e build/connect-<run>` 后台启动应用，通过实际按钮复制各客户端配置和令牌读取命令，核对系统剪贴板、shell 参数与真实 MCP 认证。输出目录可含空格、单引号、`$()` 和反引号以检查命令转义。Claude 命令由本地接收器验证参数；Codex/Cursor 不启动真实客户端、不修改用户配置，不据此声明第三方客户端的原生接入已覆盖。工件保存报告、源码和浅深色截图，不含令牌内容。
- **完整查询与生命周期。** `node e2e/agent-traffic.cjs bin/aicat-e2e build/agent-traffic-<run>` 使用后台应用、headless 界面与真实本地上游，核对 HTTP/MCP 分页、组合筛选、参数错误、四个观测位置、二进制正文、两级正文上限、流式与错误状态、淘汰/清空/停用采集及重启；报告、查询记录和正文完整性证明保存在输出目录。`python3 e2e/mcp.py --binary bin/aicat-cli-e2e --output build/mcp-<run>` 补充 CLI、认证边界、较小采集前缀和历史配置拒绝场景。
- **HTTP 列表。** `GET /api/traffic?limit=2` 返回摘要列表；发出超过两条请求后，使用结果的 `nextPage` 作为下一次 `before`，核对无重复和遗漏。组合 `host`、`method`、`state`、`q` 检查本次 fixture；主机名匹配其全部端口，带端口则限定该目标，IPv6 支持裸地址、方括号地址和带端口形式。清楚区分零结果与访问失败。
- **HTTP 详情/body。** 从返回记录取实际 ID 请求 `/api/traffic/{id}`，检查四个位置 `requestReceived`、`requestSent`、`responseReceived`、`responseSent`。从消息 body 引用取真实 body ID 请求 `/api/bodies/{id}`，核对原始字节及 `X-Aicat-Body-State`，同时保留请求和响应证明。
- **MCP 连接。** 调用 `rpc('initialize', {'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'feature-map','version':'1'}})`，再调用 `rpc('tools/list')`；工具列表包含 traffic_list、traffic_get、traffic_body、rooms_list、messages_send、schedules_set、schedules_list、schedules_delete；脚本工具和 SDK 资源不可用。
- **MCP 流量。** `tool('traffic_list', {'limit': 2})`，将返回的 `nextPage` 直接作为 `before` 续页。可组合 `host`、`method`、`state`、`query`，状态包括 `request`、`response`、`completed`、`failed`、`canceled`；省略状态或空字符串匹配全部。取实际 ID 再 `tool('traffic_get', {'id': id})`；以实际 body ID 调用 `tool('traffic_body', {'id': body_id})`，base64 解码后与 HTTP body 比较，并检查 `info.state` 和 `returnedTruncated`。
- **控制流量隔离。** `node e2e/control-traffic.cjs bin/aicat-e2e build/control-traffic-<run>` 通过实际界面配置本地供应商，验证完整 URL 的 MCP 认证/Origin、控制流量排除、模型 SSE 首段及时到达与单条脱敏记录、外部同名路径正常转发采集；保存报告和截图，不覆盖原生窗口。
- **入口限制。** 同一 MCP 请求不带 token 应为 401；正确 token 加浏览器 Origin 应为 403。用真实代理请求访问本地上游的同名 `/api/traffic` 或 `/mcp` 路径，应到达上游，不应被 aicat 控制服务截获。完整参数错误与访问边界可通过 `--only mcp` 重复验证。
- **采集独立性。** 先采集记录，再从桌面关闭流量功能；HTTP/MCP 仍能读取保留记录，新代理请求继续转发但不产生新记录。

## Gotchas

- `before` 是请求 ID 上界，续页读取更旧的记录；可直接使用字符串 `nextPage`，也兼容数字格式。省略或传 0 从最新记录开始；Revision 属于单条记录。
- MCP `traffic_list` 省略 limit 时返回至多 50 条；显式 limit 为 1–200，HTTP 列表则支持至多 2000 条。
- body 为有界快照，且 MCP 单次最多返回 1 MiB；HTTP 返回字节，MCP 返回 base64。查询成功表示快照可读，完整性仍需同时检查 `info.state`、`captured/observed` 和 `returnedTruncated`；采集截断与 MCP 返回截断独立，网络读取失败也可返回已保留的前缀。
- HTTP 只读查询与 MCP 的认证方式不同，不向只读 HTTP 虚构 Bearer 要求；控制接口都仍限制本机 peer 和目标 authority。
- 当前没有通用节点配置、证书授权或系统代理管理的 MCP 工具；Agent 不能因为已有 MCP 连接就直接管理这些操作。
- 代理记录不包含可可靠推导的 Agent 任务、进度、产物或 Skill 使用状态。
