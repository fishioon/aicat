# 网关本体就是 go-mitmproxy，涉及网关的业务一律是 addon

状态：accepted；取代 [0017](0017-builtin-gateway-for-codex-wiring.md) 中「入口与转发由 aicat 用标准库
`net/http` 自行实现」的部分，并取代本文上一版「业务规则沉在 addon 之外的纯函数层」的决策。0017 的其余决策继续
有效：standalone agentgateway 及其 LaunchAgent 不回潮、网关与业务 in-process、只绑 loopback、`~/.aicat/auth.json`
承载私云凭据、用户凭据只透传。system proxy 的约定由 [0020](0020-macos-certificate-and-system-proxy.md)
取代：用户可在 UI 显式开启，并由 aicat 保存与恢复原设置。

网关不是 aicat 自研的 HTTP 服务，也不是包一层框架：**本体就是
[go-mitmproxy](https://github.com/lqqyt2423/go-mitmproxy) 的装配**（MIT、纯 Go、无 CGO，以库的形式链进
aicat binary，不引入外部二进制、不引入第二个长期进程）。`internal/gateway/` 里只有装配——监听地址、拦截规则、
CA、生命周期、流式回复——没有 Codex、私云或过滤逻辑。

**涉及网关的业务一律是 addon**，放 `internal/gateway/addons/<业务>/`，实现 go-mitmproxy 的 `proxy.Addon`（嵌
`proxy.BaseAddon`，要几个钩子实现几个）：

- 一个业务一个包，可独立开关、独立测试；
- 现状：`addons/codex/`，承担 Codex 的 `/backend-api/codex`；
- 之后：广告过滤、抓包、其它上游，都按同一形状加，不新开入口、不新开进程；
- 不涉及网关的业务（私云登录、`auth.json`、模型目录、消息连接器、Tailscale 管理器）是普通包，不是 addon。

## 一个端口，三种 wire 形态

客户端配哪一种都能进同一个 addon：

1. **代理 + MITM**：`HTTPS_PROXY=http://127.0.0.1:9981`，到 `chatgpt.com` 的 CONNECT 被解密后进 addon；其它
   host 由 `SetShouldInterceptRule` 的白名单挡下，直接隧道转发、不解密。
2. **代理 + 明文**：同样的 `HTTP_PROXY`，`openai_base_url` 用 `http://` 时是 absolute-form，不需要 CA。
3. **origin**：`openai_base_url = http://127.0.0.1:9981/backend-api/codex`，请求以 aicat 自己为目标。

## Why

自研入口要自己承担 CONNECT、TLS 握手、证书签发与缓存、HTTP/2、WebSocket、SSE 与流式 body，这些都与 aicat 的
领域无关，go-mitmproxy 已经做完并有对应测试（`attacker_http2_test.go`、`websocket_test.go`、`sse_test.go`）。
它提供的接缝正好对上我们的需求：`Requestheaders` 在读取 body 前触发（WebSocket 升级可以在这一步直接回 426）；
addon 改 `Flow.Request.URL` 的 host 或 scheme 会自动切到独立 `http.Client`（按模型分流不用伪造证书）；短路响应
支持 `Response.BodyReader` 并分块 `Flush()`（SSE 能流式回）。

把业务做成 addon，而不是写进网关本体，是为了让网关这一层永远只有"怎么进来、怎么出去"：新能力是新增一个包，改
不到入口，也改不到别的业务。

## 实测事实（本机 codex-cli 0.162.0-alpha.17.2）

- Codex 遵循 `HTTP_PROXY`/`HTTPS_PROXY`：阻断探针立即报 `HTTP CONNECT failed`。
- 代理下 loopback 不绕过：到 `127.0.0.1` 的请求也以 absolute-form 走代理；WebSocket 升级以
  `CONNECT 127.0.0.1:<port>` 出现，隧道里是明文 `GET /` 而不是 TLS，必须特判。
- MITM 不需要管理员权限：只加 `SSL_CERT_FILE=<自签 CA>` 就能让 rustls 握手成功，把 `chatgpt.com` 签进 SAN 后
  不改任何 Codex 配置就能读到明文。当前用户钥匙串 CA 与纯系统代理接线后续已实测，见 [0020](0020-macos-certificate-and-system-proxy.md)。
- `/backend-api/codex` 全是明文：`GET .../models?client_version=…` 带用户 Bearer 与 `Chatgpt-Account-Id`；
  `/responses` 先以 `Upgrade: websocket` 抢跑，回落 `POST` 时是 `Content-Encoding: zstd` +
  `Accept: text/event-stream`。
- WebSocket 拒绝方式的代价不同：426 只重试 1 次、约 1.6 秒回落；200/404 重试 6 次、约 9 秒。magpie 用 426。
- `models` 接口 200，`{"models": […]}` 5 项，条目约 50 个字段；私云条目必须以上游条目为模板派生，否则可能被
  schema 校验拒掉。

## 库的已知形状（实现时必须绕开）

- **origin 形态不进 `Request`/`Response` 钩子**：非 absolute-form 的请求只触发 `AccessProxyServer`，addon 不写就
  回 400。所以 origin 形态由 addon 自己在 `AccessProxyServer` 里转发（`addons/codex` 的现状）。
- **`helper.ResponseCheck` 没有 `Unwrap()`**：entry 交给 addon 的 writer 断不出 `http.Flusher`，SSE 会掉帧。流式
  回复要用 `gateway.Stream`——状态与响应头走 entry 给的 writer（entry 据此知道请求已被回答，不会补写 400），body
  走它下面那层并逐块 flush。
- **`StreamLargeBodies`（库默认 5MB）以上不再触发 `Request` 钩子**：aicat 默认抬到 32MB，好让请求侧改写（按
  model 分流）在大 body 下仍然可用，代价是内存。
- 库用 logrus 输出，需要桥接到 aicat 的 `log/slog`，避免两套日志。

## 端到端验证

`internal/gateway/e2e_test.go` 用真实 socket、真实 CA、真实 TLS 握手走同一个端口，覆盖上面三种形态，并在
`internal/gateway/testdata/e2e-report.json` 留下每次运行可复核的工件：

- **origin 形态**：`openai_base_url` 指向 aicat 时，请求被改写成 Codex 的 host、body 原样透传，且
  `text/event-stream` 在 upstream 尚未结束时就逐块到达客户端（对照实验：去掉 `gateway.Stream` 的逐块 flush，
  该用例 3 秒内失败）；
- **absolute-form**：同一个端口既把 `/backend-api/codex` 交给 upstream，也让无关路径原样通过；WebSocket 升级
  在不拨号 upstream 的前提下回 426；
- **CONNECT + MITM**：`chatgpt.com` 的隧道被解密，客户端只用 aicat 自己的 CA 即可验证 aicat 出示的叶子证书；
- **非白名单 CONNECT**：原样隧道转发，明文往返，沿途不出现 aicat 的证书。

未覆盖的是 MITM 隧道内普通请求发往真实 `chatgpt.com` 的那一次上游拨号：离线跑通需要库对上游证书校验开个口子
（`SslInsecure` 或自备 `RootCAs`），第一版不为测试引入这个开关。真实 Codex 走 `HTTP_PROXY` 的探针结论见上节。

## Consequences

- 入口是代理协议或 aicat 自己的 origin，两种接线都要写进 Codex Wiring 的用户说明；
- 新增若干纯 Go 依赖（`go-mitmproxy` 及其传递依赖）；
- 抓包默认关闭；一旦开启必须先脱敏（`Authorization`、`chatgpt-account-id`、attestation 之类），且范围只覆盖
  白名单 host；
- CA、钥匙串安装与 MITM 属于第一版（见 [0017](0017-builtin-gateway-for-codex-wiring.md)）；system proxy 由
  [0020](0020-macos-certificate-and-system-proxy.md) 规定；
- 库的 origin 形态缺口与 `ResponseCheck` 缺口是上游问题，先在 addon 侧绕开；若将来要清掉，优先向上游提补丁而不
  维护 aicat 私有 fork。
