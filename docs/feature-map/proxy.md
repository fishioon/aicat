# 代理接入

用户为客户端显式配置 aicat 的 HTTP proxy，或授权接管 macOS 的 HTTP/HTTPS 系统代理。监听地址可即时修改；关闭系统代理与退出时恢复原设置。

## Sub-features

- `proxy-explicit`：HTTP 与 CONNECT 请求经过网关，API/MCP 共用监听。
- `proxy-listener`：选择 loopback、通配或本机 IP，修改端口并持久化。
- `proxy-system`：显式开启/关闭系统代理，保持代理设置与实际监听一致。
- `proxy-recovery`：正常退出恢复、异常退出后恢复、外部修改冲突与恢复失败重试。
- `proxy-cli`：以 `serve` 运行同一业务服务，不创建桌面和托盘。

## How to get to it (user POV)

- 导航栏底部的设置 → 代理；也可从通用设置、托盘设置或流量扩展的“代理设置”进入。
- “监听地址”选择 IP、编辑端口；有有效改动时点击“应用”。切换期间锁定输入并显示“应用中…”，失败保留待修改输入，旧监听保持可用。
- “系统代理”使用一个开关直接启用或关闭；仅恢复失败时显示“恢复原设置”重试入口。
- CLI：`bin/aicat-cli serve --home <隔离目录> --addr 127.0.0.1:<空闲端口>`。

## Driving it with Playwright and HTTP

Preconditions:

- 使用独立 Home、本地上游和现有 OS fixture；系统代理初始关闭。系统接管不要求先安装或信任证书；只有 HTTPS 解密需要证书信任。

- **监听与域名表单。** `node e2e/proxy-settings.cjs bin/aicat-e2e build/proxy-settings-<run>` 为独立后台场景，覆盖未修改/非法端口、占用失败回退、通配监听与重启持久化；同时核对域名输入在异步成功/失败、重复提交、配置受阻时的保留和恢复。
- **同连接多目标。** `node e2e/http-origins.cjs bin/aicat-e2e build/http-origins-<run>` 用一条真实客户端到代理的 TCP 连接交替访问两个上游和控制 API，核对每条请求、凭据、二进制正文都到达目标站点；各 POST 只提交一次，SSE 分段到达且结束后仍可复用。
- **显式代理。** 在 harness 中运行 `await send('/proxy-check')`；核对上游响应和 `/api/traffic` 中已完成记录。终端等价请求为 `curl --noproxy '' --proxy "http://127.0.0.1:$AICAT_PROXY_PORT" "http://127.0.0.1:$AICAT_UPSTREAM_PORT/proxy-check"`，两个变量取本次 fixture 端口。
- **监听切换。** 通过导航栏底部的设置进入代理页，在 `#listener-address` 选择 `0.0.0.0`，在 `#listener-port` 填入空闲端口，点击 `#listener-apply`；等待 `#gateway-address` 显示新地址。向 `127.0.0.1:新端口` 发送请求，核对历史记录与 CA 指纹保留；只读检查 `gateway.addr`，重启同一 Home 后复查。
- **输入失败。** 分别填入 `0`、`65536` 和非数字，应用按钮禁用、`#listener-error` 有提示。使用被本次测试占用的端口再应用，旧监听应继续或恢复可用，配置不保存失败目标，并显示错误。
- **系统接管。** 点击 `#system-proxy-toggle`，不弹应用内确认；从 OS fixture 核对 HTTP/HTTPS 地址与端口，原有例外和无关字段保留。再点关闭，核对 OS 设置恢复，而代理 listener 仍可响应。
- **独立后台场景。** `node e2e/system-proxy.cjs bin/aicat-e2e build/system-proxy-<run>`：覆盖无证书信任直接开关、正常接管不误报待恢复、退出恢复、SIGKILL 后重启恢复再接管、恢复失败重试与外部设置保护；保存浅深色截图、日志与报告，不操作真实系统网络设置或原生授权窗口。
- **恢复异常。** 使用现有桌面 harness 的 OS 故障 fixture，覆盖恢复失败、重试及外部修改；失败时出现问题及 `#system-proxy-restore`，应用保留可用监听。重试后验证实际 OS 状态，不能仅断言按钮文字。
- **传输入口。** 运行索引中的 `--only transport`，核对 loopback、通配与 IPv6 的 HTTP/TLS 真实请求和 CA 跨重启保留；该报告不涵盖系统代理设置 UI。

## Gotchas

- 默认地址 `127.0.0.1:9981`；首次运行不启用系统代理、不安装 CA 系统信任。日常验证不要直接使用真实 `~/.aicat/`。
- `0.0.0.0` 是监听选择，客户端和系统代理使用可达的本机地址；控制 API/MCP 仍只允许本机 peer。
- 只接管遵守显式代理或系统代理的客户端，不是 TUN 或全机网络接管。
- 真实系统代理写入必须沿用应用已有授权流程；验证使用 `e2e` OS 适配器，不修改用户真实网络设置。
- 配置保存或运行切换失败时要验证回退；不能仅凭页面显示新端口报告成功。
