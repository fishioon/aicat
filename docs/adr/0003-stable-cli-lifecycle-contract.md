# aicat CLI 将 stdout 保留给稳定生命周期契约

状态：已被 ADR 0004 的 Go `exec` launcher 决策取代。本文保留为历史记录，不再描述当前实现。

aicat CLI V1 只把 help、version、Ready Announcement 和 Stop Announcement 写入 stdout，把 progress、warning、Failure Report 与 embedded agentgateway diagnostics 写入 stderr；TTY 与 pipe 使用同一无 ANSI 文本。退出码只定义 `0`（成功或正常 stop）、`1`（operational failure）和 `2`（usage error），`SIGINT`/`SIGTERM` 在 cleanup 成功后返回 `0`。这让 Codex 和 shell 可以用精确 stdout 行建立启动与清理 barrier，同时避免为 V1 引入 JSON event protocol 或含糊的细粒度 `sysexits` taxonomy。

## Consequences

aicat host 必须拥有 process-global logging，把 embedded runtime tracing 固定到 stderr，并避免调用会写 stdout、打印完整配置或自行拥有 signal 的 standalone agentgateway runner。Ready 写入失败会触发 cleanup 并返回 `1`，而不是继续运行一个调用者无法可靠观察的服务；Ready 或 Stop 也不能取代最终 exit status。
