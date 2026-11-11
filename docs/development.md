# 开发约定

## 工程与构建

本仓库默认在主仓库目录开发；没有用户明确要求，不创建额外 worktree。

根 `go.mod`、`cmd/`、`internal/`、`ui/` 和 `assets/` 是主工程。桌面使用 MyGo + 系统 WebView；CLI 运行相同的 Go 服务。

| 目录 | 职责 |
| --- | --- |
| `ui/` | 框架无关的 HTML、CSS、JS、翻译及 Go 资源嵌入入口；不引入前端运行时或构建器 |
| `internal/uihost/` | Go 界面接口、流量摘要合并和有界详情读取，不依赖 MyGo |
| `internal/app/` | 服务装配、界面命令、MyGo 窗口/托盘/剪贴板/系统对话框 |
| `docs/design/aicat-shell.html` | 本次迁移核对的 HTML 设计稿快照；包含尚未交付的产品方向，不代表所有稿内功能都已实现 |

`ui/design.css` 直接提取设计稿样式；`ui/layout.css` 承载桌面尺寸和真实数据适配，`ui/style.css` 提供动态列表等基础布局。页面不加载外部 CDN。变更设计稿时同步相应组件样式并复核浅深色截图。

需要 Go 1.27.3+，本地构建面向 macOS：

| 命令 | 产物与行为 |
| --- | --- |
| `make build` | WebView 桌面入口 `bin/aicat`，不含自动化插件和 HTTP 预览 |
| `make cli` | `-tags nogui` 的 `bin/aicat-cli` |
| `make app` | 未签名的 `build/native/aicat.app`；目录名沿用打包路径，不表示 Native UI |
| `make verify` | 格式、vet、已有 Go 检查、release 构建、app 打包及 CLI smoke |
| `make e2e` | race 构建 + WebView/浏览器、HTTP/TLS、规则/插件/MCP 完整验收 |

前端资源由 `ui/embed.go` 嵌入。这是 Go embed 必须相对当前包取文件的薄资源入口，页面逻辑不依赖 Go 或 MyGo。更换桌面框架时复用页面和 `uihost` 契约，替换 `internal/app` 中的平台适配即可。

## 验证流程

日常功能实现和 bug 修复只验证受影响的流程。整轮开发结束、准备统一验收，或用户明确要求全量回归时，再运行完整检查：

```sh
npm --prefix e2e ci
make verify
make e2e
```

浏览器验收使用 Playwright 1.58.2。macOS 默认使用 `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`，也可通过 `AICAT_CHROMIUM` 指向已有浏览器。依赖位于别处时，`AICAT_PLAYWRIGHT` 可指定已安装的 `playwright` 包路径，无须重复安装。全量验收不下载浏览器。

若 `make` 不可用，使用 Makefile 的相同 tags、`-trimpath` 和 release `-ldflags`，执行 `gofmt`、`go vet ./...`、`go test ./...`、桌面/CLI 构建、打包及 CLI `--version`、`serve --help`。不在实现后补写单元测试；有必要的隔离测试先列出失败方式。

定向入口：

```sh
# 构建测试运行体；OS 操作仍通过测试适配器隔离
 go build -race -tags e2e -o bin/aicat-e2e ./cmd/aicat
 go build -race -tags nogui,e2e -o bin/aicat-rules-e2e ./cmd/aicat

# 只运行某组受影响的验收
python3 e2e/prototype.py --only desktop
python3 e2e/prototype.py --only transport
python3 e2e/prototype.py --only rules
```

`e2e/prototype.py` 是完整验收编排入口，未指定 `--only` 时三组必须全部通过。`--output` 指定工件根目录，默认 `build/full-e2e-*/`。旧 Native 控件树用例由当前 WebView 用例替代，不能继续作为桌面验收入口。

| 组 | 实际验证 |
| --- | --- |
| desktop | 正式桌面入口与真实 WKWebView；同一嵌入页面在 Chromium 中使用键盘、点击和滚轮；有界列表、搜索、Header/Raw/复制、cURL、域名树、历史、扩展/更多/固定、偏好、监听与配置失败、证书与系统代理、隐藏/恢复及退出/重启 |
| design | 与仓库内 HTML 稿比较标题栏、导航、侧栏、工具栏、表格、详情栏、复制按钮和 Raw 行号尺寸；比较浅深色强调色，保存稿件与实际页面截图 |
| transport | CLI 的 loopback、通配、IPv6 HTTP/TLS MITM，以及 CA 跨进程重启保留 |
| rules | 规则/插件管理、MCP 认证与 schema、四阶段 HTTP/HTTPS 改写、前后快照、失败回滚、时限/内存/并发、大 body/SSE、自定义响应、重启生效及采集独立性 |

工件包括总报告、子报告、日志、源码与二进制 hash、依赖信息、配置和实际截图。画面验收需要 macOS 解锁；锁屏时不得把缓存截图或未运行用例报告为通过。Chromium 的输入测试与 WKWebView 的 DOM/原生壳验证分别记录，不能把前者称作 WKWebView 指针事件覆盖。

## 隔离与权限

smoke 和 E2E 必须使用 `--home <隔离目录>`，不写真实 `~/.aicat/`，不修改真实 Keychain 或 system proxy。测试只替换 OS 边界和保存对话框；配置、证书文件、HTTP/TLS、窗口、托盘、剪贴板与服务保持真实。

正式界面使用 `desktopapi://localhost` 私有协议调用 Go handler。默认构建不开 HTTP 管理预览，不把桌面操作暴露到 9981；9981 的 API/MCP 继续执行原有访问限制。`e2e` tag 下才允许通过 `AICAT_E2E_PREVIEW` 创建随机 loopback 预览，URL 文件为 0600，接口要求随机 token，拒绝跨源 Origin。

安装证书仍由用户点击后直接进入 macOS 授权。删除证书和启用系统代理保留相应确认流程；退出恢复失败则保留网关运行并显示恢复错误，供用户重试。

## 本地构建与已安装应用

本地 app 构建不签名，`make verify` 不包含签名步骤，也不覆盖已安装应用。不得将本地未签名产物直接覆盖安装。

更新已获用户授权的 macOS 安装时：

1. 确认目标机现有 app 的签名证书及 designated requirement。
2. 沿用目标机原有证书签署完整新 app。
3. 在覆盖前核对新旧包 designated requirement 一致，保持系统授权身份。
4. 只有核对通过才覆盖安装；缺少原证书或无法保持身份时，说明阻塞，不以未签名或不同身份的包替代。

## 历史可行性实验

2026-11-11 迁移前的流量页原型已在 WKWebView 与 Chrome 跑通；其主进程 RSS 不含已确认归属的 WebKit XPC 内存，不能用来证明整应用更省内存。当时相同 release 参数下，Native `.app` 约 22.08 MiB、ZIP 8.88 MiB，局部 WebView 原型 `.app` 约 18.54 MiB、ZIP 7.38 MiB；后者未完全对齐功能，不代表本次完整迁移后的最终体积。旧工件 `build/webview-probe-*/`、`build/webview-size/` 仅保留本机参考。原型入口已由正式桌面实现替代。
