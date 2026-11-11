# 查找流量

用户实时查看经过代理的请求，按域名、路径或本机应用缩小范围，用 URL 搜索定位记录，并通过历史导航返回之前的查看位置。

## Sub-features

- `traffic-list`：显示请求 ID、URL、方法、状态、耗时、大小和实时更新。
- `traffic-url-search`：完整 URL 模糊搜索、空结果与清除搜索。
- `traffic-tree`：固定 → 域名 → 应用分组，域名/路径层级与应用筛选。
- `traffic-domain-pin`：从侧边栏或请求右键固定域名，跨重启与清空保留。
- `traffic-history`：前进/后退恢复筛选、搜索及仍有效的选择。
- `traffic-recording`：暂停/继续采集，清空记录与筛选。

## How to get to it (user POV)

- 导航栏“流量”；未固定时从“更多 → 流量”或“扩展 → 流量 → 打开流量”进入。
- 左侧“查找应用或域名”缩小侧边栏条目；单击应用、域名或路径行筛选列表（与 URL 搜索叠加），双击行筛选并展开或折叠子树，也可单击箭头切换。
- 列表顶部搜索完整 URL，`⌘K` 聚焦；标题栏后退/前进或 `⌘[` / `⌘]` 恢复历史。
- 域名行或请求行右键 → 固定/取消固定；标题栏暂停/继续、工具栏清空；托盘也有暂停/继续。

## Driving it with Playwright

Preconditions:

- 流量功能启用且正在采集；本地上游准备 `/chat/completions`、`/responses`、`/api/a`、`/api/ab` 和 `/api/a/child`，并包含不同方法、两个域名/端口及 CONNECT 数据。

- **列表。** 通过一个真实入口进入流量，`await send('/responses')`，等待 `#rows .traffic-row` 出现该 URL；从 `api()` 找到 ID，使用 `.traffic-row[data-id="实际ID"]` 点击，核对选中状态与详情。滚动 `#rows-viewport` 后再定位屏幕外记录；首次单击应立即选中并展示详情，列表保持原滚动位置。可独立运行 `node e2e/desktop.cjs bin/aicat-e2e build/list-selection-<run> list-selection`，覆盖滚动后的单击、右键、相同筛选下的历史导航，以及搜索/域名筛选变化时回到顶部；保留点击前后状态、截图和 WKWebView DOM 验证记录。
- **URL 搜索。** `await page.locator('#search').fill('rspns')` 应匹配 `/responses`；`chat cmpl` 匹配 `/chat/completions`；输入不匹配的词后 `#list-empty` 显示“没有匹配的请求”。大小写不改变结果，清空后恢复原筛选下记录，结果保持采集顺序。
- **侧边栏。** `#domains [data-group]` 的值依次为 `pins`、`domains`、`apps`；中文标签固定/域名/应用，英文 Pin/Domain/App。`#domain-count` 是域名计数。可独立运行索引中的 `sidebar` 场景，并保留中英文及 WKWebView 截图。
- **路径和应用。** 在 `[data-group="domains"]` 内用 `.ac-side-item[data-host="本次host"][data-path=""]` 选中域名；通过相邻展开按钮逐级显示 `/api/a`。点击该路径应包含 `/api/a/child`、排除 `/api/ab`。折叠选中路径的祖先，筛选回到可见祖先。应用在 `[data-group="apps"] [data-app]` 中选择。先在 `#search` 输入词，再单击或双击应用、域名、路径行，搜索框内容不变，列表为两者交集；在 `#domain-search` 输入片段只筛选侧边栏候选。
- **双击展开/折叠。** 可独立运行 `node e2e/desktop.cjs bin/aicat-e2e build/sidebar-doubleclick-<run> app-tree`。对应用、全局域名、固定域名和路径行调用 `dblclick()`，核对相邻箭头的 `aria-expanded` 切换且子节点显示/隐藏；双击箭头或固定/域名/应用分组标题只切换一次。单击行仍筛选；叶节点双击不展开，后退一次恢复上一筛选。报告区分 Chromium 指针输入和 WKWebView DOM 事件，并保留浅深色截图。
- **固定。** 右键域名按钮，在 `#popup` 点击精确名称“固定”的 menuitem；固定分组出现该域名。请求行右键同样操作，核对同一状态；清空及重启后仍固定，只读检查 `traffic.pinnedDomains`。
- **历史。** 选中请求，输入搜索，再点击另一域名；点 `#history-back` / `#history-forward`，核对筛选、搜索和选择恢复。记录被清空/淘汰后返回历史不能复活旧详情。另验证两组键盘快捷键。
- **暂停与清空。** 点击 `#recording` 后发送 `/paused`，上游成功但没有新记录；继续后 `/resumed` 出现。通过托盘独立重复暂停/继续。点击 `#clear`，API 列表为空、URL 与域名筛选复位、详情为空，固定域名仍保留；再发请求，其 ID 大于清空前最大值。

## Gotchas

- URL 搜索是大小写不敏感、有序字符模糊匹配，空白分词全部命中；不支持 fzf 扩展语法。HTTP/MCP 查询不能假定采用同一搜索算法。
- URL 搜索与侧边栏筛选相互独立：选择应用、域名、路径或回到全部域名都保留搜索，离开流量页再返回也保留；只有清除搜索框或点击清空才复位。
- 应用归属表示直接连接代理的本机进程，识别为异步且可能未知；不能据此推断请求的最终业务来源。应用计数分别显示 HTTP 与 CONNECT。
- 列表为虚拟列表，DOM 行数不等于记录总数。记录与 body 有容量上限，进程退出后清空；清空不重置 ID，进程重启才从 1 编号。
- 界面“代理监听中”不证明已配置客户端或系统代理；空列表先核对真实代理请求是否发出。
