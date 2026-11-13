# 开发约定

## 工程与构建

本仓库默认在主仓库目录开发；没有用户明确要求，不创建额外 worktree。

根 `go.mod`、`cmd/`、`internal/`、`ui/` 和 `assets/` 是主工程。桌面使用 MyGo + 系统 WebView，前端采用 Vue 3、TypeScript 和 Vite。CLI 运行相同的 Go 业务服务。

| 目录 | 职责 |
| --- | --- |
| `ui/` | 独立的前端工程、package-lock、Vite/TypeScript 配置及薄 Go embed 入口 |
| `ui/src/components/` | Vue 单文件组件，按导航、设置、扩展、流量列表与详情拆分 |
| `ui/src/state/` | 页面状态、历史、筛选、虚拟列表数据与订阅生命周期 |
| `ui/src/host.ts` | 桌面通信适配，视图经此调用生成的服务客户端 |
| `ui/src/generated/` | MyGo 从 Go 服务及语言目录生成的代码，不手改 |
| `internal/app/` | 服务装配、typed Desktop 服务、MyGo 窗口/托盘/剪贴板/系统对话框 |
| `internal/provider/` | 供应商密钥存储（Home 下 0600 的 `providers.json`）、模型列表同步（`provider-models.json`）与带密钥的转发路由 |
| `internal/extension/` | 内置扩展的登记与启停；`internal/app/extensions.go` 是唯一的登记处 |
| `internal/codex/` | Codex 扩展：合并 Codex 模型列表、按模型分流 ChatGPT 与供应商、解密入口；`catalog/` 是 models.dev 目录的本地副本与查询 |
| `ui/src/extensions/` | 扩展页里各扩展自定义的详情与操作，按目录名（扩展 ID）自动加载，不需登记 |
| `internal/traffic/` 等业务包 | 业务模型与规则，不依赖 MyGo、Vue 或页面状态 |
| `docs/design/aicat-redesign.html` | HTML 设计稿，含尚未交付的产品方向（如用量图表）；不代表稿内全部功能已实现 |

`ui/src/app.css` 是唯一样式表，色板、字号、圆角和组件 class 取自设计稿（`--ink`、`--mark`、`--ok` 等变量与稿子同名），另含虚拟列表、对话框和分隔条。Vue 模板沿用稿子的 class，不引入改变视觉规范的组件库。纯图标按钮统一使用 `icon-btn` 居中，SVG 通过 `Icon.vue` 避免参与文字基线排版。页面不加载 CDN。变更设计稿时同步组件并复核浅深色截图。

需要 Go 1.27.3+、Node.js 22.12+、npm 及 Bun 1.4.4。构建时用 AICAT_BUILD_BUN 或构建环境中的 bun 准备固定运行时；成品只执行应用内 Bun，不查系统 PATH。MyGo Go 模块和 CLI tool 固定为同一提交；UI 依赖由 `ui/package-lock.json` 锁定。TypeScript 使用与当前 vue-tsc 兼容的 5.9 系列。

| 命令 | 产物与行为 |
| --- | --- |
| `make bindings` | `go tool mygo generate`，生成 Go 服务对应的 TypeScript 类型与方法 |
| `make frontend` | 安装锁定依赖、生成接口、严格类型检查、构建 `ui/dist/` |
| `make build` | 生产桌面入口 `bin/aicat`，不含自动化插件或 HTTP 预览 |
| `make cli` | `-tags nogui` 的 `bin/aicat-cli`，无需前端依赖 |
| `make app` | 未签名的 `build/desktop/aicat.app` |
| `make run` | 构建前端、内置渠道资源和 bin/aicat-dev 后运行开发桌面入口 |
| `make tray-icons` | 从 `assets/logo.svg` 生成原始卷尾、30% 卷曲端点及统一画布的 25 帧 PNG 图集；原生托盘以模板图着色 |
| `make verify` | 格式、类型检查、vet、已有 Go 检查、release 构建、app 打包、CLI smoke 及构建验收 |
| `make e2e-build` | 生成独立验收前端，构建带 race 的桌面与 CLI 验收入口 |
| `make e2e-headless` | 整轮开发收尾的全部后台功能场景、独立 Home 和逐场景报告；不含原生窗口、授权或真实平台消息 |
| `make e2e` | 构建后运行 WebView/浏览器、HTTP/TLS、规则/插件/MCP 完整验收 |
| `make design-shots` | 渲染 `aicat-redesign.html` 各场景的浅深色截图到 `build/design-shots/`，`SCENES="overview traffic"` 只渲染指定场景；用于标注评审 |

`runtime/channel-host/` 保存宿主及共享 SDK shim 源文件，prepare.mjs 在构建阶段复制源码和 Bun 1.4.4，无需安装宿主运行依赖；同时用 Python 3.9+ 和 clang 从锁定源码构建原版 SQLite3 Multiple Ciphers 2.7.0，构建缓存位于 build/sqlite3mc-stock/。首次构建需下载归档，可用 AICAT_SQLITE3MC_SOURCE 指定同 SHA-256 的本地归档；成品无需 Python、编译器或网络下载。客户端读取器位于 wecom-local/，TS 开发检查使用 runtime/channel-host/package-lock.json 锁定的类型依赖，`make channel-check` 执行严格类型检查；成品不包含这些开发依赖。产物在 bin/channel-host/；make app 将其放入 Contents/Resources/channel-host/。宿主修改后重新构建。官方渠道插件由用户点击安装时用应用内 Bun 下载，禁用安装脚本；用户启动或启用时不安装依赖。

开发时可在一个终端运行 `npm --prefix ui run dev`，另一个终端运行 `MYGO_DEV_URL=http://127.0.0.1:5173 make run`，或使用构建后的 `MYGO_DEV_URL=http://127.0.0.1:5173 bin/aicat-dev --home <隔离目录>`，在 WebView 中使用 Vite 热更新。首次先执行 `make bindings`。开发 URL 仅对开发二进制生效；生产链接标记优先于 `MYGO_ENV` 和 `MYGO_DEV_URL`。MyGo 配置也保留官方 CLI 的 dev/build 字段，CLI 打包会进行签名，应在明确准备分发时使用。

生产遵循 MyGo 官方的手动构建方式：Vite 输出静态资源，Go embed + `mygo.SetFrontend` 嵌入，链接 `-X github.com/egoist/mygo.production=1` 并使用 `mygo_noinspector`。保留手动打包是为了执行本仓库本地构建不签名的约束。`mygo_noinspector` 不能替代 production 链接标记。首次类型生成前 Make 创建忽略的临时 embed 输入，Vite 构建时清理；不把占位资源或前端源码打进成品。

## 界面接口

Go 的 `Desktop` 是前后端接口的唯一声明：具体方法和 DTO 经 `mygo.Bind` 暴露，`mygo generate` 输出类型客户端，不维护手写字符串命令或重复 DTO。`Desktop.WatchTraffic` 通过官方 `Channel<TrafficBatch>` 推送每 100ms 合并的摘要；正文只在 Raw 展开时读取。`DesktopChanged` 是 typed event，用于失效页面快照，托盘操作也走同一通知。页面没有定时状态轮询。

网关、配置及 OS 初始化在 `App.WhenReady` 后进行。类型生成只运行入口和绑定注册，不能读写 Home、启动 listener 或操作证书/系统代理。

组件和页面状态只引用 `host.ts` 的方法与生成的 DTO。未来迁移桌面框架时可复用 Vue 页面及 Go 业务包，替换 host 适配、生成器和 `internal/app` 中的平台装配；不是完全零成本。扩展自身 UI 的权限与隔离需要另行设计，不直接信任第三方 HTML。

## 验证流程

日常功能只验证受影响的流程。整轮开发结束、准备统一验收时运行：

```sh
npm --prefix e2e ci
make verify
make e2e
```

不在实现后补写单元测试；确需隔离测试先列出失败方式。构建验收 `e2e/build.py` 使用生产二进制在占用端口下生成类型，确认没有创建数据 Home，核对 DTO、参数约束、生产资源和包体积，工件输出到 `build/build-e2e-*/`。桌面验收还实际断言 production 模式及忽略开发 URL。

后台收尾入口为 `python3 e2e/acceptance.py --output build/headless-acceptance-<run>`，默认串行运行所有后台场景，以免剪贴板验证互相干扰；逐场景保存命令、PID、退出状态、日志及报告 hash，并生成 Feature map 对照清单。`--list` 列出场景，`--only <名称...>` 用于失败后的定向复验；客户端界面场景会自动准备独立夹具。输出目录必须全新。总报告通过仅证明所选后台范围，不代表原生窗口、授权或真实平台消息已通过。

Playwright 使用锁定版本和已安装的 Chrome，不下载浏览器。macOS 默认浏览器路径为 `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`；可用 `AICAT_CHROMIUM` 改写，`AICAT_PLAYWRIGHT` 可指定已有 playwright 包路径。

定向入口：

```sh
make e2e-build
python3 e2e/prototype.py --only desktop
python3 e2e/prototype.py --only transport
python3 e2e/prototype.py --only mcp
# 仅验证托盘菜单模型、采集对号、系统代理两态与显式刷新（后台，不弹原生菜单）
node e2e/tray.cjs bin/aicat-e2e build/tray-e2e
# 仅验证 SVG 生成、30% 端点、等长/等粗和固定画布，并保存浅深色资源对照图
node e2e/tray-assets.cjs build/tray-assets-e2e
# 仅验证系统代理直接开关、正常/崩溃退出恢复、失败重试和外部设置保护
node e2e/system-proxy.cjs bin/aicat-e2e build/system-proxy-e2e
# 仅验证流量入口默认可用、采集默认暂停、手动采集与启动配置
node e2e/traffic-default.cjs bin/aicat-e2e build/traffic-default-e2e
# 仅验证 CONNECT 双栏、响应侧解密提示与域名启用
node e2e/tunnel.cjs bin/aicat-e2e build/tunnel-e2e
# 仅验证流量侧边栏的顺序、中英文标签与 Domain 计数
node e2e/desktop.cjs bin/aicat-e2e build/sidebar-e2e sidebar
# 仅验证 App → 域名 → 路径树及跨应用隔离、搜索、折叠与历史
node e2e/desktop.cjs bin/aicat-e2e build/app-tree-e2e app-tree
# 仅验证滚动后首次选中、右键、历史及筛选重置滚动
node e2e/desktop.cjs bin/aicat-e2e build/list-selection-e2e list-selection
# 仅验证外壳：主页、流量列表状态/类型、详情视图、键盘、隧道、更多、扩展启停、解密域名通配符、历史，截图在 build/redesign-e2e/
node e2e/redesign.cjs bin/aicat-e2e build/redesign-e2e
# 仅验证接入供应商：密钥只进 0600 的 providers.json、带密钥转发与流式、流量记录不含凭据、移除后失效
node e2e/provider.cjs bin/aicat-e2e build/provider-e2e
# 仅验证 Codex 扩展：证书提示与启停、模型列表同步、models.dev 补全、直连与解密两个入口的分流、426、流量记录不含密钥
node e2e/codex.cjs bin/aicat-e2e build/codex-e2e

# 仅验证客户端本地 DB → Room、WAL 增量与崩溃恢复
make cli channel-check
python3 e2e/wecom-local.py
# 仅验证 MCP → 客户端脚本边界 → DB/WAL 自发消息确认、幂等与崩溃恢复
python3 e2e/wecom-local-send.py
# 使用前一步合成工件，后台验证客户端扩展入口、配置、启停与诊断
node e2e/wecom-local-ui.cjs bin/aicat-e2e build/wecom-local-e2e-<run>
# 仅验证企业微信 BOT 的配置、私聊收发、Room 确认和重启恢复
node e2e/wecom.cjs bin/aicat-e2e
# 仅验证 Codex 私聊任务、会话隔离、显式回复及重启/崩溃恢复（协议进程替身）
node e2e/wecom.cjs bin/aicat-e2e build/wecom-agent-e2e agent
# 仅验证图标在 WebView/浏览器、浅深色和不同宽度下的居中与边界
node e2e/desktop.cjs bin/aicat-e2e build/icons-e2e icons
```

只经浏览器预览驱动的场景（`tray.cjs`、`traffic-default.cjs`、`tunnel.cjs`、`redesign.cjs`、`provider.cjs`、`codex.cjs`）以 `AICAT_E2E_BACKGROUND=1` 启动验收二进制：原生窗口保持隐藏、不抢焦点、不出现 Dock 图标，可在后台反复运行。该开关只存在于 `e2e` tag 构建。采集按钮的 `traffic-default.cjs` 不连接原生自动化，保留浏览器截图、日志与报告；原 `desktop.cjs ... traffic-default` 命令也转交此后台入口。`desktop.cjs` 的其他场景及 `prototype.py --only desktop` 要驱动并断言真实 WKWebView 窗口的显示/隐藏，仍会弹出窗口。

`e2e/prototype.py` 不带 `--only` 时三组必须全部通过；`--output` 指定工件根目录，默认 `build/full-e2e-*/`。

| 组 | 实际验证 |
| --- | --- |
| desktop | 真实 MyGo WKWebView/Channel 与原生生命周期；同一前端在 Chromium 中的键盘/点击/滚轮；有界列表、搜索、Header/Raw/复制、cURL、域名树、历史、扩展/更多/固定、偏好、监听与配置失败、证书与系统代理、隐藏/恢复/退出/重启 |
| design（desktop 内） | 与 `aicat-redesign.html` 比较标题栏、导航栏、侧栏、表格行、详情摘要、窗格标题、复制按钮和状态栏尺寸；比较浅深色色板变量与导航当前项颜色，保存稿件和实际截图 |
| transport | loopback、通配、IPv6 HTTP/TLS MITM 与 CA 跨进程重启保留 |
| mcp | MCP 认证与 schema、已停用脚本工具不可用、正文透明转发与有界采集、SSE、旧改写配置显式报错 |

工件含报告、日志、源码与二进制 hash、依赖、配置和截图。画面验收需要 macOS 解锁；锁屏时不能把缓存截图或未运行用例报告为通过。Chromium 输入与 WKWebView DOM/原生壳覆盖分别记录，不把前者称为 WKWebView 指针事件覆盖。

## 隔离与权限

smoke 和 E2E 必须使用隔离 `--home`，不写真实 `~/.aicat/`，不修改真实 Keychain 或 system proxy。测试只替换 OS 边界和保存对话框，其他服务、配置、证书文件、HTTP/TLS、窗口、托盘和剪贴板保持真实。

正式桌面通过 MyGo 内建绑定通信，不增加 HTTP 管理 listener。9981 的 API/MCP 继续执行原有访问限制。仅 `e2e` tag 编译随机 loopback 浏览器预览；它通过反射调用同一个 Desktop 服务，桥接相同的摘要和状态通知。URL 文件为 0600，RPC/事件要求随机 token 并拒绝跨源 Origin。预览 JavaScript 仅进入 `ui/dist-e2e/`；正式 `ui/dist/` 与发布二进制均不包含此适配。

安装证书仍由用户点击后进入 macOS 授权；删除证书和启用系统代理保留确认。退出恢复失败则保留网关并显示错误供重试。

## 本地构建与已安装应用

本地 app 构建不签名，`make verify` 不包含签名或安装，不覆盖已安装应用。不得将本地未签名产物直接覆盖安装。

更新已获授权的 macOS 安装时，先确认现有签名证书及 designated requirement；沿用原证书签署新 app 并核对 requirement 一致后才覆盖。缺少原证书或无法保持身份时，说明阻塞，不换成未签名或其他身份的包。

## 参考

- [MyGo frontend 与手动 embed](https://mygo.egoist.dev/docs/frontend)
- [绑定、生成客户端、事件和 Channel](https://mygo.egoist.dev/docs/bindings)
- [Vue 的 TypeScript 工程配置](https://vuejs.org/guide/typescript/overview.html)

构建验收只记录成品体积。评估整应用内存时，需要在一致场景下汇总主进程与归属的 WebView 子进程，不能用前端资源大小替代。
