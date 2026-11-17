# aicat

[简体中文](README_CN.md)

**Your General-Purpose AI Connector**

Modular by design. Extensible and composable.

Inspired by netcat.

aicat connects agents, models, tools, and messaging channels on your own machine, with traffic inspection built in. Work in the agent of your choice while aicat runs in the background. Use the desktop window for setup, configuration, authorization, and troubleshooting.

Currently available for macOS. The Go process runs the gateway, and messaging extensions start the bundled Bun host as needed. All services share one endpoint, defaulting to `127.0.0.1:9981`.

## Features

| Capability | Description |
| --- | --- |
| HTTP/HTTPS proxy | Use an explicit proxy or take over the system proxy. HTTPS passes through by default, with decryption enabled per domain. |
| Traffic inspection | Browse live requests, filter by domain, path, or app, inspect requests and responses, copy cURL commands, and export JSON. |
| Model providers | Add a provider with a name, endpoint, and API key. Keep its model list and transparently forward requests, returning upstream errors directly. |
| Codex extension | Use provider models in Codex's model list while keeping ChatGPT sign-in. |
| WeCom desktop client | An NPM channel that checks sign-in and prepares database access and keys when enabled. Reads the local database and WAL with stock SQLite3MC, saves incremental messages to Rooms, resumes after restart, triggers agents per conversation, and requests text sends through MCP. |
| WeCom BOT | Install the official plugin from the recommended extensions, configure it, verify Bot authentication, and enable it. Receive direct messages, persist Room history, configure each Room's model and automatic processing in Messages, and explicitly publish replies or authorize outbound sends through MCP. |
| Agent interfaces | Query requests that actually passed through the gateway using HTTP API and MCP. |
| Unified messaging | Route WeCom channel conversations into Rooms. Use Codex app-server with two prompt layers, configure models and triggers per Room in the Room management page, and send replies explicitly through MCP. |

See the [Feature map](docs/feature-map/README.md) for entry points, usage, verification steps, and capabilities that have not yet shipped. Linked project documentation is currently in Chinese.

## Build and run

Building requires Go 1.27.3+, Node.js 22.12+, npm, and Bun 1.4.4. Building the app bundle additionally requires Xcode 26+ with `actool`. The build bundles Bun, the channel host, and locked dependencies, so users do not need to install Bun. Set `AICAT_BUILD_BUN` to choose a Bun binary during the build.

```sh
make build
bin/aicat --home "$PWD/build/preview/.aicat"
```

Use `--home` to isolate application data. The default is `~/.aicat/`. Other entry points:

| Command | Output |
| --- | --- |
| `make app` | Unsigned `build/desktop/aicat.app` |
| `make cli` | Headless entry point `bin/aicat-cli`; run the same services with `bin/aicat-cli serve` |
| `make run` | Build the frontend and run the desktop entry point |

Closing the main window leaves aicat running in the system tray. Reopen it from the tray or Dock, and quit from the tray menu.

## Connect

Configure a client to use aicat as its HTTP proxy:

```sh
curl --proxy http://127.0.0.1:9981 http://example.com
```

- **System proxy:** Enable it in Settings → Proxy. It is off by default, and the original settings are restored when aicat quits.
- **HTTPS decryption:** Install the certificate in Settings → Proxy, then add domains to decrypt, including patterns such as `*.example.com`. You can also enable decryption for a domain from its CONNECT details. Other domains remain in passthrough mode.
- **Model providers:** Add a provider and save its key in Home → Connect provider, then point your agent at aicat with `ANTHROPIC_BASE_URL=http://127.0.0.1:9981` or `OPENAI_BASE_URL=http://127.0.0.1:9981/v1`. Use model names in the form `provider-id/model`.
- **MCP:** Use `http://127.0.0.1:9981/mcp` with Streamable HTTP and `Authorization: Bearer <token>`. The token is stored in `mcp-token` under the data directory. Client configuration snippets are available in Home → Connect Agent.

The API, MCP, and model forwarding endpoints accept local requests only. Query traffic with:

```sh
curl http://127.0.0.1:9981/api/traffic
```

| Path | Content |
| --- | --- |
| `GET /api/traffic` | Request summaries; supports `limit`, `before`, `host`, `method`, `state`, and `q` |
| `GET /api/traffic/{id}` | Headers and body references at all four observation points |
| `GET /api/bodies/{id}` | Retained body bytes; `X-Aicat-Body-State` indicates capture state |

## Configuration

Behavior is configured in `aicat.yaml` under the data directory, created on first run. Settings changed in the UI are saved to this file:

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

Traffic records are held in bounded memory. Records may be evicted or truncated when limits are reached and are cleared on exit. These limits do not affect forwarding. Provider keys are stored separately in `providers.json` under the data directory with permissions `0600`, rather than in `aicat.yaml`.

## Documentation

| Document | Content |
| --- | --- |
| [CONTEXT.md](CONTEXT.md) | Product positioning and domain terminology |
| [Feature map](docs/feature-map/README.md) | Features, entry points, and verification paths |
| [Architecture](docs/architecture.md) | Module boundaries, data flow, and constraints |
| [Development](docs/development.md) | Build, verification, and local installation requirements |

This repository contains the desktop application, built-in extensions, and the shared extension host. NPM extension source and acceptance tests live in a separate repository: [aicat-wecom-local](https://github.com/fishioon/aicat-wecom-local). Its package is not bundled with the app and has not yet been published to the public NPM registry.
