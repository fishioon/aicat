# 扩展与导航固定

用户在扩展中启停流量、Codex、企业微信客户端与企业微信 BOT 扩展，在“更多”中打开或固定已启用的模块。扩展的边界与验收要求见 [扩展规范](../extension-standard.md)。代理始终运行；流量功能开关和临时暂停采集分别控制不同状态。

## Sub-features

- `extensions-browse`：搜索、展开或折叠已安装/推荐列表，查看详情；只有独立功能说明才提供页签，企业微信客户端直接展示详情。内置扩展在已安装里带“内置”标记。
- `extensions-traffic`：启用或停用流量并保存，停用后隐藏入口、停止新采集。
- `extensions-codex`：启用或停用 Codex 扩展，见 [Codex 扩展](codex.md)。
- `extensions-wecom-local`：配置与启停内置的只读企业微信客户端，增量消息保存到 Room，见[企业微信客户端](wecom-local.md)。
- `extensions-wecom`：配置、启停企业微信 BOT 并收发私聊文本，见[企业微信 BOT](wecom.md)。
- `extensions-settings`：详情点击“配置”进入设置中的扩展表单；“诊断”查看任务、消息及发送测试。详情不混放配置与运行流水。
- `extensions-open`：从详情打开流量或代理设置。
- `extensions-navigation`：从“更多”打开模块，固定/取消固定导航入口。
- `extensions-availability`：推荐列表只展示当前受支持的可安装扩展，未安装时不提供配置与诊断入口。
- `extensions-icons`：Codex 与企业微信 BOT 使用随前端打包的官方应用图标，列表与详情一致；Codex 按应用主题切换浅深色资源，流量保留功能图标。

## How to get to it (user POV)

- 导航栏“扩展” → 已安装 → 流量 → 启用/停用；齿轮菜单里是打开流量、代理设置、复制扩展标识符。
- 企业微信 BOT 详情 → 配置，或设置 → 扩展 → 企业微信 BOT；详情或配置页 → 诊断。前进/后退恢复页面与所选扩展。
- 导航栏“更多” → 点击“流量”打开；点击该行图钉固定或取消固定。
- 扩展侧边栏搜索框筛选条目；推荐列表展示尚未安装的企业微信官方插件；安装后移入已安装列表，配置鉴权验证通过后才能启用。
- 流量扩展默认启用；采集默认暂停，在流量页点击“开始采集”后才记录新请求。
- 启动进入主页；流量功能停用时导航和前进后退都跳过流量页，扩展与设置仍可使用。

## Driving it with Playwright

Preconditions:

- **详情与空状态。** `node e2e/extension-details.cjs bin/aicat-e2e build/extension-details-<run>` 后台逐项验证当前登记扩展的详情/功能页签、搜索无匹配状态、安装与配置入口边界、键盘页签、Codex 手动配置复制及设置跳转，保留全部面板文字与浅深色截图；不下载插件或连接真实平台。

- **品牌图标。** 独立后台运行 `node e2e/extension-icons.cjs bin/aicat-e2e`，验证列表和详情的官方图标、本地加载、浅深色切换和 36/64px 容器，保存截图与报告；不启用渠道或访问真实凭据。资源来源见各扩展 assets/README.md。

- 本次验收实例流量已启用、正在采集，已有一条记录，已记录当前域名与导航固定配置。

- **默认不采集。** 可独立运行 `node e2e/traffic-default.cjs bin/aicat-e2e build/traffic-default-<run>`。从全新 Home 启动，流量扩展启用且导航入口可见，打开流量页显示尚未开始采集；代理请求成功但 `/api/traffic` 为空。点击“开始采集”后记录新请求；扩展启停保留本次会话的采集状态，重启后默认回到暂停。另核对缺失、空/null 配置、显式扩展开关与 `traffic.recording`，保留截图和报告。
- **找到扩展。** 点击 `#nav-extensions`，在 `#extension-search` 输入 `traffic`，点击 `#extension-traffic`；详情显示内置标记，`#feature-traffic` 的文字为“停用”。输入不匹配的词后条目消失，清空恢复。
- **关闭。** 点击 `#feature-traffic`（文字由“停用”变为“启用”）；`#nav-traffic` 消失，详情出现停用说明，齿轮菜单 `#extension-menu` 里的“打开流量”禁用。调用 `send('/feature-disabled')`，上游仍返回正常结果，`api()` 中没有这条新记录，但原记录仍存在。
- **恢复。** 再点 `#feature-traffic`，在齿轮菜单点击“打开流量”，发送 `/feature-resumed`，列表与 API 出现新记录。先在流量页点 `#recording` 暂停，再关闭和开启功能，回到流量页应仍暂停；手动继续后才采集新请求。
- **固定。** 点击 `#nav-more`，点击 `#more-pop [data-action="pin-navigation"]` 取消固定；流量功能仍启用，导航按钮消失。再次打开“更多”，点击 `#more-traffic`，仍能进入流量；再点图钉恢复。只读核对 `navigation.pinned`，重启后仍保持。
- **入口与占位。** 推荐列表的企业微信条目提供安装入口；点击安装后显示已安装状态。未安装条目没有配置入口，安装后可配置，验证通过才允许启用。
- **重启。** 保存流量关闭状态后重启同一 Home，启动应进入主页且导航栏没有流量；再启用并恢复原固定配置。检查 `aicat.yaml` 中 `extensions.traffic.enabled` 与界面一致。

## Gotchas

- `extensions.traffic.enabled` 缺省开启，控制入口和功能可用性；`traffic.recording` 缺省 `false`，控制启动时是否采集。显式配置优先。
- 手动开始/暂停仅作用于本次运行，重启按 `traffic.recording` 初始化（默认暂停）；功能开关与导航固定持久化。重新启用功能保留本次会话之前的暂停状态。
- 关闭流量不关闭代理或只读 API；现有记录仍受容量淘汰与清空影响。
- “更多”只有已启用模块可固定；功能停用期间不显示其入口，重新启用恢复原固定位置。
- 当前内置流量、Codex 与只读企业微信客户端，默认选中流量；企业微信 BOT 在推荐列表提供官方插件安装入口，安装、配置鉴权验证与启用分开。其他渠道和任意社区包尚未开放。
