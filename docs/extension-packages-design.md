# NPM 消息渠道包

流量与 Codex 模型路由是内置扩展；消息渠道以 NPM 包声明交付，可从 registry、公开 GitHub 仓库或安装包地址安装。推荐目录包括企业微信官方 OpenClaw BOT、`@aicat/wecom-local` 与 `@aicat/wpk`。后两个包分别在独立的 [aicat-wecom-local](https://github.com/fishioon/aicat-wecom-local) 与 [aicat-wpk](https://github.com/fishioon/aicat-wpk) 仓库维护，尚未发布到公网 registry；目录元数据不代表包已可下载。主仓库及应用资源只提供通用宿主，不包含 NPM 扩展源码或归档。企业微信官方包走专用 OpenClaw 兼容适配，平台 SDK 不作为通用原生宿主的顶层依赖。

## 安装与生命周期

`internal/channelplugin` 使用应用内置 Bun 安装固定版本，禁用生命周期脚本，校验包身份、入口、API 与锁文件摘要，在暂存目录完成后原子切换 current.json。失败或取消保留原版本；停用后可更新、卸载，卸载保留配置、凭据、Room 和包业务数据。安装进度与取消复用扩展宿主服务。

扩展侧边栏底部的整行「自定义安装」进入统一来源表单。一个输入框自动识别 NPM 包名、公开 GitHub 仓库和安装包 URL，「更多选项」仅提供版本。安装使用包内 `aicat.id`，不从包名猜测 ID；完整校验后登记到权限为 0600 的 channel-catalog.json。安装完成后保持停用，用户配置并验证后另行启用。不能覆盖内置扩展或改变已有渠道的包名、格式；为推荐的 aicat 渠道指定来源时必须保持同一包身份，不生成重复条目。

| 来源 | 示例 | 版本选择 |
| --- | --- | --- |
| NPM | `@publisher/channel`、`@publisher/channel@1.0.0` | 固定版本或 dist-tag，留空解析 `latest`，按 registry 提供的摘要校验安装包 |
| GitHub | `github:owner/repo`、`https://github.com/owner/repo.git#v1.0.0` | 分支、标签或 commit，留空使用默认分支；公开 API 解析 commit 后下载归档 |
| 安装包 | `https://example.com/extension.tgz` | 使用 URL 对应版本，包内读取版本，不接受独立版本参数，也不要求 URL 有文件后缀 |

自定义来源仅接受声明 aicat API 的渠道包。GitHub 仓库根目录须包含 package.json、渠道声明和可直接加载的入口，不执行 prepare、build 或其他生命周期脚本。安装包地址支持 HTTP/HTTPS；内网地址与临时授权下载链接按相同规则处理，不带 Authorization、Cookie，也不读取本机 npm/Git 登录凭据。401/403 明确提示暂不支持安装凭据，不提供账号、Token、登录或会员入口。URL 查询参数仅保存在私有目录记录中，扩展详情只显示去除查询参数的来源。

归档为单一根目录的 gzip tar，压缩体积最多 64 MiB、文件总大小最多 256 MiB、最多 10,000 个条目；拒绝越界路径、符号链接及特殊文件。所有源码、入口和 manifest 校验完成后才切换当前版本。自定义来源的安装记录保存归档 SHA-256、GitHub commit（适用时）和独立版本目录，支持包版本号相同但内容不同的手动更新。更新从原来源重新解析，失败保留旧安装。旧的 registry 固定版本记录继续可读。

安装与下载不发生在应用构建时。旧配置和 Room 保留在用户 Home，安装相同渠道包后由通用兼容迁移接续；启动不自动下载、更新或补装已卸载的扩展。

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

每渠道独立 Bun 进程和 state/<channel>，通过 stdio JSONL 的 channel_open/close/send/observe、channel_status 与 message_push 交接；凭据通过 stdin，session 与 source 由 Go 绑定。createChannel(api) 返回 open、close 与可选 send/observe。api 包含 configuration、stateDirectory、signal、status、publish、updateRoom 及已声明的 native context。

publish 先进入宿主持久待确认队列，实际 Room fsync 确认后 Promise 才完成。失败保留队列，重启重推；账号和来源变化不能重推到另一账号。单事件最多 256 KiB，待确认队列最多 32 MiB；协议帧有上限，进程独占锁防止并发宿主覆盖状态。插件控制采集游标，但不能自己写 Room 或创建 Agent。

`updateRoom` 通过相同队列提交会话名称、头像、类型和成员展示信息，独立于 `publish` 的聊天消息；不触发 Agent，不覆盖用户设置。数据字段与持久化语义见[消息接入规范](message-ingress-standard.md)。

## 能力

- Send 为可选平台外发，仍需 Room 允许且渠道启用。
- Hosts/Intercepts 精确声明代理观测与解密域名，Page/Routes 将采集入口限制在 /.aicat/<id>/，WebSocket 声明帧观测。统一 gateway addon 转交包入口，按大小/时间/队列上限交接；停用或无效采集路径本地拒绝，不转发给平台。
- prepare 需要明确 native 声明。当前原生能力 wecom-local 提供实际用户 Home、应用共享的原版 SQLite3MC 库，以及用户启用时的一次性准备。平台 Profile、查询、数据库解析、发送脚本均随 NPM 包维护；宿主仅提供能力与进程生命周期。

`statusMessages` 可按界面语言声明状态码提示，例如 `{"zh-Hans":{"READ_FAILED":"读取失败，请检查来源。"},"en":{"READ_FAILED":"Cannot read the source."}}`。文案随扩展包维护；宿主只校验声明并按当前界面语言显示，缺少翻译时保留原始状态码。最多 8 种语言、每种 128 条提示，单条文本最多 1024 字节。

NPM 扩展是受信任的本机可执行代码，并非沙箱。能力声明用于接缝和资源边界，不宣称隔离包自身的本机权限。

统一消息页面读取 Room 服务；扩展页只负责安装、连接、平台字段与诊断。完整数据和工具契约见[消息接入规范](message-ingress-standard.md)。

## 仓库与验收边界

aicat 的 `make verify` 和 `make e2e` 不依赖扩展源码仓库，使用合成渠道夹具验证 NPM 安装与公共消息链路。扩展仓库独立运行 `npm run check`、`npm pack --ignore-scripts` 与 `npm run e2e -- --aicat ../aicat`；后台 E2E 将本仓库的实际包交给本机 registry，通过 aicat 公共服务验证完整接入链路，工件保存在各自 `build/`。扩展专属测试不复制回主仓库。
