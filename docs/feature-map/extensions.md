# 扩展与导航固定

用户在扩展中启停流量与 Codex 扩展，在“更多”中打开或固定已启用的模块。扩展的边界与验收要求见 [扩展规范](../extension-standard.md)。代理始终运行；流量功能开关和临时暂停采集分别控制不同状态。

## Sub-features

- `extensions-browse`：搜索、展开或折叠已安装/推荐列表，查看详情；内置扩展在已安装里带“内置”标记。
- `extensions-traffic`：启用或停用流量并保存，停用后隐藏入口、停止新采集。
- `extensions-codex`：启用或停用 Codex 扩展，见 [Codex 扩展](codex.md)。
- `extensions-open`：从详情打开流量或代理设置。
- `extensions-navigation`：从“更多”打开模块，固定/取消固定导航入口。
- `extensions-placeholder`：社区扩展和已安装社区扩展显示真实占位状态。

## How to get to it (user POV)

- 导航栏“扩展” → 已安装 → 流量 → 启用/停用；齿轮菜单里是打开流量、代理设置、复制扩展标识符。
- 导航栏“更多” → 点击“流量”打开；点击该行图钉固定或取消固定。
- 扩展侧边栏搜索框筛选条目；推荐一组只有“社区扩展的发现和安装尚未提供”的说明。
- 启动进入概览；流量功能停用时导航和前进后退都跳过流量页，扩展与设置仍可使用。

## Driving it with Playwright

Preconditions:

- 本次验收实例流量已启用、正在采集，已有一条记录，已记录当前域名与导航固定配置。

- **找到扩展。** 点击 `#nav-extensions`，在 `#extension-search` 输入 `traffic`，点击 `#extension-traffic`；详情显示内置流量和 `#feature-traffic[aria-checked="true"]`。输入不匹配的词后条目消失，清空恢复。
- **关闭。** 点击 `#feature-traffic`（文字由“停用”变为“启用”）；`#nav-traffic` 消失，详情出现停用说明，齿轮菜单 `#extension-menu` 里的“打开流量”禁用。调用 `send('/feature-disabled')`，上游仍返回正常结果，`api()` 中没有这条新记录，但原记录仍存在。
- **恢复。** 再点 `#feature-traffic`，在齿轮菜单点击“打开流量”，发送 `/feature-resumed`，列表与 API 出现新记录。先在流量页点 `#recording` 暂停，再关闭和开启功能，回到流量页应仍暂停；手动继续后才采集新请求。
- **固定。** 点击 `#nav-more`，点击 `#popup [data-action="pin-navigation"]` 取消固定；流量功能仍启用，导航按钮消失。再次打开“更多”，点击角色为 `menuitem`、名称精确为“流量”的行，仍能进入流量；再点图钉恢复。只读核对 `navigation.pinned`，重启后仍保持。
- **入口与占位。** 从扩展详情点 `#extension-proxy-settings` 应进入代理设置。返回扩展，点 `#extension-community`，主面板显示“即将推出”，没有安装操作；展开“已安装”显示暂无已安装社区扩展。
- **重启。** 保存流量关闭状态后重启同一 Home，启动应进入概览且导航栏没有流量；再启用并恢复原固定配置。检查 `aicat.yaml` 中 `extensions.traffic.enabled` 与界面一致。

## Gotchas

- 当前 `extensions.traffic.enabled` 缺省开启；产品方向中的默认关闭还未实现。
- 临时暂停不跨重启保留；功能开关与导航固定持久化。重新启用功能保留本次会话之前的暂停状态。
- 关闭流量不关闭代理、规则改写或只读 API；现有记录仍受容量淘汰与清空影响。
- “更多”只有已启用模块可固定；功能停用期间不显示其入口，重新启用恢复原固定位置。
- 当前内置流量与 Codex 两个扩展，默认选中流量；Codex 默认停用。moejs 脚本由 MCP 管理，不能通过社区列表安装。
