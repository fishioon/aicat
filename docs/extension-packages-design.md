# NPM 消息渠道包

流量与 Codex 模型路由是内置扩展；消息渠道通过 NPM 安装。推荐目录包括企业微信官方 OpenClaw BOT、`@aicat/wecom-local` 与 `@aicat/wpk`。后两个包分别在独立的 [aicat-wecom-local](https://github.com/fishioon/aicat-wecom-local) 与 [aicat-wpk](https://github.com/fishioon/aicat-wpk) 仓库维护，尚未发布到公网 registry；目录元数据不代表包已可下载。主仓库及应用资源只提供通用宿主，不包含 NPM 扩展源码或归档。企业微信官方包走专用 OpenClaw 兼容适配，平台 SDK 不作为通用原生宿主的顶层依赖。

## 安装与生命周期

`internal/channelplugin` 使用应用内置 Bun 安装固定版本，禁用生命周期脚本，校验包身份、入口、API 与锁文件摘要，在暂存目录完成后原子切换 current.json。失败或取消保留原版本；停用后可更新、卸载，卸载保留配置、凭据、Room 和包业务数据。安装进度与取消复用扩展宿主服务。

扩展列表的「安装 NPM 渠道」允许登记声明 aicat API 的第三方包，无需修改或重编宿主。包名、固定版本、registry 与派生渠道 ID 校验后持久登记于 channel-catalog.json。不能覆盖推荐来源或改已有包身份。所有 NPM 渠道均从声明的 registry 安装，不在应用构建时打包或安装。旧配置和 Room 保留在用户 Home，安装相同渠道包后由通用兼容迁移接续；启动不自动补装已卸载的扩展。

## 原生声明

```json
{
  "name":"@example/chat",
  "version":"1.0.0",
  "type":"module",
  "aicat":{
    "api":1,"id":"example.chat","entry":"index.mjs",
    "identityFields":["account"],
    "fields":[{"key":"account","type":"string","label":"账号","required":true}],
    "capabilities":{"send":true},
    "room":{"mode":"conversation","trigger":{"all":true}}
  }
}
```

入口及可选 prepare 都必须是包内常规文件，解析符号链接后不能越界。fields 支持 string/directory/boolean/secret；公开字段存 extensions.<id>.settings，secret 保存在私有 credentials.<values>，空值保留现有凭据。身份字段只能引用必填公开文本字段，不能把 model、prompt 或 Agent 配置塞进渠道字段。

Room 默认模板可以描述业务 prompt、上下文及处理模式，但不能自行开启自动处理或外发。首次创建 Room 时写入，包升级不覆盖用户配置。

## 宿主协议

每渠道独立 Bun 进程和 state/<channel>，通过 stdio JSONL 的 channel_open/close/send/observe、channel_status 与 message_push 交接；凭据通过 stdin，session 与 source 由 Go 绑定。createChannel(api) 返回 open、close 与可选 send/observe。api 包含 configuration、stateDirectory、signal、status、publish 及已声明的 native context。

publish 先进入宿主持久待确认队列，实际 Room fsync 确认后 Promise 才完成。失败保留队列，重启重推；账号和来源变化不能重推到另一账号。单事件最多 256 KiB，待确认队列最多 32 MiB；协议帧有上限，进程独占锁防止并发宿主覆盖状态。插件控制采集游标，但不能自己写 Room 或创建 Agent。

## 能力

- Send 为可选平台外发，仍需 Room 允许且渠道启用。
- Hosts/Intercepts 精确声明代理观测与解密域名，Page/Routes 将采集入口限制在 /.aicat/<id>/，WebSocket 声明帧观测。统一 gateway addon 转交包入口，按大小/时间/队列上限交接；停用或无效采集路径本地拒绝，不转发给平台。
- prepare 需要明确 native 声明。当前原生能力 wecom-local 提供实际用户 Home、应用共享的原版 SQLite3MC 库，以及用户启用时的一次性准备。平台 Profile、查询、数据库解析、发送脚本均随 NPM 包维护；宿主仅提供能力与进程生命周期。

`statusMessages` 可按界面语言声明状态码提示，例如 `{"zh-Hans":{"READ_FAILED":"读取失败，请检查来源。"},"en":{"READ_FAILED":"Cannot read the source."}}`。文案随扩展包维护；宿主只校验声明并按当前界面语言显示，缺少翻译时保留原始状态码。最多 8 种语言、每种 128 条提示，单条文本最多 1024 字节。

NPM 扩展是受信任的本机可执行代码，并非沙箱。能力声明用于接缝和资源边界，不宣称隔离包自身的本机权限。

统一消息页面读取 Room 服务；扩展页只负责安装、连接、平台字段与诊断。完整数据和工具契约见[消息接入规范](message-ingress-standard.md)。

## 仓库与验收边界

aicat 的 `make verify` 和 `make e2e` 不依赖扩展源码仓库，使用合成渠道夹具验证 NPM 安装与公共消息链路。扩展仓库独立运行 `npm run check`、`npm pack --ignore-scripts` 与 `npm run e2e -- --aicat ../aicat`；后台 E2E 将本仓库的实际包交给本机 registry，通过 aicat 公共服务验证完整接入链路，工件保存在各自 `build/`。扩展专属测试不复制回主仓库。
