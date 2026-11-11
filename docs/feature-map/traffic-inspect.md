# 检查与导出请求

用户选中流量记录后，分别阅读请求与响应的 Header/Raw，复制当前内容或 cURL，并把当前筛选结果的摘要导出为 JSON。

## Sub-features

- `inspect-selection`：选中显示详情，Escape、清空、筛选排除或淘汰后取消选择。
- `inspect-views`：请求与响应各自的 Header/Raw、文本与二进制正文、采集状态提示。
- `inspect-copy`：按当前视图复制请求/响应，右键复制 URL 或完整请求 cURL。
- `inspect-export`：导出当前筛选下的请求摘要并核对文件内容。

## How to get to it (user POV)

- 流量列表单击请求行，下方显示请求/响应双栏；各自点击 Header 或 Raw。
- 各栏右上角复制按钮；请求行右键 → 复制 URL、复制 cURL、固定/取消固定。
- 流量页标题栏“导出记录”；按 Escape 返回“未选择请求”。
- CONNECT 响应侧提供加密提示与域名解密入口，见[HTTPS](https.md)。

## Driving it with Playwright

Preconditions:

- 准备包含重复 headers、换行文本、二进制、大正文和 CONNECT 的真实请求；复制 cURL 使用完整采集且只指向本地 fixture 的请求。

- **选择与空状态。** 用真实流量入口进入页面，无选择时仅有 `#detail-empty`；点击 `.traffic-row[data-id="实际ID"]`，`#detail-title` 对应目标请求，`#detail-content` 可见。按 `await page.keyboard.press('Escape')` 后为空状态；另分别用清空、排除该行的筛选和容量淘汰验证，不允许旧异步详情重新填回。
- **双栏视图。** 点击 `[data-side="request"] [data-view="raw"]`，请求侧显示请求行、headers 和正文，响应侧仍为 Header；切换记录不重置两侧选择。响应侧切 Raw 后核对多值 headers、正文空白与换行；二进制显示十六进制，正文不完整时 `.body-notice` 显示采集状态，不能将前缀当作完整正文。
- **复制详情。** 分别点击 `[data-side-copy="request"]` 和 `[data-side-copy="response"]`；从真实剪贴板读取内容，与当前可见视图比较，Raw 不包含行号，Header 保留多值；复制按钮附近出现反馈。Header 与 Raw、请求与响应分别验证。
- **右键复制。** 对目标请求行 `click({button: 'right'})`，在 `#popup` 选精确名称“复制 URL”或“复制 cURL”的 menuitem；用 `pbpaste` 核对剪贴板。仅对本次本地 fixture 执行得到的 cURL，比较上游接收的方法、URL、headers 与带单引号/换行的文本正文。CONNECT 或正文非完整状态时 cURL 菜单项应禁用。
- **导出。** 在 `#search` 输入仅命中一条 fixture 的词，点击 `#export`；正式应用使用原生保存面板，验收构建写入 OS fixture 同目录的 `aicat-traffic.json`。读取 JSON 数组，核对 ID、URL、记录数量与本次筛选一致，再保留文件与界面截图。当前文件仅含摘要，不含 headers、正文和四阶段快照。

## Gotchas

- 桌面当前请求侧显示 `RequestReceived`、响应侧显示 `ResponseSent`，没有四阶段对比切换器。完整改写前后快照通过 API/MCP 读取。
- Raw 基于已解析的 HTTP 消息重建；不能证明原始 header 大小写和顺序。响应协议版本不可得时显示 HTTP。
- 响应未到达、body pending/truncated/omitted/error 都需要按实际状态断言。采集截断不意味着网络转发也被截断。
- 导出跟随当前过滤结果，不只导出选中请求，也不默认导出被筛掉的记录。需要正文或四阶段证明时另从 API/MCP 保存。
- 复制成功提示不证明剪贴板内容正确；导出提示不证明磁盘文件正确。剪贴板为真实系统资源，验证工件不保存运行前的私人内容。
- 执行复制出的 cURL 会再发送请求；只使用受控本地上游，不能对用户生产请求自动重发。
