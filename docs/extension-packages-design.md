# OpenClaw 渠道宿主与安装

AICat 内置 Bun 与渠道宿主，扩展列表展示受支持的官方 OpenClaw 渠道包，用户安装后配置、验证并启用。第一期以企业微信长连接 Bot 为完整交付范围，单账号、私聊文本、主动 sendText；微信、飞书、群聊、媒体、开放插件市场及任意 npm/git 包加载不属于当前支持范围。

只读[企业微信客户端](feature-map/wecom-local.md)是独立内置扩展，不属于官方渠道包安装范围；它运行随应用分发的 TS 读取器，与 BOT 分开配置、启停和保存游标。

## 模块职责

| 模块 | 职责 |
| --- | --- |
| channelplugin | 维护已验证的官方包目录，准备包与依赖，管理当前安装版本、手动更新和卸载 |
| channelconfig | 定义渠道配置类型、字段约束和校验；调用通用配置存储 |
| extension | 统一展示内置扩展与可安装渠道，安装、配置验证及启用状态门控 |
| channelhost | 启停应用内 Bun、stdio 会话、账号会话绑定及调用 |
| Bun 宿主 | 加载原版插件，提供 OpenClaw SDK 兼容接口，桥接收消息和主动发送 |
| messages | 凭据与验证指纹、来源校验、Room 日志、任务交付与发送回执 |
| agent | 旧版 Codex app-server 的 thread/start、thread/resume、turn/start、turn/steer 及普通回复 |

增加受支持渠道以目录声明与宿主公共能力为边界，不为每个渠道编写 Go Extension。当前 SDK shim 与平台鉴权适配只覆盖已验证的企业微信版本，不宣称任意 OpenClaw 插件或其所有工具都能运行。

## 安装与目录

应用资源包含固定 Bun、宿主、验证入口、兼容层与内置客户端读取器。原版渠道包不随应用分发，也不在应用启动或账号启用时自动安装。

用户点击安装后，使用应用内 Bun 在 Home 的私有暂存目录执行依赖安装，禁用包安装脚本，registry 使用 HTTPS 官方 npm 服务。目录锁定渠道包名与版本；安装产生依赖锁并记录摘要。包名、版本、OpenClaw 渠道声明、入口普通文件与路径边界校验成功后，移动到 extensions/<ID>/<版本>/，最后原子写入 extensions/<ID>/current.json。安装失败清理暂存，不显示为已安装。重复安装有效包不重新下载；损坏的安装可通过安装操作重新准备并原子替换。

current.json 记录当前安装包、实际版本、安装时间与依赖锁摘要；版本目录内 installation.json 必须与它一致。推荐目录的版本是安装和更新目标，运行时只按 current.json 选择实际版本，不按推荐版本推算目录。current.json、安装记录、bun.lock 与原包 package.json 共同判断本地安装是否可用。正式启用只加载该目录的原版入口，不改官方插件文件。插件依赖通过已安装包解析，SDK shim 由应用提供。

## 配置读写

行为配置仍只保存到 aicat.yaml 的 extensions.<ID>。通用 config 只认识 enabled 与扩展自有字段映射，ReadExtension 与 UpdateExtension 提供读取和串行合并保存；字段类型与业务校验由 channelconfig、messages 或具体扩展提供。表单调用 ReadExtensionConfig / UpdateExtensionConfig，启用状态通过专用开关管理，凭据通过 ConfigureMessageChannel 保存和验证。更新只合并提交字段，校验失败不写文件；不允许通过配置接口写 enabled、Secret 或未知业务字段。

修改需要先停用扩展；下次启用由 Go 读取配置与凭据，通过 stdio 注入 Bun 宿主，并转换为原版 OpenClaw 配置视图。不开放运行中插件修改用户配置的接口；游标和任务继续写各自的状态或业务存储。

## 更新与卸载

安装、更新、卸载、配置写入和启停由 extension 按扩展串行协调。内置扩展随应用发布，只能启停，不提供独立更新或卸载。

用户先停用再手动更新。UpdateExtension 只准备推荐目录指定的目标版本，沿用暂存安装与校验，全部成功后切换 current.json；准备失败或进程在准备期间退出，旧记录与旧版本仍可用。更新保留配置、凭据、状态和 Room 数据，并保持停用；鉴权指纹绑定实际安装版本，因此更新后需要重新验证。旧版本目录保留，但没有自动更新、运行中替换或手动版本选择界面。

用户在更多菜单确认卸载后，UninstallExtension 先持久保存停用状态，撤销 current.json，再删除该扩展全部包目录。配置、凭据、状态和 Room 数据保留；重复卸载可清理残留目录，重新安装后仍保持停用。没有 current.json 的旧安装目录不会被自动识别或迁移。

## 账号与生命周期

安装、配置验证、正式启用是三个动作。用户保存 Bot ID 和 Secret 后，通过官方 SDK 的临时连接验证真实鉴权，鉴权成功即关闭；失败返回错误且不能启用。验证不启动渠道处理器，不触发 Agent 或发送平台消息。官方插件的固定成功 probeAccount 不用作鉴权依据。

凭据保存到 credentials/<ID>.json，0600；界面不回传 Secret。验证指纹关联凭据、插件版本与平台端点，不是独立行为配置。行为仍只保存在 aicat.yaml。

首次启用才启动宿主与正式平台连接。停用取消账号及 Codex 工作并回收不用的进程，保留安装、凭据和 Room 数据。重启只恢复已安装、已验证且保存了启用意图的渠道，不联网补装缺少的插件。

## 消息与能力

OpenClaw 插件通过注册和运行时回调与宿主交互。兼容层将入站消息转成 AICat CloudEvent，data 直接使用旧版消息正文；平台私有回复上下文不交给 Codex。

AICat 的公共发送语义是主动 sendText。第一期企业微信使用插件 outbound.sendText，底层走官方 SDK sendMessage。普通 Codex 输出由 AICat 主动发回，显式 messages_send 复用同一发送与回执服务。平台不支持主动发送时，不能仅靠接口转换宣称具备不受时效限制的主动能力。

插件附带工具、CLI、HTTP 路由、prompt hooks 不直接注册进 AICat；媒体和群聊保持明确的不支持状态。独立 Bun 进程不是操作系统沙箱，官方插件仍按应用权限执行。

## 验收

- 后台界面完成“推荐 → 安装 → 配置真实格式凭据 → 鉴权验证 → 启用 → 私聊 → Codex → 主动回复”。
- 应用资源无渠道原包；安装原包与 SDK 在启停和收发后保持文件摘要不变。
- registry 失败不留下安装，重试成功，重复安装幂等，损坏记录可修复。
- 配置鉴权失败不能启用；验证结束关闭连接，不产生消息或 Agent 任务。
- Room 日志与任务保存后才确认入站；失败不确认，未确认消息及 pending 交付可恢复，unknown 不自动重跑或重发。
- 同 Room 执行中新消息使用 turn/steer，不同 Room 保持独立 thread。
- 真正的企业微信与 Codex 联调使用隔离 Home，并另行核对收件端。纯 headless 结果不代替真实平台送达或原生桌面覆盖。

具体入口与工件见[企业微信 BOT](feature-map/wecom.md)。

## 生命周期定向验收

`node e2e/extension-lifecycle.cjs bin/aicat-e2e build/extension-lifecycle-<run>` 使用隔离 Home、本地 NPM 包源、真实 Bun 安装器与 Desktop 服务，通过 headless Chromium 验证配置合并与拒绝非法写入、安装失败清理、禁止安装脚本、推荐版本与实际安装版本分离、更新失败与中断恢复、卸载保留数据及重新安装。报告保留命令、二进制摘要、日志和截图。`wecom.cjs` 使用原版插件验证运行中拒绝配置修改、更新及卸载，配置只读结果不包含凭据。
