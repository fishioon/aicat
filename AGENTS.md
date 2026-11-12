# 项目约束

- 本仓库只维护 aicat 桌面端；微信小程序不在本仓库范围内。
- 领域语言以 `CONTEXT.md` 为准，模块边界以 [架构](docs/architecture.md) 为准；文档只描述当前状态，不记录变更过程。
- 功能开发只验证受影响的流程；全套 E2E 留到整轮开发结束、准备统一验收时执行，不因单个功能完成或小修重复跑全套。脚本仅支持全量时，先提供可独立运行的相关场景。
- 不要写单元测试。
- 样式尽量简单、统一，优先保持原生表现，只在关键位置使用主题色。能修改或合并原有规则时，不新增覆盖规则；清理样式时只处理明显多余的声明，不扩大范围或重新设计界面。

## E2E 运行方式

- 日常 E2E 默认使用 headless / 后台方式，不弹出应用或浏览器窗口、不抢焦点、不干扰用户桌面。
- 浏览器使用 Playwright `headless: true`；启动桌面验收二进制时同时设置 `AICAT_E2E_BACKGROUND=1`，使原生窗口保持隐藏且不出现 Dock 图标。仅浏览器 headless 不能阻止原生应用弹窗。后台开关仅适用于 `e2e` tag 构建。
- 优先复用已支持后台运行的 `e2e/tray.cjs`、`e2e/traffic-default.cjs`、`e2e/redesign.cjs`、`e2e/provider.cjs`、`e2e/codex.cjs`；新场景默认通过验收预览与真实业务服务验证，保存报告、日志和截图。
- `e2e/desktop.cjs`（除已转发至无头入口的 `traffic-default` 场景）和 `prototype.py --only desktop` 仍操作真实 WKWebView 窗口，不直接用于日常后台验证。受影响流程只有此类入口时，先提供可独立运行的 headless 场景，不直接执行会打扰桌面的脚本。
- 窗口显示/隐藏、原生托盘和系统授权等确需真实桌面的验收，提前说明范围并取得用户明确同意后集中执行；报告如实区分浏览器与原生窗口覆盖，不以 headless 结果替代原生验收。

用户功能、入口与验证路径见 [Feature map](docs/feature-map/README.md)，领域文档的用法见 [Domain docs](docs/agents/domain.md)，构建与安装见 [开发约定](docs/development.md)，需求跟踪见 [Issue tracker](docs/agents/issue-tracker.md)。
