# aicat feature map

这份地图说明当前 aicat 有哪些用户可用能力、从哪里进入、如何操作，以及什么可观察结果能证明功能正常。先读索引，再按对应功能文件验证。

aicat 的产品方向是用户节点上的常驻 Agent Gateway：用户主要在自己的 Agent 中交互，桌面负责接入、配置、授权与诊断。当前已经交付的是 HTTP/HTTPS 代理、流量检查、脚本改写和供 Agent 使用的 API/MCP。地图以当前实现为准，产品目标见 [CONTEXT.md](../../CONTEXT.md)。

## 功能索引

| 功能 | 用户想完成的事 | 当前入口 |
| --- | --- | --- |
| [桌面与偏好](desktop.md) | 打开或隐藏窗口，找到设置，切换语言和主题 | 导航栏（概览、设置）、快捷键、托盘 |
| [扩展与导航固定](extensions.md) | 启停流量与 Codex 扩展，固定常用入口 | 扩展 → 已安装；更多 |
| [代理接入](proxy.md) | 让客户端经过 aicat，修改监听，接管或恢复系统代理 | 设置 → 代理；CLI `serve` |
| [证书与 HTTPS 解密](https.md) | 信任、查看、导出或删除证书，选择需要解密的域名 | 设置 → 代理；CONNECT 详情 |
| [查找流量](traffic-browse.md) | 实时查看、搜索、按域名/路径/应用筛选，固定域名 | 流量 → 侧边栏与请求列表 |
| [检查与导出请求](traffic-inspect.md) | 阅读请求/响应，复制 URL、cURL 或正文，导出结果 | 请求行、右键菜单、详情、标题栏 |
| [供应商接入](providers.md) | 让 aicat 保管供应商密钥并同步模型列表，Agent 经 aicat 调用模型 | 概览 → 接入供应商；同一监听地址的 `/p/<供应商 ID>` |
| [Codex 扩展](codex.md) | 在 Codex 的模型列表中使用供应商的模型，保留 ChatGPT 登录 | 扩展 → 已安装 → Codex；解密 `chatgpt.com` 或 `openai_base_url` 指向 `/backend-api/codex` |
| [Agent 查询接口](agent-access.md) | 通过 HTTP 或 MCP 查询实际经过网关的请求 | 同一监听地址的 `/api/*` 与 `/mcp` |
| [规则与脚本插件](rewrite.md) | 让 Agent 编写改写、离线试运行并检查实际效果 | MCP；当前无桌面编辑页 |

## 尚未交付的能力

以下内容用于识别边界，不提供可执行的功能配方，也不计入当前功能覆盖。

| 能力 | 当前边界 |
| --- | --- |
| 多密钥、失败切换与协议转换 | 尚未实现；[供应商接入](providers.md)每种只保管一把密钥，[Codex 扩展](codex.md)只按 `供应商/模型` 分流到支持 Responses 的供应商 |
| MCP 服务聚合 | 尚未实现；现有 `/mcp` 是 aicat 自身管理服务 |
| Skill 分发与消息渠道 | 尚未实现；消息扩展的设计见 [草案](../message-extension-design.md) |
| Token、费用、缓存命中率等用量统计与趋势图 | 概览已显示网关状态、接入地址、待处理事项和按流量记录统计的请求；模型用量需要解析模型响应，尚未实现 |
| Agent 任务名称、进度、产物及 Skill 实际使用情况 | 需要 Agent 另行上报，不能从代理记录推断；上报能力尚未实现 |
| 流量检查默认关闭 | 设计目标；当前 `features.traffic` 缺省仍为 `true` |
| 规则与插件管理界面 | 后端与 MCP 已有，桌面编辑页尚未实现 |
| 编辑与重放请求 | 尚未实现；复制 cURL 不等于内置重放 |
| 私云登录、社区扩展安装/更新 | 当前仅有状态说明或占位；脚本插件不属于社区扩展列表 |
| TUN、Tailscale/tsnet、多出口 | 暂缓，不承诺接管本机全部网络流量 |
| 流量历史持久化 | 当前为有界内存记录，退出清空；可手动导出 JSON |
| 微信小程序 | 不属于本仓库 |

## 启动与隔离

构建、环境要求和权限约束遵循 [开发约定](../development.md)。在仓库根目录操作，所有体验与验证使用独立 `--home`。

```sh
make e2e-build
npm --prefix e2e ci
```

已有 harness 会准备本地 HTTP/TLS 上游、独立 Home、证书/系统代理 OS 适配器，并在结束时停止自己创建的进程。桌面与传输场景选择空闲端口；规则场景默认使用 19993，运行前检查占用，并行时用 `e2e/rules.py --port <空闲端口>` 直接运行。以下命令是独立验证入口，输出目录每次使用新路径：

| 范围 | 命令 |
| --- | --- |
| 侧边栏顺序、中英文标签和 Domain 计数 | `node e2e/desktop.cjs bin/aicat-e2e build/feature-map-sidebar-<run> sidebar` |
| 滚动后首次选中、右键、历史与筛选滚动重置 | `node e2e/desktop.cjs bin/aicat-e2e build/feature-map-list-selection-<run> list-selection` |
| 图标布局 | `node e2e/desktop.cjs bin/aicat-e2e build/feature-map-icons-<run> icons` |
| HTTP/TLS 接入 | `python3 e2e/prototype.py --only transport --output build/feature-map-transport-<run>` |
| 规则、插件与 MCP | `python3 e2e/prototype.py --only rules --output build/feature-map-rules-<run>` |
| Codex 扩展、模型同步与两个入口 | `node e2e/codex.cjs bin/aicat-e2e build/feature-map-codex-<run>` |
| 供应商密钥与转发 | `node e2e/provider.cjs bin/aicat-e2e build/feature-map-provider-<run>` |
| 整组桌面验收 | `python3 e2e/prototype.py --only desktop --output build/feature-map-desktop-<run>` |

`<run>` 换成本次运行的唯一名称。日常只验证受影响的流程；整组桌面包含设计稿比较，不应用它代替每个小流程的定向验证。当前桌面脚本有独立的 `sidebar`、`app-tree`、`list-selection` 与 `icons` 场景，其余功能文件中的配方用于定向操作，不能给脚本传入不存在的场景名。需要新增自动场景时先提供独立入口。全套 `make e2e` 留到整轮开发结束统一验收。

## 驱动约定

- 桌面功能文件使用 Playwright，`page` 指向本次运行的验收预览。它与真实 WKWebView 使用同一套嵌入前端和业务服务；正式二进制没有这个预览入口。其启动与连接范式见 [desktop harness](../../e2e/desktop.cjs)。
- 配方中的 `send(path, body)` 是该 harness 的本地上游请求助手；`api()` 从真实 HTTP API 读取记录；`settled()` 等待界面操作结束。它们是 harness 内的助手，不是终端命令。示例代码在已经完成启动的 harness 上下文中执行。
- 浏览器步骤通过点击、输入、快捷键驱动。优先使用角色和可访问名称；跨语言或文案重复时使用地图列出的现有 ID、`data-*`。不要调用 `window.desktop.navigate`、直接 setter 或预览 RPC 来替代入口操作的证明。
- HTTP 与 MCP 文件使用真实网络接口；规则文件中的 `tool(name, arguments)` 是 [rules harness](../../e2e/rules.py) 的 MCP 调用助手。实际工具参数以 MCP `tools/list` 返回的 schema 和 [插件 SDK](../../internal/mcpserver/guide.md) 为准。
- 每条配方从所列前置条件开始。默认中文、浅色、流量功能开启且正在采集；准备普通 HTTP、未解密 CONNECT、完整正文和被截断正文等所需数据。动态域名、端口和请求 ID 必须取自本次 fixture，不能硬编码历史值。
- 原生托盘、窗口隐藏/恢复、系统授权及保存面板要单独记录。Chromium 预览中的点击不能证明 WKWebView 指针操作或真实 macOS 授权已通过。只有 OS 边界和保存面板可使用现有测试适配器。

## 证明与工件

每次验证记录功能 ID、实际入口、操作、预期结果与实测结果；截图同时保留操作前后状态，HTTP/MCP 保留脱敏后的请求与响应。写配置后从 `aicat.yaml` 只读核对，并在涉及持久化的场景重启同一隔离 Home。导出后读取实际文件，不能只截“成功”提示。

工件留在本次 `build/feature-map-*` 目录，包含报告、应用日志、截图、导出文件及必要的配置/OS 状态。复用已有 harness 时同时保留其二进制 hash 和实际运行命令。MCP token、CA 私钥及带认证信息的预览 URL 不出现在公开报告中。

清理只停止本次运行启动的进程和浏览器，不按进程名批量结束应用，不删除证明工件。未达到前置条件或未走到的入口明确记为未验证；不能用 API 验证替代界面验证，也不能用某个入口通过代替其他入口通过。

## 维护方式

参考 poteto 的 [create-verification-skill](https://github.com/cursor/plugins/blob/63912a91d52c43946f974f2aaf661334a6116a82/pstack/skills/create-verification-skill/SKILL.md) 与 [feature map 示例](https://github.com/cursor/plugins/tree/63912a91d52c43946f974f2aaf661334a6116a82/pstack/skills/create-verification-skill/references/feature-map-example)：每个功能文件保留四个 H2，依次写子功能、用户入口、harness 操作与可观察结果、容易使验证失效的条件。正文使用中文，保留原模板的 H2 名称便于对照。

入口、控件、默认状态或用户可见行为变化时，同步更新对应配方及索引。新增已交付能力时移出上面的未交付表。这里维护操作与验证路径；领域术语放在 `CONTEXT.md`，实现设计和构建流程仍由各自文档维护。
