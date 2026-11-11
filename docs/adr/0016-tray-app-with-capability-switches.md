# aicat 采用常驻托盘应用加能力开关的产品形态

状态：accepted；取代 [0001](0001-cli-only-product.md) 中「CLI 是唯一原生控制面」和「不提供原生 GUI」的决策，
不修改 macOS system proxy 的决策仍然有效。

aicat 是 macOS 上的本机瑞士军刀：一个常驻托盘应用加一条 CLI 控制面，功能按能力合集组织，每个能力有独立
开关，默认只开最核心的本机中转。第一版只支持 Mac。托盘与窗口只承载开关、状态和登录，不提供独立的 Web UI。

技术方案与 magpie 一致：Go + Wails v3，界面在系统 WebView 中渲染；托盘进程就是长期运行的运行体，`aicat serve`
是同一份运行体在前台的入口（见 [0019](0019-tray-process-serves-aicat.md)）。桌面形态分两个表面：点菜单栏图标
弹出的 Panel 只放状态与开关，从 Panel 打开的 Main Window 放需要动手的能力。GUI 代码放 `internal/gui/`，与 CLI
共用同一个二进制：默认构建带托盘，`-tags nogui` 出纯 CLI。

## Why

能力合集需要一个常驻入口：纯 CLI 无法承载开关与登录状态，而托盘是 macOS 上长期运行工具最轻的原生入口。
选 Wails v3 加系统 WebView 而不是独立 Web UI，是因为 aicat 只服务本机桌面用户，不需要为无桌面服务器场景维护
一套 HTTP 控制面。默认只开核心中转、其余能力显式开启，是为了让 aicat 的默认行为不改变用户既有的工具与网络
配置。

## Consequences

- [0001](0001-cli-only-product.md) 的「CLI 为唯一原生控制面」与「需要图形界面时使用 agentgateway Web UI」作废；
  agentgateway Web UI 不再是 aicat 的图形界面。
- 托盘应用与 CLI 共享同一份 `~/.aicat/` 状态，不引入第二份配置，也不引入第二个长期进程入口。
- 每个能力必须有显式开关且默认关闭，只有本机中转默认开启；开关不由能力是否存在隐式决定。
- 第一版的能力集合包括：登录与私云 Provider 同步、内置网关、微信/企业微信连接器、Tailscale 管理器。代为安装
  第三方运行时暂不实现，后续单独讨论。
- aicat 默认允许通过标准 HTTP(S) 代理出网，具体配置形态在实现时确定。
- 引入 Wails 依赖意味着构建链需要 macOS WebView 与前端资源；CLI 仍可单独构建和运行。
