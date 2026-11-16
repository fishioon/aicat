# aicat

[English](README.md)

**你的通用 AI 连接工具**

模块化设计，可扩展、可组合。

灵感源自 netcat。

aicat 在你自己的机器上连接 Agent、模型、工具与消息渠道，并提供流量检查。你在自己选用的 Agent 里工作，aicat 在后台运行；桌面窗口用于接入、配置、授权和排查问题。

当前支持 macOS，Go 主进程承载网关，消息扩展按需启动应用内 Bun 宿主；共用一个入口（默认 `127.0.0.1:9981`）。

## 能做什么

| 能力 | 说明 |
| --- | --- |
| HTTP/HTTPS 代理 | 显式代理或接管系统代理；HTTPS 默认直通，按域名选择解密 |
| 流量检查 | 实时列表、按域名/路径/应用筛选、请求与响应详情、复制 cURL、导出 JSON |
| 供应商接入 | 填写名称、地址和 Key 接入供应商；保留模型列表，按请求接口透明转发，上游错误直接返回 |
| Codex 扩展 | 在 Codex 的模型列表里使用供应商的模型，同时保留 ChatGPT 登录 |
| 企业微信客户端 | NPM客户端渠道，启用时检查登录态并自动准备数据库与密钥，使用原版 SQLite3MC 读取本地数据库与 WAL，增量保存到 Room，重启续读，按会话触发 Agent 并通过 MCP 请求客户端发送文本 |
| 企业微信 BOT | 从推荐列表安装官方插件，配置并通过 Bot 鉴权验证后启用；接收私聊、持久保存 Room 历史，在消息页面按Room配置model与自动处理，显式MCP发布或授权外发 |
| Agent 接口 | HTTP API 与 MCP 查询实际经过网关的请求 |
| 统一消息 | 企微与扑克等渠道按会话进入Room；固定Codex app-server，两层prompt，model和触发按Room设置，本地发布与授权外发分开 |

每项能力的入口、操作步骤与验证方式见 [Feature map](docs/feature-map/README.md)，其中也列出了尚未交付的能力。

## 构建与运行

构建需要 Go 1.27.3+、Node.js 22.12+、npm 和 Bun 1.4.4；构建 app 还需要带 `actool` 的 Xcode 26+。构建流程将 Bun、渠道宿主和锁定依赖一起打包，用户运行 app 不需要自行准备 Bun。构建时可用 AICAT_BUILD_BUN 指定 Bun 二进制。

```sh
make build
bin/aicat --home "$PWD/build/preview/.aicat"
```

`--home` 指定数据目录，用于隔离体验；省略时使用 `~/.aicat/`。其他入口：

| 命令 | 产物 |
| --- | --- |
| `make app` | 未签名的 `build/desktop/aicat.app` |
| `make cli` | 无界面入口 `bin/aicat-cli`，用 `bin/aicat-cli serve` 运行同一组服务 |
| `make run` | 构建前端后直接运行桌面入口 |

关闭主窗口后 aicat 留在系统托盘继续运行，从托盘或 Dock 重新打开，从托盘菜单退出。

## 接入

让客户端使用 aicat 作为 HTTP 代理：

```sh
curl --proxy http://127.0.0.1:9981 http://example.com
```

- **系统代理**：在「设置 → 代理」开启，默认关闭；退出时恢复原设置。
- **HTTPS 解密**：在「设置 → 代理」安装证书，再添加要解密的域名（支持 `*.example.com`）；也可在流量详情里对某个 CONNECT 记录直接启用。未列入的域名保持直通。
- **模型供应商**：在「主页 → 接入供应商」选择供应商并保存密钥，然后让 Agent 指向 aicat（`ANTHROPIC_BASE_URL=http://127.0.0.1:9981` 或 `OPENAI_BASE_URL=http://127.0.0.1:9981/v1`），模型名写作 `供应商 ID/模型`。
- **MCP**：地址为 `http://127.0.0.1:9981/mcp`（Streamable HTTP），`Authorization: Bearer <token>`，token 在数据目录的 `mcp-token`。各客户端的配置片段见「主页 → 接入 Agent」。

API、MCP 与模型转发只接受本机访问。查询流量：

```sh
curl http://127.0.0.1:9981/api/traffic
```

| 路径 | 内容 |
| --- | --- |
| `GET /api/traffic` | 请求摘要；支持 `limit`、`before`、`host`、`method`、`state`、`q` |
| `GET /api/traffic/{id}` | 单个请求在四个观测位置的 headers 与 body 引用 |
| `GET /api/bodies/{id}` | 已保留的 body 字节；`X-Aicat-Body-State` 表示采集状态 |

## 配置

行为配置只有数据目录中的 `aicat.yaml`，首次运行生成。界面中的设置都写入这一个文件：

```yaml
ui:
  language: system   # system / zh-Hans / en
  theme: system      # system / light / dark
systemProxy:
  enabled: false
extensions:
  traffic:
    enabled: true
  codex:
    enabled: false
gateway:
  addr: 127.0.0.1:9981
  interceptHosts: []
  bufferedBodyLimit: 1048576
traffic:
  maxRecords: 2000
  maxBodyBytes: 262144
  maxTotalBodyBytes: 67108864
```

流量记录保存在有界内存中，超出上限会淘汰或截断，退出即清空；这些上限不影响实际转发。供应商密钥单独存放在数据目录的 `providers.json`（权限 0600），不写入 `aicat.yaml`。

## 文档

| 文档 | 内容 |
| --- | --- |
| [CONTEXT.md](CONTEXT.md) | 产品定位与领域术语 |
| [Feature map](docs/feature-map/README.md) | 功能、入口与验证路径 |
| [架构](docs/architecture.md) | 模块边界、数据流与约束 |
| [开发约定](docs/development.md) | 构建、验证与本地安装约束 |

本仓库维护桌面端、内置扩展和通用扩展宿主。NPM 扩展的实现与专属验收在独立仓库：[aicat-wecom-local](https://github.com/fishioon/aicat-wecom-local)、[aicat-wpk](https://github.com/fishioon/aicat-wpk)。这两个包不随应用提供，尚未发布到公网 NPM。
