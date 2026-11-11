# 项目约束

- 本仓库只维护 aicat 桌面端；微信小程序不在本仓库范围内。
- 以当前设计和 `CONTEXT.md` 为准；旧 ADR 保留历史语境，不据此恢复已移除的业务。
- 功能开发只验证受影响的流程；全套 E2E 留到整轮开发结束、准备统一验收时执行，不因单个功能完成或小修重复跑全套。脚本仅支持全量时，先提供可独立运行的相关场景。
- 不要写单元测试。

用户功能、入口与验证路径见 [Feature map](docs/feature-map/README.md)，架构见 [领域文档](docs/agents/domain.md)，构建与安装见 [开发约定](docs/development.md)，需求跟踪见 [Issue tracker](docs/agents/issue-tracker.md)。
