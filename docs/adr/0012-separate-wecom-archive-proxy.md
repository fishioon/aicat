# 企业微信会话存档使用 Webox 独立轻量代理

企业微信 Finance SDK 与个人微信桌面客户端的依赖和运行语义完全不同，因此企业微信容器运行 Webox 仓库内
独立的 `wecomproxy`，不再把 Finance 来源编入 `weagent`。代理继续提供 aicat 已使用的身份、鉴权、统一游标
和 Shared Directory contract，所以 aicat 的 endpoint、token 和 Room 配置保持不变；代理原样保留未知消息类型
及消息体，只规范化 `roomid` 并对明确媒体类型做本地化，消息保存、解释与 trigger 仍由 aicat 负责。
