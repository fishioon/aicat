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

需要 Go 1.27.3+、Node.js 22.12+、npm 及 Bun 1.4.4；`make app` 还需要带 `actool` 的 Xcode 26+。构建时用 AICAT_BUILD_BUN 或构建环境中的 bun 准备固定运行时；成品只执行应用内 Bun，不查系统 PATH。MyGo Go 模块和 CLI tool 固定为同一提交；UI 依赖由 `ui/package-lock.json` 锁定。TypeScript 使用与当前 vue-tsc 兼容的 5.9 系列。

| 命令 | 产物与行为 |
| --- | --- |
| `make bindings` | `go tool mygo generate`，生成 Go 服务对应的 TypeScript 类型与方法 |
| `make frontend` | 安装锁定依赖、生成接口、严格类型检查、构建 `ui/dist/` |
| `make build` | 生产桌面入口 `bin/aicat`，不含自动化插件或 HTTP 预览 |
| `make cli` | `-tags nogui` 的 `bin/aicat-cli`，无需前端依赖 |
| `make app` | 未签名的 `build/desktop/aicat.app` |
| `make app-icon` | 从 `build/darwin/aicat.icon` 生成新版 `Assets.car` 与兼容用 `aicat.icns` |
| `make run` | 构建前端、内置渠道资源和 bin/aicat-dev 后运行开发桌面入口 |
| `make tray-icons` | 从 `assets/logo.svg` 生成原始卷尾、30% 卷曲端点及统一画布的 25 帧 PNG 图集；原生托盘以模板图着色 |
| `make verify` | 格式、类型检查、vet、已有 Go 检查、release 构建、app 打包、CLI smoke 及构建验收 |
| `make e2e-build` | 生成独立验收前端，构建带 race 的桌面与 CLI 验收入口 |
| `make e2e` | 构建后运行完整后台验收：无头浏览器、HTTP/TLS、MCP、扩展生命周期与消息 fixture，不操作真实桌面或账号 |
| `make design-shots` | 渲染 `aicat-redesign.html` 各场景的浅深色截图到 `build/design-shots/`，`SCENES="overview traffic"` 只渲染指定场景；用于标注评审 |

`runtime/channel-host/` 保存宿主及共享 SDK shim 源文件，prepare.mjs 在构建阶段复制源码和 Bun 1.4.4，无需安装宿主运行依赖；同时用 Python 3.9+ 和 clang 从锁定源码构建原版 SQLite3 Multiple Ciphers 2.7.0，构建缓存位于 build/sqlite3mc-stock/。首次构建需下载归档，可用 AICAT_SQLITE3MC_SOURCE 指定同 SHA-256 的本地归档；成品无需 Python、编译器或网络下载。`make channel-check` 检查通用宿主与共享适配层的 JavaScript 语法。NPM 扩展的源码、类型依赖、打包和专属 E2E 在独立仓库维护，主工程构建不访问这些仓库，也不把扩展归档放入成品。产物在 bin/channel-host/；make app 将其放入 Contents/Resources/channel-host/。宿主修改后重新构建。官方渠道插件由用户点击安装时用应用内 Bun 下载，禁用安装脚本；用户启动或启用时不安装依赖。

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

Playwright 使用锁定版本和已安装的 Chrome，不下载浏览器。macOS 默认浏览器路径为 `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`；可用 `AICAT_CHROMIUM` 改写，`AICAT_PLAYWRIGHT` 可指定已有 playwright 包路径。

定向入口：

```sh
make e2e-build
# 扩展包安装、配置合并、手动更新、卸载与中断恢复（后台）
node e2e/extension-lifecycle.cjs bin/aicat-e2e build/extension-lifecycle-e2e
# 统一来源安装、版本、取消、认证拒绝与原子更新（后台）
node e2e/extension-source.cjs bin/aicat-e2e build/extension-source-e2e
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
# 仅验证外壳：主页、流量列表状态/类型、详情视图、键盘、隧道、默认导航、扩展启停、解密域名通配符、历史，截图在 build/redesign-e2e/
node e2e/redesign.cjs bin/aicat-e2e build/redesign-e2e
# 仅验证接入供应商：密钥只进 0600 的 providers.json、带密钥转发与流式、流量记录不含凭据、移除后失效
node e2e/provider.cjs bin/aicat-e2e build/provider-e2e
# 仅验证 Codex 扩展：证书提示与启停、模型列表同步、models.dev 补全、直连与解密两个入口的分流、426、流量记录不含密钥
node e2e/codex.cjs bin/aicat-e2e build/codex-e2e

# 仅验证 Room 管理页的列表、搜索与设置保存，输出报告、日志和截图
node e2e/messages.cjs bin/aicat-e2e build/messages-<run>
# 仅验证企业微信 BOT 的配置、私聊收发、Room 确认和重启恢复
node e2e/wecom.cjs bin/aicat-e2e
# 仅验证 Codex 私聊任务、会话隔离、显式回复及重启不恢复任务（协议进程替身）
node e2e/wecom.cjs bin/aicat-e2e build/wecom-agent-e2e agent
# 仅验证图标在 WebView/浏览器、浅深色和不同宽度下的居中与边界
node e2e/desktop.cjs bin/aicat-e2e build/icons-e2e icons
```

只经浏览器预览驱动的场景（`tray.cjs`、`traffic-default.cjs`、`tunnel.cjs`、`redesign.cjs`、`provider.cjs`、`codex.cjs`）以 `AICAT_E2E_BACKGROUND=1` 启动验收二进制：原生窗口保持隐藏、不抢焦点、不出现 Dock 图标，可在后台反复运行。该开关只存在于 `e2e` tag 构建。采集按钮的 `traffic-default.cjs` 不连接原生自动化，保留浏览器截图、日志与报告；原 `desktop.cjs ... traffic-default` 命令也转交此后台入口。`desktop.cjs` 的其他场景及 `prototype.py --only desktop` 要驱动并断言真实 WKWebView 窗口的显示/隐藏，仍会弹出窗口。

统一后台验收包含首页用量、供应商保存与模型状态、证书恢复、跨目标 HTTP、API/MCP 控制流量隔离、正文/事件流、详情重试、等待状态、快捷键和默认导航。只运行指定场景时使用：

```sh
python3 e2e/prototype.py --background --scenarios http-origins control-traffic agent-traffic --output build/query-<run>
```

场景之间恢复运行前的剪贴板内容；报告保留实际命令与覆盖范围。原生窗口、macOS 授权及真实消息送达仍需单独验收。

NPM 扩展的专属验收在各自仓库执行：企业微信客户端见 [aicat-wecom-local](https://github.com/fishioon/aicat-wecom-local)。在对应仓库运行 `npm run e2e -- --aicat ../aicat`，以实际外部 NPM 包和本机 registry 验证完整接入链路，不需要发布包或复制源码到主仓库。

`make e2e` 使用 `e2e/prototype.py --background` 汇总后台场景，包含品牌语言、标题栏导航、托盘资源及 WebSocket 补丁验证，保留每组报告、日志、截图、运行命令、源码和二进制 hash；源码摘要包含依赖补丁与构建入口，失败仍执行其余组并以非零状态退出。原生窗口、WKWebView 交互、真实授权和真实消息送达不在后台覆盖内。

`e2e/prototype.py` 不带 `--background` 或 `--only` 时会操作真实桌面，须事先取得明确同意；三组必须全部通过。`--output` 指定工件根目录，默认 `build/full-e2e-*/`。

| 组 | 实际验证 |
| --- | --- |
| desktop | 真实 MyGo WKWebView/Channel 与原生生命周期；同一前端在 Chromium 中的键盘/点击/滚轮；有界列表、搜索、Header/Raw/复制、cURL、域名树、历史、扩展/默认导航、偏好、监听与配置失败、证书与系统代理、隐藏/恢复/退出/重启 |
| design（desktop 内） | 与 `aicat-redesign.html` 比较标题栏、导航栏、侧栏、表格行、详情摘要、窗格标题、复制按钮和状态栏尺寸；比较浅深色色板变量与导航当前项颜色，保存稿件和实际截图 |
| transport | loopback、通配、IPv6 HTTP/TLS MITM 与 CA 跨进程重启保留 |
| mcp | MCP 认证与 schema、已停用脚本工具不可用、正文透明转发与有界采集、SSE、旧改写配置显式报错 |

工件含报告、日志、源码与二进制 hash、依赖、配置和截图。画面验收需要 macOS 解锁；锁屏时不能把缓存截图或未运行用例报告为通过。Chromium 输入与 WKWebView DOM/原生壳覆盖分别记录，不把前者称为 WKWebView 指针事件覆盖。

## 隔离与权限

smoke 和 E2E 必须使用隔离 `--home`，不写真实 `~/.aicat/`，不修改真实 Keychain 或 system proxy。测试只替换 OS 边界和保存对话框，其他服务、配置、证书文件、HTTP/TLS、窗口、托盘和剪贴板保持真实。

正式桌面通过 MyGo 内建绑定通信，不增加 HTTP 管理 listener。9981 的 API/MCP 继续执行原有访问限制。仅 `e2e` tag 编译随机 loopback 浏览器预览；它通过反射调用同一个 Desktop 服务，桥接相同的摘要和状态通知。URL 文件为 0600，RPC/事件要求随机 token 并拒绝跨源 Origin。预览 JavaScript 仅进入 `ui/dist-e2e/`；正式 `ui/dist/` 与发布二进制均不包含此适配。

安装证书仍由用户点击后进入 macOS 授权；删除证书和启用系统代理保留确认。退出恢复失败则保留网关并显示错误供重试。

## 本地构建与已安装应用

`make app` 生成待签名应用，`make verify` 不包含证书签名或安装，不覆盖已安装应用。Go 的 macOS arm64 可执行文件可能带有编译器生成的 ad-hoc 临时签名；这种本机构建可以启动，但不代表整个 `.app` 的资源签名完整，也不提供跨版本固定的证书身份。不得将待签名产物直接覆盖安装。

Make 使用 `scripts/proxy-go.py` 准备锁定版本的临时 WebSocket 补丁，`go.mod` 与模块缓存不变。直接编译、vet 或生成绑定时同样使用 `python3 scripts/proxy-go.py <Go 参数>`，详见[补丁说明](../patches/README.md)。

### 固定本机签名身份

本机安装使用下列固定身份，后续更新直接沿用，不重新生成或替换证书。用户已授权以此身份更新本机 aicat，无需每次重新确认签名身份；macOS 钥匙串要求的密码或授权仍由用户在系统弹窗中完成。

| 项目 | 固定值 |
| --- | --- |
| 证书名称 | `aicat Local Code Signing` |
| 证书 SHA-1 / codesign identity | `57BF8FA4BB71891C5F9E5655112190280F97E8B7` |
| 私钥与证书所在钥匙串 | `~/Library/Keychains/login.keychain-db` |
| 公开证书副本 | `~/Library/Application Support/aicat/CodeSigning/aicat-local-codesign.pem` |
| Bundle identifier | `ai.netcat.aicat` |
| 安装位置 | `~/Applications/aicat.app` |

签名身份按节点固定，不把开发机证书替换到其他节点。`mini` 的现有安装位于 `/Applications/aicat.app`，沿用它原有的同名证书，SHA-1 为 `4AD9591BF05FD28CABB7887EC5E7981DFF6E366C`；其 requirement 的 leaf 指纹必须保持 `4ad9591bf05fd28cabb7887ec5e7981dff6e366c`。该节点的证书与私钥在用户 `fish` 的 login 钥匙串中。通过 root SSH 检查或签名时，用 `launchctl asuser 501 sudo -H -u fish` 在该用户的 GUI 会话中执行 `security` 和 `codesign`，并显式指定 `/Users/fish/Library/Keychains/login.keychain-db`；root SSH 域中的“无有效身份”不能作为证书缺失的结论。

固定的 designated requirement 为：

```text
designated => identifier "ai.netcat.aicat" and certificate leaf = H"57bf8fa4bb71891c5f9e5655112190280f97e8b7"
```

证书仅在当前用户域按 `codeSign` 策略信任，用于本机自签名，不是 Apple Developer ID，也没有公证。公开 PEM 文件不含私钥，单独保留它不能恢复签名能力。不要把私钥或钥匙串凭据写入仓库、日志或验收工件。缺少此证书或私钥、证书失效、已安装应用的身份不匹配时，停止覆盖并说明阻塞，不自动创建新身份，不改用 ad-hoc 签名。

### 签名与替换安装

1. 完成本轮构建和验收，将 `build/desktop/aicat.app` 复制到本次独立的 `build/install-<run>/aicat.app`。保留待签名原产物和旧应用备份，不重新运行整套 E2E 来代替安装校验。
2. 使用 `security find-identity -v -p codesigning` 确认固定 SHA-1 的身份可用。对已安装应用执行 `codesign --verify --deep --strict` 和 `codesign -d -r-`，核对其完整性和固定 requirement；校验失败则停止覆盖。
3. 从内到外签名 SQLite3MC 动态库、Bun 和整个应用。以下命令中的 `signing_app` 必须指向本次待安装副本：

```sh
signing_app="$PWD/build/install-<run>/aicat.app"
signing_identity=57BF8FA4BB71891C5F9E5655112190280F97E8B7
codesign --force --sign "$signing_identity" --timestamp=none "$signing_app/Contents/Resources/channel-host/sqlite3mc/libsqlite3mc.dylib"
codesign --force --sign "$signing_identity" --timestamp=none "$signing_app/Contents/Resources/channel-host/bun"
codesign --force --sign "$signing_identity" --timestamp=none --identifier ai.netcat.aicat "$signing_app"
codesign --verify --deep --strict --verbose=2 "$signing_app"
codesign -d -r- "$signing_app"
```

4. 必须与已安装应用逐字比较 designated requirement，不能只比较证书名称。使用 `codesign -d -r-` 输出 requirement。钥匙串弹窗由用户授权 codesign 使用固定私钥，可选择“始终允许”；不索要聊天中的密码，也不放宽其他应用对私钥的访问。
5. 从应用菜单正常退出当前实例，确认进程已结束，让退出流程恢复系统代理并关闭业务服务。桌面入口不使用 `kill -TERM` 代替正常退出。备份 `~/Applications/aicat.app`，用 `ditto` 将签名副本复制到同级临时路径，验证签名和 requirement 后再通过重命名替换；切换失败恢复旧应用。不删除或覆盖 `~/.aicat/` 中的配置、凭据和业务数据。
6. 对最终安装路径再次执行完整签名验证并比较 requirement，然后启动 `~/Applications/aicat.app`。核对实际进程路径、网关监听、原生主页就绪和原有配置可见，不主动发送消息或触发系统授权验收。异常时保留日志和备份，正常退出失败实例后回退旧包。
7. 在本次 `build/install-<run>/` 保存安装报告、提交 SHA、证书 SHA-1、requirement、签名前后与最终安装校验、运行路径和监听结果。安装备份和验收工件不入库。

## 参考

- [MyGo frontend 与手动 embed](https://mygo.egoist.dev/docs/frontend)
- [绑定、生成客户端、事件和 Channel](https://mygo.egoist.dev/docs/bindings)
- [Vue 的 TypeScript 工程配置](https://vuejs.org/guide/typescript/overview.html)

构建验收只记录成品体积。评估整应用内存时，需要在一致场景下汇总主进程与归属的 WebView 子进程，不能用前端资源大小替代。
