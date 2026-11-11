# 生成 aicat CA，但不修改系统信任

状态：accepted；「CA 是启动时准备的本地状态」这一前提已由
[0017](0017-builtin-gateway-for-codex-wiring.md) 取代（CA 随 MITM 模块后移），不修改系统信任的决策继续有效。

`aicat serve` 在本机尚无 CA identity 时生成 ECDSA P-256 CA，并将 cert/key 原子安装到
`~/.aicat/certificates/current/`。aicat 不读取或写入 macOS Keychain，也不调用
`security add-trusted-cert`。

已有完整 CA identity 的后续启动只验证文件属性、有效期和 key match 后复用。

## Why

CA generation 是准备 agentgateway 动态 TLS 所需本地状态的一部分；修改操作系统信任则是独立的高影响
操作，可能触发授权交互，并改变其他应用的安全边界。信任应由用户在明确知情的情况下，在需要使用代理的
设备上自行建立。

## Consequences

- 首次启动可无授权交互地生成并持久化 CA；
- aicat 启动、重启和升级都不会修改系统证书或信任设置；
- 用户需要自行下载、安装并信任 CA，未信任的 TLS 客户端会拒绝动态签发的证书；
- aicat 仍不读取、写入或恢复 macOS system proxy。
