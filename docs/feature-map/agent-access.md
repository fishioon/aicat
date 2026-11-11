# Agent 查询接口

Agent 从同一代理监听地址读取真实流量记录，通过 HTTP 获取摘要、详情和 body，或通过带 Bearer 认证的 MCP 使用等价查询。当前管理工具服务的是 aicat 自身。

## Sub-features

- `agent-http`：HTTP 摘要分页、筛选、详情与原始 body 前缀。
- `agent-mcp-connect`：连接 Streamable HTTP、初始化、发现工具及 SDK。
- `agent-mcp-traffic`：查询记录、四个观测位置、body 完整性与改写诊断。
- `agent-access-boundary`：本机入口限制、MCP 认证、Origin 拒绝与目标 authority 检查。

## How to get to it (user POV)

- 从设置 → 代理读取监听地址；默认 HTTP 基地址为 `http://127.0.0.1:9981`。
- HTTP：`GET /api/traffic`、`GET /api/traffic/{id}`、`GET /api/bodies/{id}`。
- Agent 的 MCP 客户端配置 Streamable HTTP 地址 `<基地址>/mcp`，Authorization 为当前 Home 的 `mcp-token` 对应的 Bearer；先调用 `plugins_guide` 获取说明。
- MCP 查询工具为 `traffic_list`、`traffic_get`、`traffic_body`；规则与插件管理见[改写配方](rewrite.md)。

## Driving it with HTTP and MCP

Preconditions:

- 本次隔离实例已就绪、已配置真实代理请求并采集；知道当前监听和 Home，MCP 凭据只留在进程内。以下 `tool` 与 `rpc` 使用索引说明的 rules harness。

- **HTTP 列表。** `GET /api/traffic?limit=2` 返回摘要列表；发出超过两条请求后，使用结果的 `nextPage` 作为下一次 `before`，核对无重复和遗漏。组合 `host`、`method`、`state`、`q` 检查本次 fixture，清楚区分零结果与访问失败。
- **HTTP 详情/body。** 从返回记录取实际 ID 请求 `/api/traffic/{id}`，检查四个位置 `requestReceived`、`requestSent`、`responseReceived`、`responseSent`。从消息 body 引用取真实 body ID 请求 `/api/bodies/{id}`，核对原始字节及 `X-Aicat-Body-State`，同时保留请求和响应证明。
- **MCP 连接。** 调用 `rpc('initialize', {'protocolVersion':'2025-06-18','capabilities':{},'clientInfo':{'name':'feature-map','version':'1'}})`，再调用 `rpc('tools/list')`；工具列表包含流量、规则和插件工具，`tool('plugins_guide')` 返回 SDK，资源 `aicat://plugins/sdk/v1` 可读取同一说明。
- **MCP 流量。** `tool('traffic_list', {'limit': 2})`，取实际 ID 再 `tool('traffic_get', {'id': id})`；以实际 body ID 调用 `tool('traffic_body', {'id': body_id})`，base64 解码后与 HTTP body 比较，并检查 `info.state` 和 `returnedTruncated`。
- **入口限制。** 同一 MCP 请求不带 token 应为 401；正确 token 加浏览器 Origin 应为 403。用真实代理请求访问本地上游的同名 `/api/traffic` 或 `/mcp` 路径，应到达上游，不应被 aicat 控制服务截获。完整参数错误与访问边界可通过 `--only rules` 重复验证。
- **采集独立性。** 先采集记录，再从桌面关闭流量功能；HTTP/MCP 仍能读取保留记录，新代理请求继续转发但不产生新记录。

## Gotchas

- `before` 是请求 ID 边界，不是可续接事件 cursor；记录 Revision 也不是全局事件编号。
- MCP `traffic_list` 的 limit 为 1–200；不要照搬 HTTP 或 UI 一次加载的容量。
- body 为有界快照，且 MCP 单次最多返回 1 MiB；完整性以元数据为准。HTTP 返回字节，MCP 返回 base64。
- HTTP 只读查询与 MCP 的认证方式不同，不向只读 HTTP 虚构 Bearer 要求；控制接口都仍限制本机 peer 和目标 authority。
- 当前没有通用节点配置、证书授权或系统代理管理的 MCP 工具；Agent 不能因为已有 MCP 连接就直接管理这些操作。
- 代理记录不包含可可靠推导的 Agent 任务、进度、产物或 Skill 使用状态。
