# 供应商接入

aicat 保管模型供应商的 API Key。本机的 Agent 把模型地址指向 aicat，请求由 aicat 带上密钥转发给供应商，Agent 自己不用保存密钥。

## 能力

- `provider-save`：新增、修改或移除供应商。每个供应商是一个命名实例，有自己的 ID、名称、接口协议（`anthropic`、`openai`、仅 Chat 的 `chat`）、接口地址和 API Key；同一家可以接入多次。ID 由预置或名称生成，保存后不变。
- `provider-presets`：新增时先从预置目录选供应商，接口地址和协议自动带出，只需填密钥，“获取密钥”打开供应商的密钥页面；列表里没有的选“自定义”。预置在 `ui/src/state/providers.ts`，地址取自 Magpie。
- `provider-forward`：经 `/p/<ID>` 转发请求并附上保管的密钥，流式响应边到边发。`/p/<ID>` 等同于供应商的接口地址。
- `provider-status`：模型列表同步的结果作为连通状态显示：已连通、密钥被拒（401/403）、连不上、这个地址没有模型列表。未连通时不拦截保存，但第二步不标记为可用，概览的供应商条目显示“密钥无效”或“连不上”。
- `provider-records`：转发的请求出现在流量里，不含任何凭据。
- `provider-model-sync`：保存（地址或密钥有变化）后自动向供应商的 `/models` 同步模型列表，可手动重新同步；Codex 扩展用它向 Codex 提供模型，见 [Codex 扩展](codex.md)。

## 入口

- 概览 → “接入供应商”；已保存的供应商出现在概览的“已接入”里，点“管理”回到这一页。
- Agent 侧：页面第二步给出的环境变量，例如 `ANTHROPIC_BASE_URL=http://127.0.0.1:9981/p/anthropic`。

## 前提

- 使用隔离的 `--home`。上游用本机的 HTTP fixture（接口地址允许 `http://127.0.0.1:<端口>`）。
- 运行 `node e2e/provider.cjs bin/aicat-e2e build/provider-e2e` 即覆盖下列配方。

## 配方

- **旧文件。** 启动前在 Home 放一份旧格式的 `providers.json`（`{"openai":{...}}`，地址不带 `/v1`）。启动后 `state().providers[0]` 的 ID 是 `openai`、挂在 `/p/openai`；模型列表只在 `/v1/models` 有回答，接口地址被补上 `/v1`，`/p/openai/chat/completions` 到达 fixture 的 `/v1/chat/completions`。旧路径 `/openai/v1/...` 返回 404。
- **预置。** 点击 `#connect-provider` 出现 `#provider-presets`，点击 `#preset-anthropic`，`#provider-name` 与 `#provider-url` 已填好；`#preset-custom` 是自定义。
- **被拒的密钥。** 填一把 fixture 不认的密钥保存：供应商的 `models.errorKind` 是 `auth`，出现 `#provider-status.bad`，`#provider-snippet` 所在步骤没有 `now`；概览条目显示“密钥无效”。
- **保存。** 在 `#provider-url` 填 fixture 地址，在 `#provider-key` 填密钥，点击 `#provider-save`。出现 `#provider-status.ok`，`window.desktop.state().providers` 里该项的 `keyHint` 是密钥末四位；`#provider-key` 被清空。
- **只进文件。** Home 下 `providers.json` 权限为 0600 且含密钥；`aicat.yaml`、页面内容和 `state()` 都不含密钥。
- **拒绝明文地址。** 接口地址填 `http://example.com` 保存，出现错误提示，`providers.json` 不含这把密钥。
- **转发。** 向 `http://127.0.0.1:<监听端口>/p/anthropic/v1/messages` 发 POST，自带一个假的 `x-api-key` 和 `Authorization`。fixture 收到的 `x-api-key` 是保管的密钥，没有 `Authorization`，路径、查询串、正文和其他请求头原样到达；SSE 的两段数据分开到达客户端。
- **流量记录。** `/api/traffic` 里有这条记录，URL 是上游地址，状态 200，类型 SSE。`/api/traffic/{id}` 的全文不含保管的密钥和 Agent 自带的假密钥，`X-Api-Key` 记为 `[aicat]`，没有 `requestSent`；请求和响应正文可读。
- **改地址不丢密钥。** 在概览点“管理”，只改 `#provider-url` 后保存；再发请求仍然带着原密钥。
- **移除。** 点击 `#provider-delete` 并确认，回到概览；`providers.json` 不再含密钥，请求返回 503，fixture 不再收到请求。
- **同步模型列表。** 保存后 fixture 收到一次带保管密钥的 `GET /v1/models`；转发断言只看 `/v1/messages` 请求。模型列表本身的配方见 [Codex 扩展](codex.md)。
- **未接入与浏览器来源。** 保存前请求返回 503；带 `Origin` 头的请求返回 403。

## 边界

- 一个供应商只有一种协议、一个地址和一把密钥；同一家的两种协议要接入两次。没有多密钥、失败切换或协议转换。模型列表全部同步，没有勾选，也不能手填。
- 连通状态只来自模型列表请求，不发真实的模型请求；不提供模型列表的供应商显示为“没有找到模型列表”，转发不受影响。
- 协议是 Anthropic 且地址不是 `api.anthropic.com` 时，模型列表请求同时带 `x-api-key` 和 `Authorization: Bearer`；转发仍只带 `x-api-key`。
- 没有本机服务（如 Ollama）的预置：密钥必填。
- 旧的 `/anthropic`、`/openai/v1`、`/compat/v1` 路径已移除；旧 `providers.json` 在启动时读成 ID 为 `anthropic`、`openai`、`compat` 的供应商，下次保存时改写为新格式。
- 路由只接受本机请求，监听在 `0.0.0.0` 时也一样。
- 密钥是明文文件，靠文件权限保护；备份或同步 `~/.aicat/` 时会带上它。
- 转发的记录没有应用归属，在侧栏归到“未知应用”。暂停采集或停用流量时不记录，转发照常。
- Token、费用等用量统计还没有实现。
- 没有经真实的 Anthropic 或 OpenAI 服务验证，只用本机 fixture 验证过。
