# 企业微信客户端

`@aicat/wecom-local` 是独立 NPM 消息渠道，由 [aicat-wecom-local](https://github.com/fishioon/aicat-wecom-local) 维护。读取本地客户端会话，确认 Room 落盘后推进游标，并按 Room 授权通过客户端脚本发送文本。脚本完成只代表客户端执行确认。

## Sub-features

- 扩展实现、打包、业务规则与专属验收由外部仓库维护。
- aicat 提供 NPM 安装、通用宿主、Room 持久化、Agent 调度与显式 MCP 回复。
- 当前包尚未发布到公网 NPM；推荐目录仅保留元数据，新安装需要可用的外部 registry 版本。主仓库及应用资源不提供源码或归档。

## How to get to it (user POV)

扩展 → 推荐 → 企业微信客户端。包可下载后安装并启用；平台配置在扩展详情中，消息、自动处理与发送能力在「消息」对应 Room 中管理。已有安装、配置和 Room 历史保留在用户 Home。

## Driving it with Playwright and native harness

在 [扩展仓库](https://github.com/fishioon/aicat-wecom-local) 运行：

```sh
npm run e2e -- --aicat ../aicat
```

实际扩展包由本机 NPM registry 提供，使用隔离 Home、后台 aicat 和 headless Chromium 验证。完整业务行为及定向场景见扩展仓库的 [FEATURES.md](https://github.com/fishioon/aicat-wecom-local/blob/main/FEATURES.md)。报告、日志与截图保存在扩展仓库的 `build/`。

## Gotchas

- 主仓库的 `make e2e` 验证通用宿主与合成渠道，不代替本扩展的完整接入验收。
- 包发布、私有仓库访问与 NPM registry 权限独立管理；仓库存在不表示包已发布。
- 模型和平台协议夹具不证明真实模型能力、真实账号送达或原生窗口交互。
