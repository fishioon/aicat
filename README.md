# aicat

从零重写的原生桌面流量工具：mygo Native UI + go-mitmproxy + moejs。当前原型跑通真实 HTTP/HTTPS 流量、内存采集与 native UI，不沿用旧业务代码。

本仓库只包含 aicat 桌面端；微信小程序已迁到 `netcat/wxapp`，不在本仓库。根 `go.mod`、`cmd/`、`internal/` 和 `assets/` 是 Go 主工程。构建、验证与已安装应用的签名约束见 [开发约定](docs/development.md)。

需要 Go 1.27.3+。macOS 本地构建：

```sh
make verify
bin/aicat --home "$PWD/build/preview/.aicat"
```

`--home` 用于隔离体验数据；默认目录为 `~/.aicat/`。也可运行 `make app`，产物为 `build/native/aicat.app`。本地 app 未签名，不覆盖已安装版本。`make cli` 生成同一运行体的 CLI 入口：`bin/aicat-cli serve`。

窗口依照「UI」对话的原型稿实现：48px 单行标题栏、导航栏、可折叠域名树、请求列表、下方双栏详情和账号菜单。支持域名筛选、右键固定域名、搜索、请求和响应独立的 Header / Raw 切换、暂停采集、清空与深浅主题。固定域名保存在 aicat.yaml，窗口中的查看方式在本次运行保持。固定列表为空时仅显示分组标题，不显示占位提示。未选择请求时，详情区居中显示“未选择请求”，选中后才显示详情控件；按 Escape、清空或记录过期后恢复空状态。

系统托盘提供“打开 aicat”“暂停/继续采集”“设置…”和“退出 aicat”。关闭主窗口后，窗口隐藏，代理和采集继续运行；通过托盘或 Dock 可重新打开。暂停采集只影响新记录，代理继续转发。托盘菜单与窗口共用同一采集状态。菜单顶部显示 aicat 品牌和代理地址摘要，采集操作、窗口/设置、退出分别分组；暂停时副标题明确提示代理仍继续转发。“设置”和“退出”分别显示 `⌘,`、`⌘Q`，使用普通菜单操作，不添加开关。托盘使用保持图案比例的专用模板素材，在 18pt 画布中可见高度约 15.6pt；`make build` 会在 SVG 或生成器变化时更新素材。

## 代理与 API

默认统一入口为 `127.0.0.1:9981`。代理设置中可选择 `127.0.0.1`、`0.0.0.0` 或本机网卡 IP，并单独编辑端口，点击“应用”立即切换并保存。客户端须显式配置此 HTTP proxy；API/MCP 仍限制本机访问。首次启动不修改系统代理或安装 CA 信任。

```sh
curl --noproxy '' --proxy http://127.0.0.1:9981 http://example.com
curl http://127.0.0.1:9981/api/traffic
```

设置分为“通用”和“代理”。导航栏的“扩展”独立提供搜索、可折叠列表与扩展详情；当前内置流量扩展，社区发现和已安装列表暂为占位。代理页统一管理代理地址、系统代理、证书和解密域名，不展示开发配置路径。

唯一持久行为配置是数据目录中的 `aicat.yaml`，首次运行生成默认值：

```yaml
systemProxy:
  enabled: false
features:
  traffic: true
gateway:
  addr: 127.0.0.1:9981
  interceptHosts: []
  bufferedBodyLimit: 1048576
traffic:
  maxRecords: 2000
  maxBodyBytes: 262144
  maxTotalBodyBytes: 67108864
```

在“设置 → 通用”选择界面语言（跟随系统、简体中文、English）和主题（跟随系统、浅色、深色）。两项均在保存成功后立即生效，重启保留；标题栏的主题切换也会保存。偏好统一写入 `aicat.yaml` 的 `ui.language`（`system` / `zh-Hans` / `en`）与 `ui.theme`（`system` / `light` / `dark`），缺省都跟随系统。

在“扩展”的内置列表选择“流量”，即可关闭或重新开启流量功能，并进入流量页面或代理设置。关闭后导航入口消失并停止采集新请求，已有记录继续保留至淘汰、清空或退出，代理及只读 API 继续运行。开关保存后即时生效，重启仍保留；省略 `features.traffic` 时缺省开启。功能关闭时启动进入流量扩展详情，扩展与设置入口始终保留。同一会话重新开启功能时，先前的暂停状态也会保留。代理属于基础能力，不提供关闭开关；只有显式配置使用代理的客户端流量才会经过它。

HTTPS 默认 CONNECT 透传。选中 CONNECT 记录后，详情展示连接请求头和加密提示；点击“解密此域名”后，把精确域名/IP 保存到 `interceptHosts`，对后续新建连接即时生效。已有隧道继续透传，历史 CONNECT 记录保持未解密状态。手动编辑配置后重启生效。客户端信任数据目录的 `ca/mitmproxy-ca-cert.pem` 后即可检查这些 HTTPS 请求；例如 curl 通过 `--cacert` 指定该文件。私钥留在权限为 0700 的 CA 目录，私钥文件权限为 0600。

“安装证书”按需生成缺失的根证书，并直接调用 macOS 当前用户授权，不添加应用内重复确认；页面显示等待授权、验证、成功或错误。按当前证书指纹和实际 SSL 验证检测信任，取消、失败或只导入未受信任时均可重试。证书详情显示真实名称、有效期与 SHA-256。新 CA 命名为 `aicat`，既有 CA 不更换；私钥始终留在本机，不导入钥匙串。

系统代理在代理设置页显式开启，默认关闭，需先完成证书信任。开启前保存当前网络设置，关闭与正常退出时恢复；恢复失败保留应用和代理监听，用户可重试。异常退出后的下次启动先恢复旧会话，再按 `systemProxy.enabled` 接管；外部修改不会被覆盖。恢复日志位于 aicat Home 的 `state/system-proxy.json`，属于撤销数据，不是第二份行为配置。

API 与 native UI 读取同一个进程内 traffic 服务：

| 路径 | 内容 |
| --- | --- |
| `GET /api/traffic` | 摘要；支持 `limit`、`before`、`host`、`method`、`state`、`q` |
| `GET /api/traffic/{id}` | 四个观测位置的 headers 和 body 引用 |
| `GET /api/bodies/{id}` | 已保留的原始字节前缀；`X-Aicat-Body-State` 表示采集状态 |

`before` 是上页 `nextPage` 给出的请求 ID 边界，不是事件 cursor。请求 ID 从 1 自增；清空不重置，应用重启后从 1 开始。body 与记录均有容量上限，超过容量会截断或淘汰，实际代理转发继续。

## 规则、插件与 MCP

规则负责匹配、排序、启停与插件绑定；请求和响应改写由单文件 JS/TS 插件完成。通过 MCP 让 agent 编写和管理，无需重新编译 aicat；规则或脚本保存后重启生效，工具会分别返回保存版本、运行版本与 `restartRequired`。全局改写默认关闭，流量采集开关不影响改写。

MCP 地址为 `http://127.0.0.1:9981/mcp`（Streamable HTTP）。客户端的 Authorization header 使用 `Bearer <token>`；token 从当前 aicat Home 的 `mcp-token` 文件读取，权限为 0600，不写入日志。该接口拒绝浏览器 Origin，并沿用入口的 authority 检查。

agent 先调用 `plugins_guide` 读取 [SDK 说明](internal/mcpserver/guide.md)，再按以下顺序工作：

1. `rewrite_status`、`rules_list`、`plugins_list` 了解现状；编辑时携带 `expectedRevision`，冲突后重新读取。
2. `plugins_validate` / `plugins_put` 校验并保存插件，`rules_put` 保存目标范围、插件绑定与 settings。
3. `rules_test` 用合成输入离线试运行单个阶段，不连接上游；`rules_reorder`、`rules_set_enabled` 和 `rewrite_set_enabled` 管理顺序与开关。
4. 重启后用 `traffic_list`、`traffic_get`、`traffic_body` 核对实际修改与 `executions`；`rewrite_logs` 在采集关闭时仍提供有界诊断。

插件位于 `plugins/<id>/plugin.json` 与 `index.ts` 或 `index.js`。支持四个钩子、多值 headers、完整 body 改写、自定义响应、显式 context 与 async。每次调用独立 runtime；异常、超时或非法输出仅撤销当前调用，前序成功修改保留，继续转发并记录错误。自定义响应不访问上游，仍经过响应钩子。大 body 和 SSE 持续转发，完整 body 钩子记录 skipped。默认每阶段 200ms、并发 8、runtime 32 MiB、解压后 body 1 MiB；v1 不提供 import、npm、文件、进程或网络 API。

## 验证

```sh
make verify
make e2e
```

上述两项用于整轮开发结束后的统一验收；日常功能和 bug 修复只执行受影响流程的定向验证。`make verify` 不包含 E2E。完整验证、无 `make` 时的备用流程和隔离要求见 [开发约定](docs/development.md)。

E2E 启动本地 HTTP/TLS 站点和真正的 native 应用，通过 mygo 官方 automation socket 点击真实窗口、读取详情并截图。使用独立测试 CA 和 `.aicat/`，只给测试客户端与测试进程配置 CA 信任。默认验证 9981；被其他进程占用时明确失败，可通过 `python3 e2e/prototype.py --binary bin/aicat-e2e --port 19981` 改用测试端口。

工件位于 `build/prototype-e2e-*/`，包括 `report.json`、app 日志、流量快照和 native 截图。该目录及测试私钥都不提交。原型 E2E 不依赖外部站点。

## 原型边界

规则与 moejs 插件及 MCP 管理已接入后端；规则/插件管理界面留待 UI 设计收敛。私云登录、TUN/Tailscale、重放和导出尚未接入。

go-mitmproxy v1.9.4 的 CONNECT 转发在客户端正常断开时可能等待上游半连接。采集 addon 以客户端断开结束 CONNECT 记录；库内部的上游等待仍可能持续到对端关闭或进程退出。网关会跟踪并在停止期限内关闭客户端长连接；进程退出释放其余 socket。原型不承诺在同一进程内反复重建 Proxy 后无内部 goroutine 残留，此处需要后续上游修复。

详见 [一期设计](docs/phase-one-design.md) 与 [gateway/traffic 接口](docs/gateway-traffic-design.md)。`docs/adr/` 中旧产品决策作为历史保留，以当前从零重写设计和实现为准。

证书管理位于「设置 → 代理 → 管理证书」，支持查看、导出公开证书、打开钥匙串和删除。删除统一撤销当前用户信任并清理本地身份，现有连接会断开，后续 HTTPS 保持直通；失败可重试，重启不会自动重建。再次使用时点击「安装证书」，应用会生成缺失的根证书并完成安装与信任授权；其他设备上的旧副本需自行移除。
