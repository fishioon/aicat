# aicat feature map

这份地图说明当前 aicat 有哪些用户可用能力、从哪里进入、如何操作，以及什么可观察结果能证明功能正常。先读索引，再按对应功能文件验证。

aicat 的品牌定位是「你的通用 AI 连接工具」，采用模块化设计，可扩展、可组合，灵感源自 netcat。它以用户节点上的常驻 Agent Gateway 运行：用户主要在自己的 Agent 中交互，桌面负责接入、配置、授权与诊断。当前已交付 HTTP/HTTPS 代理、流量检查、供应商接入、Codex、企业微信客户端与 BOT 扩展，以及供 Agent 使用的 API/MCP。地图以当前实现为准，产品目标见 [CONTEXT.md](../../CONTEXT.md)。

## 功能索引

| 功能 | 用户想完成的事 | 当前入口 |
| --- | --- | --- |
| [桌面与偏好](desktop.md) | 打开或隐藏窗口，找到设置，切换语言和主题 | 导航栏（主页、设置）、快捷键、托盘 |
| [扩展与导航](extensions.md) | 启停流量、Codex 与企业微信 BOT 扩展，管理功能与配置 | 扩展 → 已启用；流量 |
| [代理接入](proxy.md) | 让客户端经过 aicat，修改监听，接管或恢复系统代理 | 设置 → 代理；CLI `serve` |
| [证书与 HTTPS 解密](https.md) | 信任、查看、导出或删除证书，选择需要解密的域名 | 设置 → 代理；CONNECT 详情 |
| [查找流量](traffic-browse.md) | 实时查看、搜索、按域名/路径/应用筛选，固定域名 | 流量 → 侧边栏与请求列表 |
| [检查与导出请求](traffic-inspect.md) | 阅读请求/响应，复制 URL、cURL 或正文，导出结果 | 请求行、右键菜单、详情、标题栏 |
| [供应商接入](providers.md) | 让 aicat 保管供应商密钥并同步模型列表，Agent 经 aicat 调用模型 | 主页 → 接入供应商；同一监听地址的 `/v1/messages`、`/v1/chat/completions`、`/v1/responses`、`/v1/models` |
| [Codex 扩展](codex.md) | 在 Codex 的模型列表中使用供应商的模型，保留 ChatGPT 登录 | 扩展 → 已安装 → Codex；解密 `chatgpt.com` 或 `openai_base_url` 指向 `/backend-api/codex` |
| [微扑克](wpk.md) | NPM采集包，牌桌Room中的业务快照和正式分析 | 扩展 → 推荐 → 微扑克；消息 → 桌Room |
| [统一消息](messages.md) | 所有渠道的Room、正式回复和设置 | 消息导航 |
| [企业微信客户端](wecom-local.md) | 本地消息增量进入 Room，Agent 通过 MCP 请求客户端脚本发送文本 | 扩展 → 已安装 → 企业微信客户端 |
| [企业微信 BOT](wecom.md) | 配置机器人、接收私聊文本并通过界面或 MCP 显式发送 | 扩展 → 推荐 → 企业微信 BOT → 安装 → 配置验证 → 启用 |
| [Agent 查询接口](agent-access.md) | 通过 HTTP 或 MCP 查询实际经过网关的请求 | 同一监听地址的 `/api/*` 与 `/mcp` |

## 尚未交付的能力

以下内容用于识别边界，不提供可执行的功能配方，也不计入当前功能覆盖。

| 能力 | 当前边界 |
| --- | --- |
| 多密钥、失败切换与协议转换 | 尚未实现；[供应商接入](providers.md)每种只保管一把密钥，[Codex 扩展](codex.md)只按 `供应商/模型` 分流，供应商不支持 Responses 时返回上游错误 |
| MCP 服务聚合 | 尚未实现；现有 `/mcp` 是 aicat 自身管理服务 |
| Skill 分发与完整消息渠道体验 | Skill、扫码和多账号界面尚未实现；当前可安装[企业微信 BOT](wecom.md)，验证账号后支持单账号文本收发与 Codex 自动处理，完整方向见[草案](../message-extension-design.md) |
| 费用、用量热力图、请求明细与导出、读取 Agent 会话日志 | 主页已按供应商入口的调用显示 Token、请求、缓存命中率与耗时；费用与这些视图尚未实现 |
| Agent 任务名称、进度、产物及 Skill 实际使用情况 | 需要 Agent 另行上报，不能从代理记录推断；上报能力尚未实现 |
| 脚本请求改写 | 当前无执行入口或管理工具，后续按 Bun 扩展能力另行设计 |
| 编辑与重放请求 | 尚未实现；复制 cURL 不等于内置重放 |
| 私云登录与其他扩展类型 | aicat API 的 NPM 消息渠道支持安装与更新；私云登录和其他社区扩展类型尚未开放 |
| TUN、Tailscale/tsnet、多出口 | 暂缓，不承诺接管本机全部网络流量 |
| 流量历史持久化 | 当前为有界内存记录，退出清空；可手动导出 JSON |
| 微信小程序 | 不属于本仓库 |

## 启动与隔离

构建、环境要求和权限约束遵循 [开发约定](../development.md)。在仓库根目录操作，所有体验与验证使用独立 `--home`。

```sh
make e2e-build
npm --prefix e2e ci
```

已有 harness 会准备本地 HTTP/TLS 上游、独立 Home、证书/系统代理 OS 适配器，并在结束时停止自己创建的进程。桌面与传输场景选择空闲端口；MCP 场景自动选择空闲端口。以下命令是独立验证入口，输出目录每次使用新路径：

| 范围 | 命令 |
| --- | --- |
| 托盘菜单模型、采集对号与代理两态（无头） | `node e2e/tray.cjs bin/aicat-e2e build/feature-map-tray-<run>` |
| 流量入口默认可用、采集默认暂停及手动采集 | `node e2e/traffic-default.cjs bin/aicat-e2e build/feature-map-traffic-default-<run>` |
| 默认导航、无更多入口、旧固定配置与流量启停 | `node e2e/navigation.cjs bin/aicat-e2e build/navigation-<run>` |
| 侧边栏顺序、中英文标签和 Domain 计数 | `node e2e/desktop.cjs bin/aicat-e2e build/feature-map-sidebar-<run> sidebar` |
| 滚动后首次选中、右键、历史与筛选滚动重置 | `node e2e/desktop.cjs bin/aicat-e2e build/feature-map-list-selection-<run> list-selection` |
| CONNECT 双栏及响应侧解密提示 | `node e2e/tunnel.cjs bin/aicat-e2e build/feature-map-tunnel-<run>` |
| 图标布局 | `node e2e/desktop.cjs bin/aicat-e2e build/feature-map-icons-<run> icons` |
| HTTP/TLS 接入 | `python3 e2e/prototype.py --only transport --output build/feature-map-transport-<run>` |
| 流量与 MCP | `python3 e2e/prototype.py --only mcp --output build/feature-map-mcp-<run>` |
| Codex 扩展、模型同步与两个入口 | `node e2e/codex.cjs bin/aicat-e2e build/feature-map-codex-<run>` |
| 消息记录、异常排查、折叠设置与阅读状态移除 | `node e2e/messages.cjs bin/aicat-e2e build/messages-<run>` |
| 供应商密钥与转发 | `node e2e/provider.cjs bin/aicat-e2e build/feature-map-provider-<run>` |
| 扩展包安装、更新、卸载与配置读写 | `node e2e/extension-lifecycle.cjs bin/aicat-e2e build/extension-lifecycle-<run>` |
| 扩展内配置、诊断与导航历史 | `node e2e/settings-navigation.cjs bin/aicat-e2e build/settings-navigation-<run>` |
| 企业微信 BOT 配置、收发与恢复 | `node e2e/wecom.cjs bin/aicat-e2e` |
| 整组桌面验收 | `python3 e2e/prototype.py --only desktop --output build/feature-map-desktop-<run>` |

`<run>` 换成本次运行的唯一名称。日常只验证受影响的流程；整组桌面包含设计稿比较，不应用它代替每个小流程的定向验证。`traffic-default.cjs` 与 `tunnel.cjs` 是无头浏览器与后台应用的独立入口；桌面脚本对 `traffic-default` 保留兼容转发，对旧 `tunnel` 参数在启动应用前退出并提示新命令。`desktop.cjs` 的独立原生场景有 `sidebar`、`app-tree`、`list-selection` 与 `icons`，其余功能文件中的配方用于定向操作，不能给脚本传入不存在的场景名。需要新增自动场景时先提供独立入口。全套 `make e2e` 留到整轮开发结束统一验收，默认仅运行后台场景并汇总报告，不包含原生窗口、真实授权和真实消息送达。

企业微信客户端与微扑克的专属验收分别位于 [aicat-wecom-local](https://github.com/fishioon/aicat-wecom-local) 和 [aicat-wpk](https://github.com/fishioon/aicat-wpk)；在对应仓库执行 `npm run e2e -- --aicat ../aicat`。主仓库的完整后台验收只依赖主工程及合成渠道夹具。

整轮后台验收统一使用 `make e2e`；指定组使用 `python3 e2e/prototype.py --background --scenarios <场景名...> --output build/acceptance-<run>`。新增场景包含 overview、connect、provider-lifecycle、provider-model-state、inspector-copy、proxy-settings、certificate、certificate-entries、traffic-browse、traffic-content、traffic-events、traffic-lifecycle、http-origins、agent-traffic、control-traffic、busy-state、desktop-shell、navigation、extension-details、ipv6-tunnel、app-tree 和 list-selection。后台场景不替代真实原生菜单、窗口和授权验收。

## 驱动约定

- 桌面功能文件使用 Playwright，`page` 指向本次运行的验收预览。它与真实 WKWebView 使用同一套嵌入前端和业务服务；正式二进制没有这个预览入口。其启动与连接范式见 [desktop harness](../../e2e/desktop.cjs)。
- 配方中的 `send(path, body)` 是该 harness 的本地上游请求助手；`api()` 从真实 HTTP API 读取记录；`settled()` 等待界面操作结束。它们是 harness 内的助手，不是终端命令。示例代码在已经完成启动的 harness 上下文中执行。
- 浏览器步骤通过点击、输入、快捷键驱动。优先使用角色和可访问名称；跨语言或文案重复时使用地图列出的现有 ID、`data-*`。不要调用 `window.desktop.navigate`、直接 setter 或预览 RPC 来替代入口操作的证明。
- HTTP 与 MCP 文件使用真实网络接口；`tool(name, arguments)` 是 [MCP harness](../../e2e/mcp.py) 的调用助手。实际工具参数以 MCP `tools/list` 返回的 schema 为准。
- 每条配方从所列前置条件开始。默认中文、浅色、流量功能开启且正在采集；准备普通 HTTP、未解密 CONNECT、完整正文和被截断正文等所需数据。动态域名、端口和请求 ID 必须取自本次 fixture，不能硬编码历史值。
- 原生托盘、窗口隐藏/恢复、系统授权及保存面板要单独记录。Chromium 预览中的点击不能证明 WKWebView 指针操作或真实 macOS 授权已通过。只有 OS 边界和保存面板可使用现有测试适配器。

## 证明与工件

每次验证记录功能 ID、实际入口、操作、预期结果与实测结果；截图同时保留操作前后状态，HTTP/MCP 保留脱敏后的请求与响应。写配置后从 `aicat.yaml` 只读核对，并在涉及持久化的场景重启同一隔离 Home。导出后读取实际文件，不能只截“成功”提示。

工件留在本次 `build/feature-map-*` 目录，包含报告、应用日志、截图、导出文件及必要的配置/OS 状态。复用已有 harness 时同时保留其二进制 hash 和实际运行命令。MCP token、CA 私钥及带认证信息的预览 URL 不出现在公开报告中。

清理只停止本次运行启动的进程和浏览器，不按进程名批量结束应用，不删除证明工件。未达到前置条件或未走到的入口明确记为未验证；不能用 API 验证替代界面验证，也不能用某个入口通过代替其他入口通过。

## 维护方式

参考 poteto 的 [create-verification-skill](https://github.com/cursor/plugins/blob/63912a91d52c43946f974f2aaf661334a6116a82/pstack/skills/create-verification-skill/SKILL.md) 与 [feature map 示例](https://github.com/cursor/plugins/tree/63912a91d52c43946f974f2aaf661334a6116a82/pstack/skills/create-verification-skill/references/feature-map-example)：每个功能文件保留四个 H2，依次写子功能、用户入口、harness 操作与可观察结果、容易使验证失效的条件。正文使用中文，保留原模板的 H2 名称便于对照。

入口、控件、默认状态或用户可见行为变化时，同步更新对应配方及索引。新增已交付能力时移出上面的未交付表。这里维护操作与验证路径；领域术语放在 `CONTEXT.md`，实现设计和构建流程仍由各自文档维护。
