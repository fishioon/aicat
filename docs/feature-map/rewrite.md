# 规则与脚本插件

用户让 Agent 通过 MCP 管理规则与单文件 JS/TS 插件：先读取状态、校验和试运行，再保存并重启，通过真实请求及改写前后快照验证效果。

## Sub-features

- `rewrite-rules`：规则增删改查、目标匹配、完整排序、单条和全局启停。
- `rewrite-plugins`：读取源码、编译校验、保存/删除插件、查看 SDK 和加载错误。
- `rewrite-test`：用合成输入离线执行一条规则的一个阶段。
- `rewrite-runtime`：四阶段有序改写、自定义响应、前后快照与执行日志。
- `rewrite-failure`：异常、非法输出、超时与预算耗尽时回退；大 body/SSE 保持转发。
- `rewrite-revision`：保存版本与运行版本分离，并发修改冲突检查。

## How to get to it (user POV)

- 在 Agent 配置 aicat MCP → `plugins_guide` → `rewrite_status`、`rules_list`、`plugins_list`。
- 编辑规则使用 `rules_get/put/delete/reorder/set_enabled`；编辑插件使用 `plugins_get/validate/put/delete`。
- `rules_test` 离线试运行；`rewrite_set_enabled` 保存全局开关；重启应用后通过流量详情与 `rewrite_logs` 核对效果。
- 当前桌面无规则编辑页；扩展列表也不提供脚本插件安装入口。

## Driving it with MCP and HTTP fixtures

Preconditions:

- 使用独立 Home、本地 HTTP/TLS 上游和 MCP harness；准备可记录上游请求数的 fixture。先读 [SDK](../../internal/mcpserver/guide.md)，工具参数按实时 schema。`tool(name, arguments)` 通过真实 `/mcp` 调用，不能直接改服务内部状态代替。

- **读取与校验。** `tool('plugins_guide')` 后依次读取状态、规则和插件。用 `plugins_validate` 提交 manifest 与 source，编译成功返回 hooks/hash，但 `plugins_list` 和本地文件不增加；语法错误应有可定位错误。
- **保存插件。** 用 `plugins_put` 保存 `apiVersion: 1`、合法 id、`main: 'index.ts'` 的 manifest 和单文件源码，带最近读到的 `expectedRevision`；`plugins_get` 可读回一致源码，检查 hash、revision 和 `restartRequired`。每次写操作后重新取得 revision，不沿用最初值。
- **规则。** `rules_put` 保存含 `id/name/enabled/match/plugin/settings` 的规则；匹配支持 `hosts`、`methods`、`pathPrefix`。`rules_reorder` 提交所有规则 ID 各一次，再读取顺序；`rules_set_enabled` 和 `rewrite_set_enabled` 分别保存单条与全局开关，不能当成即时运行状态。
- **离线试运行。** `rules_test` 提交规则、一个准确钩子名和合成 exchange；请求/响应 body 按 SDK 使用 base64。结果包含改写后的 exchange 与 executions；同时检查本地上游请求数不增加，证明没有实际转发。
- **重启与真实效果。** 保存后先发送请求，仍应执行旧运行版本；停止并重启同一隔离实例，`rewrite_status` 的 `revision` 与 `activeRevision` 一致、`restartRequired` 为 false。分别发 HTTP 与已解密 HTTPS 请求，从上游和客户端核对 headers/body/status 的改变，并用 `traffic_get` 比较四阶段快照及 executions。
- **失败与流式。** 分别使用抛异常、死循环、非法输出插件，检查错误日志且当前调用修改撤销、前序成功修改保留，客户端仍完成请求。大 body 和 SSE 验证在上游结束前已收到数据，完整 body 钩子显示 skipped；自定义响应验证上游请求数不增但响应钩子执行。
- **独立性与删除。** 关闭流量采集后再次请求，改写仍生效，`rewrite_logs` 仍有诊断。先尝试删除被规则引用的插件应失败；删除引用规则后删除插件，再重启核对不再执行。用过期 `expectedRevision` 写入必须报冲突且不覆盖新内容。
- **重复验证。** 索引中的 `--only rules` 覆盖 MCP、四阶段改写、回退、预算、大 body/SSE 与重启生效，工件保留合成网络结果及执行报告。

## Gotchas

- 改写全局缺省关闭。保存成功不代表当前运行体已生效，开关修改也要重启。
- 当前插件只支持 `onRequestHeaders`、`onRequest`、`onResponseHeaders`、`onResponse`；各阶段按当前 URL/method 重新匹配，后续规则看到前序成功结果。
- v1 不提供 import、npm、文件、进程或网络 API；插件不是任意宿主机执行入口。默认阶段总预算 200ms、并发 8、runtime 32 MiB、解压后 body 1 MiB。
- 流式转发不等于逐事件/chunk 改写；不承诺 SSE 事件和 WebSocket 消息级脚本能力。
- 编译校验不执行脚本；离线试运行不证明真实网络链路；真实效果也要对照上游与客户端，不能只读脚本日志。
- `rewrite_logs` 为有界诊断，流量也为有界内存；及时保存本次证明，不能当成长期审计存储。
