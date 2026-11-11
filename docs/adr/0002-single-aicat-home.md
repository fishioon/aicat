# aicat V1 使用单一 `~/.aicat/` home

aicat CLI V1 把 Runtime Configuration、CA identity 和 Request History Store 统一放在当前用户的 `~/.aicat/`，而不延续 macOS app 的 Application Support 路径，也不按 XDG 拆分 config/data roots。这个固定、可从 binary name 推导的 CLI home 让用户和自动化 agent 只有一个检查与备份入口，并避免把 SQLite 和 CA 误归类为纯配置；代价是该路径成为 V1 的稳定产品契约，未来迁移必须显式设计。aicat 使用 `exec` 启动 runtime，不创建 Serve Session Lock、PID file 或 persistent text log。
