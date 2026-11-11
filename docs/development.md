# 开发约定

## 工程与构建

本仓库默认在主仓库目录开发；没有用户明确要求，不创建额外 worktree。

根 `go.mod`、`cmd/`、`internal/` 和 `assets/` 是 Go 主工程。当前使用 mygo Native UI，UI 直接调用进程内服务；`aicat serve` 是同一网关与采集服务的前台入口。

需要 Go 1.27.3+，本地构建面向 macOS。命令在仓库根目录运行：

| 命令 | 产物与行为 |
| --- | --- |
| `make build` | mygo native 的 `bin/aicat` |
| `make cli` | 使用 `-tags nogui` 的 `bin/aicat-cli` |
| `make app` | 未签名的 `build/native/aicat.app` |
| `make verify` | Go 格式检查、vet、已有 Go 测试、release 构建、app 打包及 CLI smoke，不包含 E2E |
| `make e2e` | 构建 E2E 运行体，验证真实网络与 native UI，输出验收工件 |

## 验证流程

日常功能实现和 bug 修复只验证受影响的流程，选择相关构建检查和针对该功能的 E2E。单个功能完成不自动触发全套 E2E；小修后仅重跑受影响的用例。若脚本仅支持全量，先提供可独立运行的相关场景，不因此扩大验证范围。

整轮开发结束、准备统一验收，或用户明确要求全量回归时，再执行：

```sh
make verify
make e2e
```

统一验收时两者均通过才算完成全量验证，不新增事后补写的单元测试。纯文档修改检查内容一致性、链接和 diff，无须构建或运行 E2E。

如果 `make` 不可用，按 [Makefile](../Makefile) 的构建参数执行相同检查：

- 用 `gofmt` 检查 Go 源码格式，执行 `go vet ./...` 和 `go test ./...`；运行已有测试不表示需要新增单元测试。
- 使用 Makefile 中的 tags、`-trimpath` 和 release `-ldflags` 构建 native 与 CLI 入口，并按 `app` 目标完成未签名打包。
- 执行 `bin/aicat-cli --version`、`bin/aicat-cli serve --help`。
- 使用 `go build -tags e2e -o bin/aicat-e2e ./cmd/aicat` 构建测试运行体，执行 `python3 e2e/prototype.py --binary bin/aicat-e2e`。创建所需的 `bin/`、`build/` 目录。

环境、签名、权限弹窗或外部依赖导致无法完成验证时，交付说明必须列出阻塞原因、已执行的检查和未验证的部分。

## 隔离与验收工件

smoke 和 E2E 不得修改 macOS system proxy 或真实用户 Keychain。涉及 CA、钥匙串或网关入口时，必须用 `--home <隔离目录>` 指定测试用的 aicat Home，不使用真实的 `~/.aicat/`；证书信任仅配置给测试客户端与测试进程，系统设置通过测试适配器验证。

原生 AppShell 对齐设计稿的定向验证：

```sh
go build -tags e2e -o bin/aicat-shell-e2e ./cmd/aicat
python3 e2e/shell.py --binary bin/aicat-shell-e2e
```

复制反馈的定向验证使用 `python3 e2e/shell.py --binary bin/aicat-shell-e2e --copy-feedback-only`，覆盖请求和响应的实际剪贴板内容、提示紧邻被点击按钮、中英文文案、连续点击刷新同一条提示、自动消失和焦点保持，以及切换请求后不显示旧提示，并保存浅深色原生截图与报告。

侧边栏去图标与已移除控件的定向验证使用 `python3 e2e/shell.py --binary bin/aicat-shell-e2e --sidebar-only`，检查分组、域名及固定项的文字直接跟随展开箭头，保留树筛选与右键固定，并确认代理页没有地址复制按钮。保存浅深色截图与报告。

URL 模糊搜索的定向验证使用 `python3 e2e/shell.py --binary bin/aicat-shell-e2e --search-only`，覆盖不连续字符、大小写、多个词全部命中、词序不限、空白输入、标点字面匹配、URL 字段边界、域名交集及原始列表顺序；逐字输入与实时新增请求保持焦点和输入标识。仅使用本地流量与隔离 Home，保留原生截图、报告及复跑命令。

应用分组的定向验证先执行 `go build -race -tags e2e -o bin/aicat-app-source-e2e ./cmd/aicat`，再执行 `python3 e2e/shell.py --binary bin/aicat-app-source-e2e --applications-only`。覆盖真实本机进程与 bundle helper 的归属、连接复用、归属不唯一、请求与 CONNECT 分开计数，以及原生侧栏搜索、筛选、历史与清空。保留原生截图、归属结果和报告。

监听设置的定向验证：先运行 `go build -race -tags e2e -o bin/aicat-listener-e2e ./cmd/aicat`，再运行 `python3 e2e/listener.py --binary bin/aicat-listener-e2e`。仅使用本地 HTTP 站点、隔离 Home 和 OS 适配器，覆盖纯 IP 下拉列表、端口校验、同端口通配切换、网卡/IPv6、占用端口、配置写入失败、系统代理写入和回退失败的恢复，以及重启保留与退出恢复。输出 report.json、地址列表、浅深色截图、日志、配置和源码/二进制 hash；`--output` 指定新目录。不会修改真实 Keychain/system proxy。

同一脚本的 `--transport-only` 只运行 CLI 在 loopback、通配地址、IPv6 上的 HTTP/TLS MITM 与重启 CA 保留检查，无须原生显示帧；报告标为 transport-only，不能替代原生编辑、切换和视觉验收。

Header / Raw 与请求菜单的定向验证使用 `python3 e2e/shell.py --binary bin/aicat-shell-e2e --inspector-menu-only`，覆盖 Header 默认视图、多值头部与复制、两侧视图独立保持、右键目标行、复制 URL、包含特殊字符的 cURL 实际执行、Pin/Unpin 同步与重启保留，以及 CONNECT/截断正文禁用 cURL。保留浅深色截图、生成的 cURL、配置与报告。

只验证布局度量、真实列表点击与 Raw、域名/路径筛选、输入焦点与光标、Pin、历史、暂停、导出、CONNECT 提示及重启恢复。使用本地 HTTP 站点、隔离 Home 与 OS 适配器；只有保存对话框替换为工件目录，导出按钮、筛选及序列化保持真实。输出 build/shell-e2e-*/report.json、浅深色截图、源码与二进制 hash、依赖信息、配置和日志。可用 --output 指定尚未使用的目录。Raw 行号栏与原生文字渲染的已知差异见 phase-one-design.md。

UI 验收按控件检查真实效果，不能只验证按钮存在：

| 范围 | 预期行为 | 验证入口 |
| --- | --- | --- |
| 标题栏 | 请求/域名/设置历史、实际折叠布局、暂停继续、可解析的 JSON 导出、主题切换 | shell.py |
| 侧边栏 | 域名与路径聚合、独立展开收起、固定/取消固定、清空与重启保留固定 | shell.py |
| 主面板 | URL 文本搜索与光标稳定、行点击、双栏 Raw、复制内容不含行号、清空复位筛选、空数组导出 | shell.py |
| 通用设置 | 中英文与三个主题即时应用、重启恢复、保存失败反馈、折叠后仍可进入 | prototype.py --preferences-only |
| 代理与证书 | 代理监听状态、系统代理授权与恢复、CA 安装/查看/导出/重检/删除、解密名单增删、CONNECT 启用解密 | prototype.py；certificate_install.py |
| 功能与托盘 | 流量启停与导航隐藏、重启保留、关闭窗口隐藏、托盘唤回/采集/设置/退出 | prototype.py |
| 扩展 | 搜索与分组、详情与社区占位、流量开关迁移、保存失败、历史、暂停与重启恢复、中英文和深浅色 | extensions.py |

私云未接入时显示静态说明，不保留禁用登录按钮。桌面显示帧验收需要 macOS 已解锁；锁屏时不得将截图缓存或未运行的原生用例报告为通过。开发结束统一验收使用完整 prototype.py，单点修复仍只重跑受影响用例。

扩展页与流量开关迁移的定向验证：

```sh
go build -tags e2e -o bin/aicat-extensions-e2e ./cmd/aicat
python3 e2e/extensions.py --binary bin/aicat-extensions-e2e --port 19994
```

仅运行扩展浏览和流量启停相关场景，使用隔离 Home、本地 HTTP 站点和 OS 适配器。输出 `build/extensions-e2e-*/report.json`、中英文深浅色原生截图、无障碍树、配置、源码与二进制 hash、依赖信息和复跑命令，不运行全套 E2E。

导航栏「更多」与固定的定向验证使用 `python3 e2e/extensions.py --binary bin/aicat-extensions-e2e --navigation-only --port 19995`。覆盖菜单打开与关闭、页面选择、固定与取消固定、保存失败、重启恢复和流量启停。保留原生截图、无障碍树和报告。

语言与主题的定向验证先构建 `go build -tags e2e -o bin/aicat-preferences-e2e ./cmd/aicat`，再运行 `python3 e2e/prototype.py --binary bin/aicat-preferences-e2e --port 19982 --preferences-only`。覆盖语言/主题即时应用、进程重启恢复、折叠侧边栏后的设置访问，以及配置保存失败；保留配置、native 截图与报告。

证书安装入口的定向验证使用 `python3 e2e/certificate_install.py --binary bin/aicat-e2e-race --port 19992`，只覆盖安装、取消、失败重试及删除后重新安装，不运行原型全套回归。运行体构建命令为 `go build -race -tags e2e -o bin/aicat-e2e-race ./cmd/aicat`。

规则/插件/MCP 的定向验证使用无 UI 的同一网关运行体：

```sh
go build -race -tags nogui,e2e -o bin/aicat-rules-e2e ./cmd/aicat
python3 e2e/rules.py --binary bin/aicat-rules-e2e --port 19993
```

仅覆盖规则与插件管理、MCP 认证与 schema、四阶段 HTTP/HTTPS 改写、前后快照、失败回滚、时限/内存/并发、大 body/SSE、自定义响应、重启生效及采集独立性。使用隔离 Home、本地站点与测试 OS 适配器，不操作真实 Keychain/system proxy，也不运行原型全套 UI E2E。工件位于 `build/rules-e2e-*/`，保留报告、复跑命令、源码/二进制 hash、依赖版本、日志及流量快照；`--output` 可指定一个尚未使用的目录。

当前 E2E 使用本地 HTTP/TLS 站点、测试 CA 和真实 native 应用，不依赖外部站点。默认检查 9981，端口占用时明确失败；可在测试运行体构建后改用隔离端口：

```sh
python3 e2e/prototype.py --binary bin/aicat-e2e --port 19981
```

工件位于 `build/prototype-e2e-*/`，包括 `report.json`、app 日志、流量快照和真实 native UI 截图。验收需保留复跑命令、代码与依赖版本及运行参数，核对报告中的断言结果；不能只凭截图判定功能通过。测试目录及私钥不提交仓库。

## 本地构建与已安装应用

本地 app 构建不签名，`make verify` 不包含签名步骤，也不覆盖已安装应用。不得将本地未签名产物直接覆盖安装。

更新已获用户授权的 macOS 安装时：

1. 确认目标机现有 app 的签名证书及 designated requirement。
2. 沿用目标机原有证书签署完整新 app。
3. 在覆盖前核对新旧包 designated requirement 一致，保持系统授权身份。
4. 只有核对通过才覆盖安装；缺少原证书或无法保持身份时，说明阻塞，不以未签名或不同身份的包替代。
