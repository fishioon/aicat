# 托盘进程自己承载 aicat，`aicat serve` 是同一份运行体的前台入口

状态：accepted；收口 [0016](0016-tray-app-with-capability-switches.md) 里「托盘进程负责长期运行，`aicat serve`
仍是实际承载能力的运行体」这句没有说清的话。

菜单栏一起来，托盘进程就调用与 `aicat serve` 相同的 `app.Serve(ctx)`：内置网关、File Server 与 Claw 都跑在这个
进程里。没有子进程、没有 LaunchAgent 托管的数据面、没有第二份 runtime 配置。`aicat serve` 保留，供前台运行、
无 GUI 环境与调试使用。

## Why

- aicat 的能力全是进程内的 Go：内置网关是 go-mitmproxy 的装配、CA 是 aicat Home 里的文件、Claw 是 in-process
  拉起 `codex app-server`。没有一样必须独立成进程，所以「数据面与 UI 解耦」这件事在 aicat 里没有对应的问题要解决。
- 单一状态源：Panel 上的网关开关与地址、Main Window 里的登录与能力状态，都由这个进程直接回答，不需要状态文件、
  端口探测或 IPC。开关写完 aicat Configuration 后由同一进程重新起停。
- aicat 已经为另一种做法付过一次代价：standalone agentgateway 加 `agentgateway.mode: child|launchd` 的
  LaunchAgent 协调（[0009](0009-launchd-agentgateway-lifecycle.md)）最终整体下线，理由是删掉外部依赖、
  LaunchAgent 与第二份配置（[0017](0017-builtin-gateway-for-codex-wiring.md)、
  [0018](0018-go-mitmproxy-entry-with-addon-business.md)）。
- magpie 已经把这套的工程细节做完并验证过：进程内起停、端口交接、开关写回配置后重启。

## Consequences

- 退出托盘应用就是停止 aicat；开机自启是托盘应用自己的事（LaunchAgent 只用来在登录时拉起托盘）。
- GUI 与能力的崩溃半径合并：WebView 异常退出会中断网关。用户把 Codex 的 `openai_base_url` 或代理指向 aicat 时，
  这意味着模型调用随 aicat 一起断。缓解是托盘随登录自启、崩溃后重新打开即可恢复；自动重启留待需要时再定。
- 端口所有权要显式处理：托盘与终端里的 `aicat serve` 可能同时想监听 `gateway.addr`。第一版由监听失败一方把错误
  报进 Panel 与日志；「谁先起谁服务、后者只显示状态、前者退出后自动接管」的交接按 magpie 的
  handover 形状后续补上。
- `app.Serve` 成为两个入口共用的运行体，凡是「在启动时做一次」的事都必须放在它里面，不能只写在 `serve` 命令里。
- `claw.agents` 非空时，Claw 的生命周期属于托盘进程，与 [0016](0016-tray-app-with-capability-switches.md) 的
  能力开关约定一致。
