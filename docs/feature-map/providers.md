# 供应商接入

aicat 保管模型供应商的 API Key。本机的 Agent 把模型地址指向 aicat，请求由 aicat 带上密钥转发给供应商，Agent 自己不用保存密钥。

## 能力

- `provider-save`：保存、修改或移除一个供应商的接口地址和 API Key。
- `provider-forward`：经 `/anthropic`、`/openai/v1`、`/compat/v1` 转发请求并附上保管的密钥，流式响应边到边发。
- `provider-records`：转发的请求出现在流量里，不含任何凭据。

## 入口

- 概览 → “接入供应商”；已保存的供应商出现在概览的“已接入”里，点“管理”回到这一页。
- Agent 侧：页面第二步给出的环境变量，例如 `ANTHROPIC_BASE_URL=http://127.0.0.1:9981/anthropic`。

## 前提

- 使用隔离的 `--home`。上游用本机的 HTTP fixture（接口地址允许 `http://127.0.0.1:<端口>`）。
- 运行 `node e2e/provider.cjs bin/aicat-e2e build/provider-e2e` 即覆盖下列配方。

## 配方

- **保存。** 点击 `#connect-provider`，在 `#provider-url` 填 fixture 地址，在 `#provider-key` 填密钥，点击 `#provider-save`。`window.desktop.state().providers` 出现一项，`keyHint` 是密钥末四位；`#provider-key` 被清空。
- **只进文件。** Home 下 `providers.json` 权限为 0600 且含密钥；`aicat.yaml`、页面内容和 `state()` 都不含密钥。
- **拒绝明文地址。** 接口地址填 `http://example.com` 保存，出现错误提示，不生成 `providers.json`。
- **转发。** 向 `http://127.0.0.1:<监听端口>/anthropic/v1/messages` 发 POST，自带一个假的 `x-api-key` 和 `Authorization`。fixture 收到的 `x-api-key` 是保管的密钥，没有 `Authorization`，路径、查询串、正文和其他请求头原样到达；SSE 的两段数据分开到达客户端。
- **流量记录。** `/api/traffic` 里有这条记录，URL 是上游地址，状态 200，类型 SSE。`/api/traffic/{id}` 的全文不含保管的密钥和 Agent 自带的假密钥，`X-Api-Key` 记为 `[aicat]`，没有 `requestSent`；请求和响应正文可读。
- **改地址不丢密钥。** 在概览点“管理”，只改 `#provider-url` 后保存；再发请求仍然带着原密钥。
- **移除。** 点击 `#provider-delete` 并确认；`providers.json` 不再含密钥，请求返回 503，fixture 不再收到请求。
- **未接入与浏览器来源。** 保存前请求返回 503；带 `Origin` 头的请求返回 403。

## 边界

- 每种供应商只保存一份配置和一把密钥；没有按模型路由、多密钥、失败切换或协议转换。
- 路由只接受本机请求，监听在 `0.0.0.0` 时也一样。
- 密钥是明文文件，靠文件权限保护；备份或同步 `~/.aicat/` 时会带上它。
- 转发的记录没有应用归属，在侧栏归到“未知应用”。暂停采集或停用流量时不记录，转发照常。
- Token、费用等用量统计还没有实现。
- 没有经真实的 Anthropic 或 OpenAI 服务验证，只用本机 fixture 验证过。
