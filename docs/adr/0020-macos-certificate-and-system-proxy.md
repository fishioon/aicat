# 首次证书引导与可恢复的系统代理

状态：accepted。取代 0018 中“不接管 macOS system proxy”的约定。

首次 Serve Session 自动生成本机独有的 aicat CA；Main Window 在未信任时引导用户安装。
“安装并信任证书”采用 Proxyman 自动模式的交互：公开 CA 导入当前用户的默认文件钥匙串（通常是 login），
由 macOS 授权设置当前用户的所有用途 Always Trust。私钥始终留在 aicat Home。系统代理仍需独立开启。

之前把 `security add-trusted-cert` 整体放进管理员 AppleScript：证书导入可成功，但后续信任授权
因非交互上下文失败（`authorization denied since no user interaction possible`）。直接在普通用户
进程中 `SecItemAdd` 到系统钥匙串则返回写权限错误（-61），管理员账号不等于进程已是 root。

曾采用“提权导入 System 钥匙串 + GUI 写 admin domain 信任”解决上述问题，但全新安装触发两次授权。
2026-10-16 同机实测 Proxyman 26.1.0 全新 CA：一次授权成功，证书实际位于 login 钥匙串，信任实际位于
user domain，System 钥匙串和 admin domain 都没有新 CA。旧证书仍在但指纹不同，不是复用旧信任。

因此对齐当前用户范围。曾使用 C + cgo 直接调用 `SecItemAdd` 和
`SecTrustSettingsSetTrustSettings`；但原生 API 不是实现这一流程的必要条件。
2026-10-16 再以独立新 CA 实测 Go `os/exec` 直接调用 Apple 自带的 `security`：一次授权成功，
精确指纹证书出现在 login 钥匙串，用户信任导出中没有任何用途限制，实际 SSL 验证通过，既有信任未改变。
之前的失败限于提权脚本与系统级信任的组合，不能推广到当前用户直接执行命令。

现使用 `security default-keychain -d user` 获取默认文件钥匙串路径，再通过参数数组执行
`security add-trusted-cert -r trustRoot -k <默认钥匙串> <公开 CA>`。
不加 `-d`（当前用户域），不加 `-p ssl`（所有用途 Always Trust），不硬编码钥匙串路径。
证书模块不再有 C/Objective-C/cgo 适配层；仅依赖 Go 标准库和 macOS 自带命令，纯 Go CLI 同样可检测信任。
取消或失败不显示成功，也不自动开启代理；重新操作可复用已导入但尚未信任的同一张 CA。
用途从 HTTPS 扩展到邮件、代码签名等，但不扩展到其他 macOS 用户；该选择由用户明确确认。
普通已解锁 login 钥匙串不需管理员提权，
锁定钥匙串等特殊状态仍可能有额外系统交互，不以隐藏系统授权来保证固定次数。
不使用 `AuthorizationExecuteWithPrivileges`、AppleScript、shell、特权 helper 或公开证书临时快照；
不改写 AuthorizationDB，不使用私有 API，不绕过系统安全策略。
既有 System/admin 信任不自动删除或迁移，清理旧身份需用户明确授权，不能作为安装的隐藏副作用。
第一版只保留“安装并信任证书”入口，不提供手动安装或显示文件操作。
状态检测通过 `security trust-settings-export` 只读导出 user domain 到 0700 临时目录（读完删除），
Go 标准库解析 XML plist，按 DER SHA-1 精确匹配证书，确认存在不受用途限制的根信任；
不依赖同名匹配或本地化的人类可读输出。再用 macOS `security verify-cert` 验证实际 SSL 链，
不传会显式信任目标根的 `-r`。旧的 SSL-only 或 System/admin 信任不算
本版本已完成的用户级所有用途信任，UI 保持安装按钮可用，允许通过同一入口升级。
安装返回后再次验证；取消或失败后仍可通过同一入口重试。

系统代理是独立的、首次默认关闭的持久开关：唯一行为配置为 `aicat.yaml` 的 `systemProxy.enabled`，
恢复快照不承担开关配置。用户确认开启后，保留只读 SystemConfiguration 适配层获取稳定服务 ID 与 Proxies 字典，
写入由 Go 参数数组直接调用 Apple `/usr/sbin/networksetup`，只接管当前网络位置内已启用的服务。
同时设置 HTTP 与 HTTPS，临时关闭 PAC、自动发现与
SOCKS，保留绕过列表。aicat 网关仍只按拦截白名单解密；网关和 Codex addon 不承担系统设置业务。

写入之前把原始字典与目标字典原子保存到 `~/.aicat/state/system-proxy.json`（0600），这是撤销日志，
不是第二份行为配置。关闭系统代理、停止网关、正常退出和异常退出后的下一次启动均尝试恢复。
正常退出或停止网关不清除开关；下次启动先恢复旧会话，再等待本次网关监听就绪、确认当前 CA 仍可信，
最后按持久开关自动接管。恢复失败或外部冲突时不进行新接管，本地网关继续可用并显示错误。
用户明确关闭时先保存关闭意图，即使随后恢复失败，下次启动也只恢复，不会重新接管。
托盘与 `aicat serve` 共用同一生命周期，运行期间的服务失败也会在关闭监听前尝试恢复。
进程内串行化启动、UI 开关与退出操作，避免启动覆盖用户刚关闭的意图。
拒绝授权、缺少 CA 信任或接管失败不会关闭本地中转；启动不会自动安装证书或引入特权 Helper。
恢复只覆盖仍匹配 aicat 目标字典的服务；外部修改保留，恢复记录保留供重试，UI 显示冲突。
匹配时，代理启用标志的缺省值与显式 `0` 均视为关闭，避免系统省略关闭字段后被误判为外部修改；
地址、端口、绕过列表等仍严格比较。原本未设置代理时，CLI 清空为关闭标志、空地址、0 端口；
仅这些空值与缺省等价，不忽略非空地址或有效端口的外部变化。

2026-10-16 用户授权在本机 Wi-Fi 直接实测 `networksetup` 接管到 9981 再恢复：6 条命令均成功，
每条约 30–70 毫秒，10 个网络服务的代理字典与测试前完全一致，用户确认没有密码/指纹弹窗。
因此移除代理写入的 `AuthorizationCreate`、`SCPreferencesCreateWithAuthorization`、原生提交代码，
不引入 Helper、sudo、AppleScript、AuthorizationDB 修改或私有 entitlement。
此结果只代表本机本账号，不保证其他账号、受管设备或系统版本免授权；失败直接报告，不隐藏提权回退。

`networksetup` 逐条提交，无法维持原生批量写入的跨进程原子性。每条命令前按稳定服务 ID、
当前服务名与位置重读核对，先原子保存 `progress.current` 与 `progress.pending` 两种可能结果，
再执行；返回后实读核验并推进日志。中断、非零返回但实际已写入、恢复中再次中断均可从日志恢复。
旧版没有 progress 的完整快照仍可恢复；不把意外的外部结果收编成自身进度。
接管前预检整个批次及反向恢复计划；恢复时允许日志中已记录的合法中间态，不要求再次恢复到该中间态。
原样保留 PAC URL、SOCKS 地址、绕过列表及未知字段，
仅切换 PAC/自动发现/SOCKS 启用标志。带认证的 HTTP/HTTPS 系统代理暂不自动接管，避免密码进入命令参数。
多个命令之间及检查与 OS 提交之间仍存在竞态窗口；遇到检测到的冲突立即停止并保留快照，不承诺全局锁。
正常退出或停止网关时若恢复失败，保留网关运行并报告错误。强制结束进程无法即时恢复，需重新启动 aicat。

Codex 接线仍支持显式 HTTP_PROXY/HTTPS_PROXY；系统代理是否被任意客户端采用不能由开关状态推断。
信任 CA 与代理配置不依赖私云登录。用户的 Codex 凭据仍只透传。

2026-10-16 对本机 `codex-cli 0.162.0-alpha.17.2` 的真实 CLI 与桌面 `app-server` 作对照：
清除所有代理环境变量、后端地址覆盖和 CA 注入，保留已开启的 macOS 系统代理与现有用户钥匙串信任。
默认 `respect_system_proxy=false` 时，模型请求仍直连；模型目录、分析事件等请求走代理不能证明模型请求也被接管。
只开启 Codex 原生 `features.respect_system_proxy=true` 后，真实模型响应通过 MITM，网关记录的 token 数与
Codex 完成事件相同，子进程 socket 也只连接代理。将该字段写入 Codex 配置后，普通 `codex app-server`
启动同样通过；无需专用启动文件，也无需 HTTP_PROXY/HTTPS_PROXY 或 SSL_CERT_FILE。
该版本将原生开关标为 `under development`。已有 Codex 桌面进程需正常重启加载配置。
系统代理 UI 的常规状态只显示网关地址。

端到端验证使用 `desktoptest` 构建：复用真实 UI API、代理恢复逻辑与网关，只替换 OS 命令执行与证书安装适配器（真实规划器、逐命令日志和恢复逻辑保持不变），
不改真实钥匙串或系统代理。真实 Codex 网络验证仅在子进程注入测试代理与测试 CA，单独报告结果。

参考：[Proxyman 自动安装](https://docs.proxyman.com/debug-devices/macos)、
[Apple 对非交互信任授权失败的说明](https://developer.apple.com/forums/thread/671582)。
