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
| `internal/provider/` | 供应商密钥存储（Home 下 0600 的 `providers.json`）与带密钥的转发路由 |
| `internal/traffic/` 等业务包 | 业务模型与规则，不依赖 MyGo、Vue 或页面状态 |
| `docs/design/aicat-redesign.html` | 当前 HTML 设计稿，含尚未交付的产品方向（用量图表、接入供应商）；不代表稿内全部功能已实现。`aicat-shell.html` 是旧稿 |

`ui/src/app.css` 是唯一样式表，色板、字号、圆角和组件 class 取自设计稿（`--ink`、`--mark`、`--ok` 等变量与稿子同名），另含虚拟列表、对话框和分隔条。Vue 模板沿用稿子的 class，不引入改变视觉规范的组件库。纯图标按钮统一使用 `icon-btn` 居中，SVG 通过 `Icon.vue` 避免参与文字基线排版。页面不加载 CDN。变更设计稿时同步组件并复核浅深色截图。

需要 Go 1.27.3+、Node.js 22.12+（本轮以 Node.js 26 验证）及 npm。MyGo Go 模块和 CLI tool 固定为同一提交；UI 依赖由 `ui/package-lock.json` 锁定。TypeScript 使用与当前 vue-tsc 兼容的 5.9 系列。

| 命令 | 产物与行为 |
| --- | --- |
| `make bindings` | `go tool mygo generate`，生成 Go 服务对应的 TypeScript 类型与方法 |
| `make frontend` | 安装锁定依赖、生成接口、严格类型检查、构建 `ui/dist/` |
| `make build` | 生产桌面入口 `bin/aicat`，不含自动化插件或 HTTP 预览 |
| `make cli` | `-tags nogui` 的 `bin/aicat-cli`，无需前端依赖 |
| `make app` | 未签名的 `build/desktop/aicat.app` |
| `make run` | 构建前端后运行开发桌面入口 |
| `make verify` | 格式、类型检查、vet、已有 Go 检查、release 构建、app 打包、CLI smoke 及构建验收 |
| `make e2e-build` | 生成独立验收前端，构建带 race 的桌面与 CLI 验收入口 |
| `make e2e` | 构建后运行 WebView/浏览器、HTTP/TLS、规则/插件/MCP 完整验收 |
| `make design-shots` | 渲染 `aicat-redesign.html` 各场景的浅深色截图到 `build/design-shots/`，`SCENES="overview traffic"` 只渲染指定场景；用于标注评审 |

开发时可在一个终端运行 `npm --prefix ui run dev`，另一个终端运行 `MYGO_DEV_URL=http://127.0.0.1:5173 go run ./cmd/aicat --home <隔离目录>`，在 WebView 中使用 Vite 热更新。首次先执行 `make bindings`。开发 URL 仅对开发二进制生效；生产链接标记优先于 `MYGO_ENV` 和 `MYGO_DEV_URL`。MyGo 配置也保留官方 CLI 的 dev/build 字段，CLI 打包会进行签名，应在明确准备分发时使用。

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

Playwright 使用锁定版本和已安装的 Chrome，不下载浏览器。macOS 默认浏览器路径为 `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`；可用 `AICAT_CHROMIUM` 改写，`AICAT_PLAYWRIGHT` 可指定已有 playwright 包路径。

定向入口：

```sh
make e2e-build
python3 e2e/prototype.py --only desktop
python3 e2e/prototype.py --only transport
python3 e2e/prototype.py --only rules
# 仅验证流量侧边栏的顺序、中英文标签与 Domain 计数
node e2e/desktop.cjs bin/aicat-e2e build/sidebar-e2e sidebar
# 仅验证 App → 域名 → 路径树及跨应用隔离、搜索、折叠与历史
node e2e/desktop.cjs bin/aicat-e2e build/app-tree-e2e app-tree
# 仅验证滚动后首次选中、右键、历史及筛选重置滚动
node e2e/desktop.cjs bin/aicat-e2e build/list-selection-e2e list-selection
# 仅验证新外壳：概览、流量列表状态/类型、详情视图、键盘、隧道、更多、扩展启停、解密域名通配符、历史，截图在 build/redesign-e2e/
node e2e/redesign.cjs bin/aicat-e2e build/redesign-e2e
# 仅验证接入供应商：密钥只进 0600 的 providers.json、带密钥转发与流式、流量记录不含凭据、移除后失效
node e2e/provider.cjs bin/aicat-e2e build/provider-e2e
# 仅验证图标在 WebView/浏览器、浅深色和不同宽度下的居中与边界
node e2e/desktop.cjs bin/aicat-e2e build/icons-e2e icons
```

只经浏览器预览驱动的场景（`redesign.cjs`、`provider.cjs`、`codex.cjs`）以 `AICAT_E2E_BACKGROUND=1` 启动验收二进制：原生窗口保持隐藏、不抢焦点、不出现 Dock 图标，可在后台反复运行。该开关只存在于 `e2e` tag 构建。`desktop.cjs` 及 `prototype.py --only desktop` 要驱动并断言真实 WKWebView 窗口的显示/隐藏，仍会弹出窗口。

`e2e/prototype.py` 不带 `--only` 时三组必须全部通过；`--output` 指定工件根目录，默认 `build/full-e2e-*/`。

| 组 | 实际验证 |
| --- | --- |
| desktop | 真实 MyGo WKWebView/Channel 与原生生命周期；同一前端在 Chromium 中的键盘/点击/滚轮；有界列表、搜索、Header/Raw/复制、cURL、域名树、历史、扩展/更多/固定、偏好、监听与配置失败、证书与系统代理、隐藏/恢复/退出/重启 |
| design（desktop 内） | 与 `aicat-redesign.html` 比较标题栏、导航栏、侧栏、表格行、详情摘要、窗格标题、复制按钮和状态栏尺寸；比较浅深色色板变量与导航当前项颜色，保存稿件和实际截图 |
| transport | loopback、通配、IPv6 HTTP/TLS MITM 与 CA 跨进程重启保留 |
| rules | 规则/插件、MCP 认证与 schema、四阶段 HTTP/HTTPS 改写、前后快照、失败回滚、时限/内存/并发、大 body/SSE、自定义响应、重启生效与采集独立性 |

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

历史内存实验的主进程 RSS 没有完整包含 WebKit XPC，不能证明整应用更省内存。当前构建验收记录成品体积；整应用内存需要在一致场景下汇总主进程与归属的 WebView 子进程，不能用前端资源大小替代。
