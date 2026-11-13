# 官方渠道插件通用独立宿主

微信、企业微信和飞书的官方插件共用同一套独立进程宿主，已通过本地合成平台的私聊文本收发与恢复验收。官方插件文件保持原包，aicat 主程序没有接入本目录。

**企微仍会自行发送“✅ 已收到”，包括宿主发送开关关闭时。** 因此文本链路通过与严格发送策略兼容是两项独立结果；原型保留此失败项，不拦截或伪造平台出站帧。

## 运行与工件

验收驱动与依赖准备需要 Node >=22.13.0 与 npm，当前对照使用 Node v26.13.0；渠道宿主本身还验证了 Bun 1.4.4。仅安装本目录的依赖，不安装完整 OpenClaw，不执行依赖安装脚本：

```sh
cd /Users/fish/git/aicat/experiments/openclaw-channel-host
npm ci --ignore-scripts --legacy-peer-deps --no-audit --no-fund
npm run e2e
```

可独立运行 `npm run e2e -- weixin`、`npm run e2e -- wecom`、`npm run e2e -- feishu`。它们只验证本原型，不启动 aicat、浏览器或原生窗口，不运行仓库全套 E2E。

| 渠道 | 固定官方插件 | 加载方式 | 合成平台连接 |
| --- | --- | --- | --- |
| 微信 | `@tencent-weixin/openclaw-weixin@2.4.10` | Node/Bun 原生加载 | 官方插件 HTTP 长轮询及发送 API |
| 企业微信 | `@wecom/wecom-openclaw-plugin@2026.10.30` | Node/Bun 原生加载 | 官方 SDK WebSocket、JSON 事件和发送回执 |
| 飞书 | `@larksuite/openclaw-lark@2026.7.16` | Node：Jiti；Bun：原生加载 | 官方 SDK 发现连接端点、接收 protobuf 事件及调用消息 API |

依赖由 package-lock.json 固定；legacy peer 模式避免自动安装完整 OpenClaw。所有运行端点必须是指定端口的 127.0.0.1，不提供真实扫码或账号登录入口。飞书使用插件支持的自定义 domain 配置连接合成服务，没有按飞书源码片段替换网络调用。

默认验收退出码 0 表示链路与恢复断言执行成功，不表示全部发送策略兼容。report.json 分开给出 `harnessPassed`、`textTransportPassed`、`policyCompatible` 和逐项 `policyChecks`；企微两项策略检查为 false，并保存实际回复帧。链路或恢复断言失败时退出码为 1。

每次运行生成 `artifacts/<UTC时间>/`，含报告、每次宿主的 stderr、双向 NDJSON、合成平台收到的 HTTP 请求/WebSocket 帧及隔离状态。报告记录固定依赖 integrity、插件前后摘要、host/runtime 摘要、实际加载方式、PID 和断言。没有 UI，故不生成截图。artifacts 与 node_modules 不入库。

## Node / Bun 对照

使用同一个 Node 验收驱动、合成 HTTP/WebSocket 平台、锁定依赖树和宿主代码，只替换宿主子进程的可执行文件。原型没有增加 Bun 特判或修改官方插件。

```sh
# 必须使用 Node 运行比较驱动；第二个参数是实际 Bun 二进制的绝对路径。
node runtime-compare.mjs "$(command -v bun)"
# 单独比较飞书
node runtime-compare.mjs "$(command -v bun)" feishu
# 只运行某运行时下的宿主场景
node e2e.mjs all --runtime "$(command -v bun)"
```

当前结果为 macOS arm64、Node v26.13.0 对照 Bun 1.4.4，三个固定插件版本与上表一致：

| 渠道 | Node 断言 | Bun 断言 | Node / Bun 加载 | 策略兼容 |
| --- | --- | --- | --- | --- |
| 微信 | 27/27 | 27/27 | native / native | 两者通过 |
| 企业微信 | 26/26 | 26/26 | native / native | 两者都有“✅ 已收到”旁路发送问题 |
| 飞书 | 28/28 | 28/28 | jiti / native | 两者通过 |

本轮工件：[对照汇总](artifacts/runtime-compare-2026-11-13T05-13-22-569Z/summary.md)、[完整报告](artifacts/runtime-compare-2026-11-13T05-13-22-569Z/report.json)。每次比较写入 artifacts/runtime-compare-<UTC时间>/，包含代码摘要、运行时二进制摘要、平台信息、相同插件/断言核对、子报告路径、stdout/stderr、策略失败证据和存活 PID 检查；子报告目录保留双向协议、平台帧和私有状态。所有 18 个宿主 PID 均退出。

两个运行时各通过 81 条链路与恢复断言；企业微信各有两项策略检查失败，不能把比较驱动退出码 0 当成完全策略兼容。没有发现 Bun 新增的阻断性问题，飞书在 Bun 下不触发 Jiti 回退；Jiti 依赖尚未移除，这不是验证其可以从包中删除。

设计阶段优先选择兼容 Bun 作为后续宿主运行时；设计采用随 app 打包的 Bun，不自动下载运行时；此原型命令仍使用本机 Bun 进行验证，保留 Node 对照入口。该结论仅覆盖三个固定版本的私聊文本、延迟发送、发送去重、重启/崩溃恢复、stdio 与正常退出。没有做速度或体积基准；单次场景耗时不能用于宣称 Bun 更快。共享多账号、真实登录/鉴权、附件、生产打包、签名、Bun 单文件编译及独立分发目录加载仍待验证。

当前原型仍是 .mjs，不因选择 Bun 改写成 TS。Bun 后续可直接加载 TS，类型检查独立执行。主程序当前不提供旧的单文件请求改写；此原型只验证渠道宿主，不提供改写执行入口。

## 失败方式与覆盖

实现和场景围绕这些失败方式组织，不编写单元测试：

- 插件能注册，却因缺少 channelRuntime、配置或回复接口而不能接收消息。
- 飞书原包 CJS/ESM 混用导致加载失败；仅注册成功却无法连接真实 SDK 的合成平台端点。
- 入站未通过官方 SDK 进入宿主持久缓冲，或者发送只在宿主内部返回伪造成功。
- 延迟回复丢失 context_token，重复 requestId 二次发送，或相同 ID 的不同内容覆盖原请求。
- 平台已接收发送、宿主尚未保存回执时崩溃，恢复后错误重发。
- 重启丢失消息、发送回执或微信回复上下文；同一平台消息被归档两次。
- 停用后长轮询、WebSocket 或后台计时器仍运行；日志混入 stdout 协议。
- 插件误连真实平台、读写用户 ~/.openclaw，或插件自行回复绕过发送开关。

E2E 会启动真实宿主子进程，通过官方插件及 SDK 连接本地 HTTP/WebSocket 服务；不是直接调用 ingest，也不 mock SDK 返回值。覆盖开启前无联网、非 loopback 拒绝、入站落盘、显式发送开关、延迟回复、重复与冲突 requestId、正常重启、重复入站、发送中途 SIGKILL 后 unknown 且不重发、自然退出和退出后无请求。飞书另断言 SDK 的端点发现、二进制帧确认和正确收件人。

## 模块职责

| 文件 | 职责 |
| --- | --- |
| host.mjs | 通用注册、生命周期、stdin/stdout RPC、发送回执 |
| loader.mjs | 原生模块加载；CJS/ESM 格式问题使用通用 Jiti fallback，不修改官方文件 |
| runtime.mjs | 共享配置读取、路由、会话、消息封装、文本分块和入站 dispatch；不实现平台协议 |
| shim/sdk.mjs、shim/text.mjs | 已支持的 SDK 导出、文本、会话读取、私聊命令授权和历史辅助；不授予 Agent 控制命令权限 |
| profiles.mjs | 固定版本与入口解析、合成账号配置和凭据布局；不复制平台收发实现 |
| store.mjs | 宿主持久缓冲、去重与回执；原子替换及 fsync |
| network.mjs | 合成验收的网络边界，只允许指定的本机端口 |
| fixtures.mjs、feishu-frame.mjs | 合成平台与 protobuf 帧编码，仅用于验收；解码、事件分派和发送由官方 SDK 执行 |
| e2e.mjs | 外部进程验收与工件记录 |

```text
验收控制器 ── stdin/stdout ── 共享宿主 + SDK 适配层
                                  │
                         微信 / 企微 / 飞书官方插件
                                  │
                      本地合成 HTTP / WebSocket 平台
```

所有渠道共用同一份 host、loader、runtime 与 SDK shim；差异集中在声明和配置，以及模拟各自服务端协议的验收 fixture。

## 原型协议

入口为 `node host.mjs <隔离状态目录> --synthetic`，目录必须专用。每行一个 JSON 对象：请求 `{id, method, params}`，响应 `{id, result}` 或 `{id, error}`。这是私有原型协议，不是 MCP，也不是设计草案的完整 channel v1 实现。

- `open`：`{kind: "weixin"|"wecom"|"feishu", endpoint, send: boolean}`；只接受本机合成端点，凭据自动生成且无真实账号含义。
- `read`：`{cursor?: number}`，返回已持久的插件上下文及 nextCursor，不启动 Agent。
- `send`：`{requestId, to, text}` 调用官方 outbound.sendText；`{operation:"lookup",requestId}` 只查回执。
- `setSend`：`{enabled:boolean}`，控制宿主发起的发送，不能约束企微插件自行确认。
- `status`：状态、共享接口调用计数、未运行注册项与网络尝试。
- `stop`：取消账号、调用官方停止入口、等待任务结束，关闭 stdin 后让进程自然退出。

发送前先持久保存 pending。同 ID 同负载返回原状态，负载不同拒绝；重启后 pending 转 unknown，不自动重发。

## 当前边界

- 只验证私聊文本。真实扫码、平台鉴权/送达、群聊、附件、OAuth、交互卡片、完整联系人目录和生产分发未验证。
- 插件附带的工具、CLI、命令、Webhook、prompt hooks 仅登记为未运行能力，不执行、不暴露；命令授权不放行。当前 SDK 导出是支持范围明确的子集，不是完整 OpenClaw runtime。
- 企微自动确认是插件自身行为，sendThinkingMessage=false 无法阻止。文本传输与严格只读策略必须分别评估。
- 微信 2.4.10 提前保存平台游标的窗口仍存在。已验证的是消息进入宿主缓冲之后的恢复，不保证平台确认到 dispatch 之间绝不丢消息。
- 微信验证了插件保存的 context_token 重启恢复；企微验证的是重连后主动发送，飞书是新连接上的消息 API 发送，均不等于恢复旧的被动回复流。
- 网络拦截是合成验收边界，不是针对恶意代码的 OS 沙箱。原型没有接触真实凭据、证书、系统代理或桌面窗口。

## 兼容研究工具

兼容成本按“精确插件版本＋依赖锁＋宿主接口＋已验证功能”评估。不同版本可能改变 SDK 路径、初始化行为、稳定消息 ID 或回复语义，导出列表相同也不能免除场景验收。

```sh
python3 compat-audit.py --out artifacts/compatibility-study
python3 upgrade-matrix.py --out artifacts/compatibility-study/matrix
python3 candidate-matrix.py --out artifacts/compatibility-study
```

审计检查发布包的字面导入和调用，不是完整调用图；其数量包括常量及可选功能，不能直接当作新增函数数或工作量。矩阵在独立副本中固定版本并保留锁文件；候选实验只修改这些副本的宿主接口，不修改官方插件。它们用于研究具体失败边界，不是日常三渠道验收入口；旧工件反映其保存的宿主快照，不代表当前适配层仍缺少同样的接口。
