# 桌面与偏好

用户通过窗口、导航栏和系统托盘操作 aicat；关闭窗口后代理继续运行，语言与主题在保存后即时生效并跨重启保留。

## Sub-features

- `desktop-window`：关闭窗口隐藏到托盘，通过托盘或 Dock 恢复，显式退出才停止服务。
- `desktop-settings`：从导航栏底部的设置入口、快捷键和托盘进入设置。
- `desktop-layout`：折叠侧边栏，调整流量列表与详情、请求与响应的比例。
- `desktop-language`：选择跟随系统、简体中文或 English。
- `desktop-theme`：选择跟随系统、浅色、深色，或使用标题栏快速切换。

## How to get to it (user POV)

- 启动桌面应用打开主窗口；关闭按钮隐藏窗口，托盘“打开 aicat”或 Dock 重新打开。
- 左下角账号按钮 → 设置，当前进入“代理”；`⌘,` 进入“通用”；托盘“设置…”打开设置。
- 设置侧边栏或顶部页签在“代理”和“通用”间切换。
- 标题栏有后退、前进、折叠侧边栏和主题按钮；流量页面有两条可拖动分隔线。
- 托盘提供暂停/继续采集和退出，原生“退出 aicat”也可用 `⌘Q`。

## Driving it with Playwright and native harness

Preconditions:

- 本次桌面验收实例已就绪，macOS 已解锁；使用隔离 Home。浏览器与原生壳分别记录结果。

- **账号入口。** `await page.locator('#account-menu').click()`，再点击 `page.locator('#popup').getByRole('menuitem', { name: '设置', exact: true })`；主面板显示代理页。另起一步按 `await page.keyboard.press('Meta+,')`，应显示通用页，不能以第一次进入设置代替快捷键验证。
- **语言。** 点击 `#preference-language-en`，等待 `html[lang="en"]`；设置标签和连接文案变为英文。选择 `zh-Hans` 后恢复中文。只读检查 `aicat.yaml` 的 `ui.language`，重启同一 Home 后再次核对。
- **主题。** `await page.locator('#preference-theme-dark').click()`，等待 `html[data-theme="dark"]`；切换 light 和 system，检查按钮的 `aria-pressed` 与实际系统外观。再点 `#theme` 验证快速切换，并检查配置与重启后的外观。
- **布局。** 点击 `#sidebar-toggle`，侧边栏隐藏，再点击恢复；在流量页用鼠标拖动 `#list-split` 和 `#detail-split`，核对两侧尺寸变化及内容可用，保留前后截图。
- **原生生命周期。** 通过真实窗口关闭按钮或 native harness 关闭窗口，向本地上游发送请求；API 仍有新记录。通过托盘和 Dock 分别恢复，再查看新记录。现有 harness 的 `auto.call('menu', {path: ['E2E', '关闭窗口']})` / `打开窗口` 只证明原生隐藏/恢复行为，不是托盘点击覆盖。
- **退出。** 通过原生退出入口退出，核对进程结束、监听关闭；系统代理已接管时另按[代理配方](proxy.md)核对恢复结果。

## Gotchas

- CLI `serve` 没有窗口和托盘，不能用于桌面交互证明。
- 语言、主题持久化；侧边栏折叠、详情视图等窗口内状态不要据此推断也会跨重启保留。
- `⌘[` / `⌘]` 对应导航历史；`⌘K` 聚焦流量搜索。快捷键应在对应页面且没有弹窗拦截时验证。
- 跟随系统必须核对当前系统值；不能因画面恰好是浅色就报告“跟随系统”已生效。
- 锁屏、原生授权等待或恢复失败应报告具体阻塞，不复用旧截图宣称通过。
