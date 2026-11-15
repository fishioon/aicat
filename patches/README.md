# 临时代理补丁

`go-mitmproxy-v1.9.4-websocket.patch` 用于当前 WPK 采样，尚未提交上游。`go.mod` 和模块缓存保持官方 v1.9.4；`scripts/proxy-go.py` 将官方源码复制到忽略的 `build/proxy-websocket-fix/`，应用补丁，再使用独立 modfile 编译。Make 默认使用该入口，直接调用 Go 时使用 `python3 scripts/proxy-go.py <Go 参数>`。`--print-modfile` 显示临时 modfile。

补丁串行化同一连接的消息分发，保留旧 `WebSocketMessage(*Flow)`，为实现 `WebSocketMessageReceived(*Flow, *WebSocketMessage)` 的 addon 直接传递本次消息。分发完成后历史最多保留 256 条且不超过 1 MiB；超大的消息正常转发和通知，但不保留在历史中。`Options.WebSocketMaxMessages` 和 `WebSocketMaxBytes` 可调整限制，负数关闭历史保留，零采用默认值。正在分发的当前消息不计入历史限额。

`Snapshot()` 提供同步、复制后的历史，`MessageCount()` 保持累计计数。Web UI 使用累计计数作为消息索引。转发结束前关闭连接并等待两个方向退出，再发送 End 事件。

独立后台 E2E：

```sh
python3 scripts/proxy-go.py build -race -tags e2e -o bin/websocket-e2e e2e/websocket.go
bin/websocket-e2e build/websocket-<run>
python3 scripts/proxy-go.py build -race -tags e2e -o bin/wpk-e2e e2e/wpk.go
bin/wpk-e2e build/wpk-<run>
```

场景使用隔离的本机 HTTP/TLS 服务，不访问真实 WPK，不操作系统代理、钥匙串或原生窗口。原版对照仅编译 `e2e/websocket.go`，使用普通 `go build -race -tags e2e`。上游提供相同能力后可移除临时补丁与构建包装。
