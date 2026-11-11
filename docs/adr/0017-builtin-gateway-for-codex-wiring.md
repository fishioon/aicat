# 内置 Go 网关取代 agentgateway，并作为 Codex 的唯一接线点

状态：accepted；取代 [0004](0004-go-exec-launcher.md) 与 [0009](0009-launchd-agentgateway-lifecycle.md)，
取代 [0005](0005-claw-room-and-child-host.md) 中「以前台 child 托管 standalone agentgateway」的 host 决策
（Room、thread 绑定等 Claw 决策保留），并取代 [0006](0006-generate-ca-without-system-trust.md) 中「CA 是启动
时准备的本地状态」与「CA 不被系统信任」这两条前提。入口与转发的实现方式由
[0018](0018-go-mitmproxy-entry-with-addon-business.md) 取代（网关本体就是 go-mitmproxy 库的装配，业务一律是
addon；仍然是 in-process、不引入外部二进制）；本文其余决策继续有效。

核心中转改用 aicat 内置的 Go 实现：in-process、只监听 loopback、默认开启（一个 `gateway.enabled` 开关，地址
默认 `127.0.0.1:9981`，仍可按 Loopback 或 Tailnet 显式改绑）。standalone `agentgateway` binary、
`~/.aicat/agentgateway.yaml`、`agentgateway.mode: child|launchd` 和 AgentGateway LaunchAgent 全部下线，不再有
「agentgateway 由谁保活」的问题。

登录直接跳浏览器完成，结果写入 `~/.aicat/auth.json`（权限 0600），形态参考 codex 的 `auth.json`，不使用
Keychain。不设独立的 login 开关，只用一个开关控制「是否把拉到的私云 Provider 写进网关」，默认关闭。

第一版的网关能力只有 Codex 接线，且必须让私云模型与用户自己的模型并存：

- Codex 不新增 `model_providers` 表项，而是把 `openai_base_url` 指向
  `http://127.0.0.1:<port>/backend-api/codex`，或者让 Codex 经 aicat 的代理入口访问 `chatgpt.com` 并被 MITM。
  `model_provider` 是单选，用 `model_providers` 替换 provider 会让用户自己的 ChatGPT 订阅模型和第三方模型从
  列表里消失，所以不能走那条路；
- `GET /backend-api/codex/models` 返回并集 catalog（Codex 形状的 `{"models": [...]}`）：私云模型加用户自己账号
  可用的模型；
- `POST /backend-api/codex/responses` 按模型分流：私云模型发往私云 API，其余发往 `chatgpt.com` 并透传用户本机
  原有凭据。用户凭据只透传，不复制、不上云。

WebSocket 第一版不做桥接：收到带 `Upgrade: websocket` 的握手直接回 426，让 Codex 回落到 HTTP/SSE，行为与
magpie 一致（回 426 只让 Codex 重试一次，回 200/404 会让它重试六次）。私云官方同样使用 agentgateway 并支持
`responses` 协议，所以本机不做协议转换。`127.0.0.1:8888` forward proxy 和 X 评论过滤第一版不做。

MITM 属于第一版：aicat CA 在 Serve Session 启动时准备，并由 aicat 作为受信任根证书写进 macOS 钥匙串（首次需
用户确认）。

## Why

一个约 92MB 的独立二进制、第二份 YAML 和一套 LaunchAgent 生命周期，对一个「本机能力合集」来说过重：托管进程
的 readiness、signal、logging、drain 协调远比网关本身的逻辑复杂，而 aicat 需要的只是本机中转。

Codex 侧只有两条路：替换 provider，或者挡住 `openai_base_url` 再按模型转发。用户要求私云模型和自己的订阅模型
同时出现在 Codex 模型列表里，所以只能选后者。

426 回落是 Codex 官方设计的路径（`force_http_fallback()` 生效后，本会话不再尝试 WebSocket），magpie 也这样做，
因此第一版不承担 WebSocket 桥接的复杂度。

## Consequences

- 删除 `internal/agentgateway/`（runtime、LaunchAgent 与测试）、`cmd/aicat/agentgateway.go`、
  `internal/app/agentgateway.go`、`make agentgateway` 与 `bin/agentgateway`；Claw 自己的 Codex app-server host
  保留；
- `~/.aicat/agentgateway.yaml` 整份下线；Claw 目前经由 agentgateway `/v1/images/generations` 出图的能力需要改接
  内置网关或直连私云；
- aicat CA 与网关一起启用：在 Serve Session 启动时准备，由 aicat 写进 macOS 钥匙串作为受信任根证书，
  [0006](0006-generate-ca-without-system-trust.md) 中「不修改系统信任」的决策作废（该 ADR 的其余部分也被本文
  与 [0018](0018-go-mitmproxy-entry-with-addon-business.md) 取代）；
- 网关默认只绑 loopback；对外开放（例如共享给手机或 Tailnet）时必须要求 key，沿用 magpie 的网络准入约定；
- 丢掉 ChatGPT 腿的 WebSocket 优化：增量上行（`previous_response_id` 加 delta）、连接复用、prewarm、
  permessage-deflate 与带内 `response.interrupt`。该协议仍是 beta 且有 60 分钟连接上限，后续实测确有必要时再补
  「只桥 ChatGPT 腿」的实现；
- 网关本体是 go-mitmproxy 的装配，业务一律是 addon：新能力按 `internal/gateway/addons/<业务>/` 增量接入，不新
  开入口、不新开进程（见 [0018](0018-go-mitmproxy-entry-with-addon-business.md)）；
- 私云服务端若直接提供 Codex 形状的 catalog，本机只需合并，不必做形状转换。
