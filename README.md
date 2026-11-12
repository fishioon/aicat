# aicat

aicat 是装在你自己机器上的常驻 Agent Gateway：为接入的 Agent 提供模型、工具与流量检查入口。你在自己选用的 Agent 里工作，aicat 在后台运行；桌面窗口只用于接入、配置、授权和排查问题。

当前支持 macOS，一个进程、一个入口（默认 `127.0.0.1:9981`）。

## 能做什么

| 能力 | 说明 |
| --- | --- |
| HTTP/HTTPS 代理 | 显式代理或接管系统代理；HTTPS 默认直通，按域名选择解密 |
| 流量检查 | 实时列表、按域名/路径/应用筛选、请求与响应详情、复制 cURL、导出 JSON |
| 供应商接入 | 保管 Anthropic、OpenAI 及兼容 OpenAI 服务的密钥，Agent 经 aicat 调用模型而不接触密钥 |
| Codex 扩展 | 在 Codex 的模型列表里使用供应商的模型，同时保留 ChatGPT 登录 |
| 规则与脚本插件 | 用单文件 JS/TS 改写请求和响应，由 Agent 通过 MCP 编写、试运行和管理 |
| Agent 接口 | HTTP API 与 MCP 查询实际经过网关的请求 |

每项能力的入口、操作步骤与验证方式见 [Feature map](docs/feature-map/README.md)，其中也列出了尚未交付的能力。

## 构建与运行

需要 Go 1.27.3+、Node.js 22.12+ 和 npm。

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
- **模型供应商**：在「概览 → 接入供应商」选择供应商并保存密钥，然后让 Agent 指向页面给出的地址，例如 `http://127.0.0.1:9981/p/anthropic`。
- **MCP**：地址为 `http://127.0.0.1:9981/mcp`（Streamable HTTP），`Authorization: Bearer <token>`，token 在数据目录的 `mcp-token`。各客户端的配置片段见「概览 → 接入 Agent」。

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

## 规则与脚本插件

改写由规则加单文件 JS/TS 插件完成：规则决定匹配范围、顺序和启停，插件实现 `onRequestHeaders`、`onRequest`、`onResponseHeaders`、`onResponse` 四个钩子。全部通过 MCP 管理，保存后重启生效，不需要重新编译 aicat。

让 Agent 先调用 `plugins_guide` 读取 [插件 SDK](internal/mcpserver/guide.md)，再按“查看现状 → 校验并保存插件 → 保存规则 → 离线试运行 → 重启后核对实际流量”的顺序工作。插件出错或超时只撤销当次修改，请求照常转发。

## 文档

| 文档 | 内容 |
| --- | --- |
| [CONTEXT.md](CONTEXT.md) | 产品定位与领域术语 |
| [Feature map](docs/feature-map/README.md) | 功能、入口与验证路径 |
| [架构](docs/architecture.md) | 模块边界、数据流与约束 |
| [开发约定](docs/development.md) | 构建、验证与本地安装约束 |
| [插件 SDK](internal/mcpserver/guide.md) | 脚本插件的清单、数据格式与工作流 |

本仓库只包含桌面端。
