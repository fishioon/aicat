# 官方渠道插件独立宿主原型

微信和企业微信官方插件已能共用同一套独立进程宿主，完成合成平台上的私聊文本收发。**企微插件仍会在宿主不回复、甚至关闭发送时自行发送“✅ 已收到”，因此发送策略检查未通过，不能直接作为 aicat 生产渠道。** 本目录不接入 aicat 主程序。

## 运行

需要 Node >=22.13.0 与 npm。仅安装这里的依赖，不安装完整 OpenClaw，也不执行依赖安装脚本：

```sh
cd /Users/fish/git/aicat/experiments/openclaw-channel-host
npm ci --ignore-scripts --legacy-peer-deps --no-audit --no-fund
npm run e2e
```

可分别运行 `npm run e2e -- weixin`、`npm run e2e -- wecom`。依赖版本由 package-lock.json 固定：微信 `@tencent-weixin/openclaw-weixin@2.4.10`，企业微信 `@wecom/wecom-openclaw-plugin@2026.10.30`。npm 的 legacy peer 模式用于避免把完整 OpenClaw 作为 peer 自动装进来；宿主只实现明确列出的兼容接口。

默认验收退出码 0 表示文本原型与恢复场景执行完成，**不表示策略兼容**。report.json 分开给出 `harnessPassed`、`textTransportPassed`、`policyCompatible` 和每项 policyChecks；企微的两项发送策略检查为 false，并保存实际出站帧。链路或恢复断言失败时退出码为 1。

每次运行生成 `artifacts/<UTC时间>/`，含报告、各次宿主的 stderr 日志、双向 NDJSON 记录、合成平台实际收到的 HTTP 请求/WebSocket 帧及隔离状态目录。报告记录依赖 integrity、插件运行前后文件摘要、共享宿主摘要、实际 PID 与各项断言。没有 UI，故不生成浏览器截图。artifacts 与 node_modules 不入库。

## 先验证的失败方式

- 插件能注册，但缺少 channelRuntime 或回复调度接口，无法收消息。
- 平台事件未通过官方插件进入宿主持久缓冲。
- 延迟回复丢失 context_token，或重复 requestId 再次发送。
- 平台已接受发送、宿主尚未保存回执时崩溃，恢复后错误重发。
- 重启后丢失缓冲、发送记录或微信回复上下文。
- 停用后长轮询、WebSocket 或后台计时器仍在运行。
- 插件日志混入 stdout，或访问真实平台、用户的 ~/.openclaw。
- 关闭显式发送后，插件自己的回复旁路发送开关。

E2E 会真实启动、停止和杀死宿主进程，经真实官方插件及 SDK 连接本地合成 HTTP/WebSocket 服务；不是直接调用 ingest、也不是 mock SDK 返回值。验收覆盖开启前无联网、拒绝非 loopback 端点、入站落盘、显式发送开关、延迟回复、重复及冲突 requestId、正常重启、重复入站去重、发送中途 SIGKILL 后恢复 unknown 且不重发，以及自然退出。企微自动确认作为真实失败项记录，不通过修改官方插件或过滤出站帧掩盖。

## 模块分工

| 文件 | 职责 |
| --- | --- |
| host.mjs | 通用进程入口、注册、生命周期、stdin/stdout RPC、发送回执 |
| runtime.mjs | 两个插件共用的路由、会话与入站 dispatch 兼容接口，无平台收发实现 |
| shim/sdk.mjs | 明确列出的 OpenClaw SDK 导出；缺失接口会失败，不用 Proxy 自动补空函数 |
| profiles.mjs | 固定插件入口与合成账号配置，平台差异集中在配置和凭据文件布局 |
| store.mjs | 宿主持久缓冲、去重、回执，原子替换与 fsync |
| network.mjs | 合成验收的网络边界，只允许指定的 127.0.0.1 端口 |
| fixtures.mjs | 合成平台服务；实现 HTTP 长轮询、WebSocket 鉴权/事件/回执，不属于运行宿主 |
| e2e.mjs | 外部进程验收控制器与工件记录 |

```text
验收控制器 ── stdin/stdout ── 通用宿主 + SDK 兼容层
                                  │
                             官方插件原包
                                  │
                     本地合成 HTTP / WebSocket 平台
```

## 原型协议

进程入口为 `node host.mjs <隔离状态目录> --synthetic`，运行目录必须专用；主程序没有引用它。每行一个 JSON 对象，请求为 `{id, method, params}`，响应为 `{id, result}` 或 `{id, error}`。这是原型私有协议，**不是 MCP，也不是设计草案的 channel v1 实现**。

- `open`：`{kind: "weixin"|"wecom", endpoint, send: boolean}`；只接受本地合成端点，账号凭据为自动生成的合成值。
- `read`：`{cursor?: number}`，返回已持久化的插件上下文和 nextCursor，不启动 Agent。
- `send`：`{requestId, to, text}` 调用官方 outbound.sendText；`{operation:"lookup",requestId}` 只查回执。
- `setSend`：`{enabled:boolean}`，控制宿主发起的发送，不能保证约束插件自行发送。
- `status`：状态、兼容接口调用次数、未运行注册项和网络尝试。
- `stop`：取消账号、调用插件停止入口并等待任务结束，然后关闭 stdin 等待进程自然退出。

发送前先持久保存 pending，同 ID 同负载返回原状态，负载不同拒绝。重启后 pending 转 unknown，不自动重发。仍不知道上游是否送达的情况保持 unknown。

## 已确认的边界

- 两个插件在各自独立进程中运行，但使用完全相同的 host.mjs、runtime.mjs 和 SDK shim。平台登录连接与发送代码没有复制到宿主。
- 官方插件的公共工具、CLI、Webhook 与 prompt hooks 仅记录为未运行注册项，不暴露、不执行，不宣称兼容。普通文本之外的媒体接口显式拒绝。
- 企微在 dispatch 返回后结束回复流，即使没有产出也自行发送“✅ 已收到”；sendThinkingMessage=false 不能阻止此行为。要让 aicat 完全控制发送，需上游提供关闭选项、受控补丁或采用更低层的官方 SDK，此原型不替用户定案。
- 微信 2.4.10 提前保存平台游标的窗口仍存在。已验证的是消息进入宿主缓冲后的恢复，未验证、也不保证平台确认到进入 dispatch 之间绝不丢消息。
- 微信测试验证了插件自己的 context_token 跨进程重启恢复；企微重启后验证的是官方主动发送，不等于恢复旧 req_id 的被动回复流。
- Node 的网络拦截用于避免本次验收误连公网，不是对恶意原生代码的 OS 沙箱。所有凭据、消息和地址均为合成数据；未登录真实账号、未启动浏览器或原生窗口、未修改证书或系统代理。
- 真实扫码、平台权限与鉴权、真实送达、群聊、媒体、OAuth 工具、完整目录、完整 OpenClaw 兼容和生产分发尚未验证。

## 通用宿主与升级成本研究

**结论：可维护一个受限的通用宿主，固定支持若干插件版本；目前证据不支持“任意官方渠道插件安装即用、升级无需适配”。** 纯文本 SDK 接口大多能共享，主要成本集中在宿主能力补全、消费/回复语义和版本验收。官方插件文件不需要修改，不等于不需要维护兼容层。

### 代码成本的口径

当前共享 runtime.mjs 为 47 行、shim/sdk.mjs 为 30 行非空非注释代码，共 77 行；它们只覆盖原型的文本子集，包含未运行能力的显式拒绝，不能用这 77 行估算完整 OpenClaw 宿主。profiles.mjs 共 28 行，其中包括入口解析、版本声明，以及微信的合成凭据文件布局和企微连接配置。宿主不包含平台网络协议实现。

在这套冻结宿主上，按插件全包的字面 SDK 引用统计如下。计数包括常量和函数，动态命名空间、计算属性和内部别名可能漏计；“缺失”也包括仅用于未测群聊、媒体、OAuth 等能力的接口，不能当作文本链路一定要实现的数量。

| 插件样本 | SDK 模块路径 / 静态命名导出项 | 当前差额与实测成本 |
| --- | --- | --- |
| 微信 2.4.10 | 8 / 10 | 当前文本场景无需平台专属 runtime；凭据布局与配置仍专属微信 |
| 企微 2026.10.30 | 5 / 3，另有动态命名空间探测 | 当前文本场景无需新增 runtime；静态扫描还能识别出媒体、配对、格式化等未覆盖能力，企微自身也带部分 fallback |
| 飞书 2026.7.16 | 18 / 31，其中 26 项当前 shim 未导出 | 通用加载器、SDK 根导出别名、两个注册收集接口即可完成注册；完整消息和工具链仍需补 runtime 与导出，不能由注册成功推算工作量 |

企微的自动确认来自插件内部的回复流收尾，不是缺一个 SDK 导出。延迟完成 dispatch 可以推迟收尾，但会改变读取与排队语义：微信 2.4.10 的 monitor 等待 processOneMessage 后才继续，企微同一会话任务串行。不能将无限挂起 dispatch 当作实现“只读采集且不回复”的通用方案。仍需验证完整回复生命周期，或要求插件提供明确的 silent/外部编排能力。

### 冻结宿主的跨版本结果

以下实验只改插件版本元数据，通用 host/runtime/SDK 源码保持不变；每个副本保留独立 package-lock、执行日志和报告。主原型没有切换依赖。

| 样本 | 实测结果 |
| --- | --- |
| 微信 2.4.8 | 加载失败，缺少 withFileLock；静态比对另识别出两个旧版鉴权导出 |
| 微信 2.4.10 | 基线文本与恢复通过，发送策略通过；不覆盖平台确认到 dispatch 的崩溃窗口 |
| 微信 3.0.0-beta-0 | 能加载并接收平台事件，但处理文本时要求 channelRuntime.media.saveMediaBuffer，冻结宿主失败 |
| 企微 2026.10.19 / 2026.10.26 / 2026.10.30 | 三个版本文本与恢复链路通过，不需修改宿主源码；三个版本的自动确认均不满足宿主发送开关 |
| 飞书 2026.7.16 | 原生加载先缺 SDK 根导出；只补根导出后暴露 CJS/ESM 混用错误，尚未进入业务逻辑 |
| 飞书 2026.8.5-beta.0 | 同样在原生加载阶段出现 CJS/ESM 混用；未启动账号 |

冻结版本实验包含失败样本，matrix-report.json 不把失败当作兼容通过。三个渠道共检查 9 个版本的静态引用；两个旧企微版本运行了独立文本 E2E，其余动态样本按表中的边界判断。

### 最小兼容增量实验

兼容增量只应用到 artifacts 内的候选副本，未改官方文件，也未把候选实现并入主宿主。

- **微信 beta**：向共享 runtime 增加媒体保存接口，共 11 行非空代码（含两个 import）；文本、持久化、发送去重和崩溃恢复场景通过，官方包字节一致。文本路径只要求该接口存在，本次没有实际下载媒体，因此不能宣布完整媒体兼容。这个接口可供其他渠道复用。
- **微信旧版**：添加 11 行旧鉴权子集和显式拒绝配对写入的导出后，可以加载、收发并正常重启，但重复入站去重断言失败。旧版没有 MessageSidFull，宿主收到的是新生成的 MessageSid；仅靠补 SDK 函数不能恢复已经丢掉的稳定 ID。本实验不提供完整旧鉴权/配对支持，建议设最低支持版本，而不是无限兼容旧版。
- **飞书**：用通用 `jiti@2.7.0` 加载原包、添加 SDK 根导出别名和 registerCli/registerCommand 两个登记接口，完成 channel、工具、hook、命令注册，网络尝试为零，官方包字节一致。没有按飞书文件名改写源码；加载器做通用模块格式转换。尚未启动飞书账号，26 个未导出项在 CommonJS 加载时不会全部立即报错，后续执行仍可能失败。

静态导出列表相同不保证行为一致：微信 beta 的新增要求是先取函数引用，不是直接调用，字面调用扫描没有捕获；微信旧版的数据字段差异也不会表现为多一个 SDK import。因此静态审计只能预警，不能代替 E2E。

### 维护方式建议

可承诺的支持单元应是“插件包精确版本 + 依赖锁文件 + 宿主兼容档位 + 已验证功能范围”，不能只保存包名。升级先进入候选副本，检查模块导出、账号/消息上下文、能力和配置变化，再运行受影响的接收、排队、回复与恢复场景，确认后才放入支持列表。beta 与 stable 分开，不自动跟随 npm latest。

可共享的 SDK 能力集中实现；少量插件差异限定在声明、配置与凭据导入、能力映射里。不要以给每个插件伪造一套 OpenClaw runtime 或按官方源码片段打运行时补丁的方式维持“通用”表象。宿主缺失能力显式拒绝，平台自己缺失的稳定 ID、历史范围或 silent 行为不能用猜测补齐。

建议先限定私聊文本，微信以 2.4.10 为基线、beta 单独验收，企微先明确是否接受自动确认；飞书再验证真实消息链路后评估是否值得补全。若要求统一的只读采集、严格关闭发送、完整消息归档与恢复保证，当前这些插件不能直接给出共同承诺，应比较“通用宿主 + 明确受限渠道”与“平台官方 SDK + 少量适配”两条路线，而不预先确定最终模型。

这里不估算完整渠道需要多少人日：当前只测了文本或注册，没有媒体、群聊、OAuth 的执行证据。代码行数记录的是本轮实际增量，不是剩余实现量或未来每次升级成本。

### 重跑研究

```sh
python3 compat-audit.py --out artifacts/compatibility-study
python3 upgrade-matrix.py --out artifacts/compatibility-study/matrix
python3 candidate-matrix.py --out artifacts/compatibility-study
```

静态审计只下载并检查发布包，校验 SHA-512，不执行插件。升级矩阵安装依赖时禁用 scripts，并使用 legacy peer 模式；动态验收只访问本地合成端点，注册探针不启动账号。候选实验依赖先运行升级矩阵，所有副本、锁文件和结果位于 artifacts/compatibility-study。

输出入口为 audit-report.json（导出和字面调用差额）、matrix/matrix-report.json（冻结宿主结果）、candidate-report.json（兼容增量结果）。它们分别代表静态覆盖、执行结果和候选修复边界，不能混为“全功能兼容”。

相关一手资料：[OpenClaw runtime 能力](https://docs.openclaw.ai/plugins/sdk-runtime/channel)、[插件加载流水线](https://docs.openclaw.ai/plugins/architecture-internals/load-pipeline)、[微信 SDK 子路径变动报告](https://github.com/Tencent/openclaw-weixin/issues/274)、[飞书 SDK 导出变动报告](https://github.com/larksuite/openclaw-lark/issues/598)。Issue 是上游用户报告；本轮具体结论以本地固定版本实验为准。
