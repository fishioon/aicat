# Codex 扩展

内置扩展，默认停用。启用后，Codex 的模型列表在 ChatGPT 自带的模型之后加入已接入供应商的模型，写作 `供应商 ID/模型`（如 `deepseek/deepseek-v4`）。选择这类模型时，请求带上 aicat 保管的密钥发给供应商；选择 ChatGPT 自带的模型时，请求原样发往 ChatGPT。Codex 保留自己的 ChatGPT 登录。

## Sub-features

- `codex-toggle`：在扩展里启用或停用，持久化为 `features.codex`；根证书未被信任时先提示安装证书，安装后自动启用。
- `codex-models`：`GET /backend-api/codex/models` 合并 ChatGPT 的列表与供应商模型，ETag 带上 aicat 的标记，供应商模型变化后 Codex 会重新拉取。
- `codex-turn`：`POST /backend-api/codex/responses` 按模型分流；供应商模型只带供应商密钥，去掉 OpenAI 专用字段，SSE 边到边发。
- `codex-entries`：两个入口共用同一个处理器。经系统代理时解密 `chatgpt.com`（不写入用户的解密域名名单）；也可在 `~/.codex/config.toml` 设置 `openai_base_url = "http://<监听地址>/backend-api/codex"` 直连 aicat。
- `provider-model-sync`：保存供应商（地址或密钥有变化）后自动同步其 `/models`，供应商页可“重新同步”。列表存于 Home 下的 `provider-models.json`，不含密钥。
- `codex-catalog`：启用期间按需下载 models.dev 目录到 `cache/models-dev.json`（超过一天才重新下载），补全供应商列表缺少的名称、上下文窗口、推理强度和图片输入。

## How to get to it (user POV)

- 导航栏“扩展” → 已安装 → Codex → 启用/停用；齿轮菜单里是接入供应商、代理设置、复制扩展标识符。
- 概览 → 接入供应商 → 保存后在“模型列表”里查看同步结果，`#provider-sync` 重新同步。
- Codex 侧：开启系统代理并重启 Codex；或按扩展详情里的那一行设置 `openai_base_url`。

## Driving it with Playwright and fixtures

`node e2e/codex.cjs bin/aicat-e2e build/codex-e2e-<run>` 覆盖以下配方，工件在输出目录（`report.json`、`app.log`、截图）。脚本以本机 fixture 代替三个远端：用独立根证书签发 `localhost` 证书的假 ChatGPT 后端（经 `AICAT_E2E_CODEX_BACKEND` 指定，仅 e2e 构建生效）、兼容 OpenAI 的假供应商、假 models.dev（`AICAT_E2E_MODELS_DEV`）。窗口、证书授权流程（OS fixture）、网关解密与流量记录都是真实的。

- **默认停用。** `state().codexEnabled` 为 false；直连入口返回 503；经网关 CONNECT 到后端时看到后端自己的证书（未解密）。
- **启用。** `#nav-extensions` → `#extension-codex` → `#feature-codex`。未信任证书时出现“需要先安装证书”对话框；取消后保持停用，点“安装证书”后启用，`aicat.yaml` 写入 `codex: true`，详情显示 `#codex-status`。
- **同步模型。** 接入供应商选 `#preset-custom`，名称填 `Compat`（ID 为 `compat`），填 fixture 地址与密钥保存；`providers[0].models` 自动出现，向量、出图模型被滤掉，只声明 Chat Completions 的模型留在列表里但不提供给 Codex。
- **模型列表。** 直连与经网关两种入口的 `/backend-api/codex/models` 都返回 ChatGPT 条目加 `compat/...` 条目；上下文窗口、推理强度、默认强度、图片输入先取供应商列表，缺少时取 models.dev；Codex 不支持的 `max` 强度被丢弃；ETag 形如 `"<后端 ETag>+aicat-<标记>"`；后端收到原始 Authorization，没有条件请求头。
- **供应商回合。** zstd 压缩的请求被解开，供应商收到 `Bearer <保管密钥>`、去掉前缀的模型名，没有 `ChatGPT-Account-Id` 等头，也没有 `stream_options.reasoning_summary_delivery`、`configuration_update` 与消息元数据；文本中的 `<` `&` 不被转义；SSE 分段到达。后端收不到这次请求。
- **ChatGPT 回合。** 原样到达后端（正文与账号头不变），响应的 `X-Models-Etag` 与列表 ETag 一致。
- **本地拒绝。** 未知供应商、不在列表或不支持 Responses 的模型返回 400，`/responses/compact` 和带 `compaction_trigger` 的供应商回合返回 400，供应商和后端都收不到。WebSocket 握手在两个入口都得到 426。
- **流量。** 供应商回合按供应商 URL 记录，密钥不出现在任何记录里；经网关的 ChatGPT 回合由普通采集记录。
- **停用。** 再点 `#feature-codex`；`codex: false`，直连入口 503，新建的 CONNECT 不再解密。

## Gotchas

- 解密只影响新连接：启用或停用后，已在运行的 Codex 需要重新启动。
- 供应商模型的对话暂不支持压缩；从 ChatGPT 模型中途切到供应商模型时，历史中的加密内容供应商读不了，建议新开对话。
- 启用期间 Codex 回落到 HTTP/SSE，不使用 WebSocket 优化。
- 只提供协议为 `openai` 的供应商；协议为 `anthropic` 或 `chat`（仅 Chat Completions）的供应商模型列表仍会同步但不提供给 Codex。没有协议转换。
- 模型全部提供，没有勾选；供应商列表里未被关键词滤掉的非对话模型仍会出现。
- 经网关的模型列表与供应商回合由扩展直接应答，流量记录里只有发往上游的一条，没有改写前后对照和应用归属。
- 只用本机 fixture 验证过，没有经真实 Codex、chatgpt.com 或真实供应商验证。
