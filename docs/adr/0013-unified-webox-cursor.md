# Claw 只消费 Webox 统一游标协议

状态：accepted

Claw 对个人微信和企业微信只支持一种 `getupdates` 协议：请求提交字符串 `cursor`，响应返回扁平的企业微信
会话存档形状 `messages` 与已消费位置 `cursor`。单条消息没有 transport `seq`；aicat 用 `roomid` 分组，保留
原始 JSON 写入各自 Room Context Log，不把不同 Room 合并给 Codex。

每个 Claw Agent 在 `<Agent Directory>/cursor` 保存游标。一次响应的所有受影响 Room 都追加并 `fsync` 成功后，
aicat 才原子替换并同步 cursor 文件；部分 Room 已写入但提交失败时，当前允许重试消息重复追加。Codex dispatch
发生在游标提交之后，dispatch 失败不会重放 transport batch。用户可以手动降低 cursor 以读取更久历史。

## Consequences

- 旧的 Room Sequence map、消息公开 `seq` 和按 Room 分组响应全部废弃，不提供双协议兼容。
- 未知 `msgtype` 与未知消息体字段无需 aicat 声明即可保存在 Context Log；只有文本和 reply 正文参与现有 trigger。
- cursor 是整个响应的提交水位，不能命名为 `next_seq`，也不能写入单条 Room 日志。
