# E2E 后台运行

## 何时读

运行验收场景、编写或修改 E2E 脚本，或需要用真实桌面与原生窗口做验收时。

## 约束

- 日常 E2E 默认使用 headless / 后台方式，不弹出应用或浏览器窗口、不抢焦点、不干扰用户桌面。
- 浏览器使用 Playwright `headless: true`；启动桌面验收二进制时同时设置 `AICAT_E2E_BACKGROUND=1`，使原生窗口保持隐藏且不出现 Dock 图标。仅浏览器 headless 不能阻止原生应用弹窗。后台开关仅适用于 `e2e` tag 构建。
- 优先复用已支持后台运行的 `e2e/tray.cjs`、`e2e/traffic-default.cjs`、`e2e/redesign.cjs`、`e2e/provider.cjs`、`e2e/codex.cjs`；新场景默认通过验收预览与真实业务服务验证，保存报告、日志和截图。
- `e2e/desktop.cjs`（除已转发至无头入口的 `traffic-default` 场景）和 `prototype.py --only desktop` 仍操作真实 WKWebView 窗口，不直接用于日常后台验证。受影响流程只有此类入口时，先提供可独立运行的 headless 场景，不直接执行会打扰桌面的脚本。
- 窗口显示/隐藏、原生托盘和系统授权等确需真实桌面的验收，提前说明范围并取得用户明确同意后集中执行；报告如实区分浏览器与原生窗口覆盖，不以 headless 结果替代原生验收。

## 来源

迁移自 `AGENTS.md` 既有「E2E 运行方式」节，原始触发事件未留档；背景约束是日常验收不能弹出窗口、抢焦点或干扰用户桌面。

## 何时可删

全部 E2E 场景都迁移到无头或后台运行、不再有需要真实桌面的入口时。
