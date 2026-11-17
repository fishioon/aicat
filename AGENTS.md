# 项目约束

- 后续新增和修改代码不写解释性注释；编译指令等必需注释除外。

- 本仓库只维护 aicat 桌面端与内置扩展。NPM 扩展的实现、打包与专属验收放在独立仓库，不将其源码或归档加入本仓库或应用资源；本仓库只保留通用宿主、接入协议和目录元数据。微信小程序不在本仓库范围内。
- 领域语言以 `CONTEXT.md` 为准，模块边界以 [架构](docs/architecture.md) 为准；文档只描述当前状态，不记录变更过程。
- 功能开发只验证受影响的流程；全套 E2E 留到整轮开发结束、准备统一验收时执行，不因单个功能完成或小修重复跑全套。脚本仅支持全量时，先提供可独立运行的相关场景。
- 不要写单元测试。

## 按需读取

本文件只保留每次必读的硬约束；具体规则单独成文件，命中触发条件时再读。

| 何时读 | 读什么 |
| --- | --- |
| 新增或修改消息接入插件 | [消息接入规范](docs/message-ingress-standard.md)：插件提交消息 → aicat 写入 Room → 驱动 Agent → 显式调用 MCP 回复 → aicat 交回插件；接入完成必须有整条链路的定向 E2E 证明 |
| 改动界面文字、语言切换或无障碍属性 | [UI 文案](docs/agents/rules/ui-copy.md) |
| 改动 CSS、组件样式或主题色 | [样式](docs/agents/rules/ui-style.md) |
| 重启桌面端、网关或替换构建产物 | [重启 aicat](docs/agents/rules/restart-aicat.md) |
| 运行或编写 E2E 验收场景 | [E2E 后台运行](docs/agents/rules/e2e-background.md) |
| 新增、复查或淘汰项目规则 | [规则索引与维护](docs/agents/rules/INDEX.md)（WorkBuddy 可由 `project-rule` skill 自动触发） |

新增具体规则一律走上述 skill，不追加到本文件；规则失效后按索引标 `deprecated` 再删除。

用户功能、入口与验证路径见 [Feature map](docs/feature-map/README.md)，领域文档的用法见 [Domain docs](docs/agents/domain.md)，构建与安装见 [开发约定](docs/development.md)，需求跟踪见 [Issue tracker](docs/agents/issue-tracker.md)。
