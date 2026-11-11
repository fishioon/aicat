# aicat UI 与 magpie 的对应关系

参考源码：`yetone/magpie`，本地参考提交 `51834ba937a929a7b2f448eb61928b2ba7e1ada7`。
以其 `internal/gui/assets/index.html`、`app.css`、`app.js` 和 `internal/gui/textsize.go`
为底稿，移植可复用的布局、组件样式和交互。MIT 版权声明随前端资源嵌入应用：
`internal/gui/assets/magpie-LICENSE.txt`。

## 保持一致的部分

- 面板宽 440，等宽分段标签，底栏固定；各面板标签共享最高内容的高度，限制在 220–560，超出后内容区滚动。
- 顶部品牌、刷新、打开主窗口、设置的位置；更新胶囊留在原位置，但未接更新服务时隐藏。
- Agent 使用图标、名称、模型摘要、展开箭头、展开详情；未安装的条目放进可展开的折叠列表。
- 主窗口保留独立导航与原生标题栏；齿轮打开设置页，面板中的设置入口打开主窗口相应页面。
- 用量使用时间段、统计卡、输入/输出趋势、Agent 聚合、模型聚合。
- 采用 magpie 的明暗基础色和系统字体；强调色替换为 netcat T500，名称和图标替换为 aicat。

## 接到 aicat 的部分

- Agent 的安装检测与用量独立。目前实际支持 Codex，检测命令路径和 macOS App；不凭空显示其他 Agent。
- Agent 摘要读取 Codex 用户配置中的模型和推理强度。展开区移植 magpie 的模型菜单、强度滑杆和原地展开/收起交互，不再把三条只读信息画成设置控件。摘要旁的三条柱表示配置中的推理强度。
- 点击模型可搜索目录、按供应商查看、输入自定义 ID 或恢复默认；滑杆支持拖动、方向键和 Home/End。保存失败显示错误并重新读取磁盘；菜单支持 Escape、点击外部关闭和焦点返回。
- 默认目标为 `$CODEX_HOME/config.toml`，未设置环境变量时为 `~/.codex/config.toml`。仅写 `model` 和 `model_reasoning_effort`；不修改 `model_provider`、base URL、账号、代理或项目配置。选择模型时调整为其目录声明的默认强度；不支持强度的模型清除旧值。
- 原生模型和能力读取 Codex 的 `models_cache.json`（配置了 `model_catalog_json` 时读取指定目录文件），与 aicat 当前启用的供应商模型合并。缺少能力元数据时不虚构推理级别；缺少目录时仍可输入自定义模型 ID。未启用或已删除供应商的缓存条目不会冒充可用供应商。
- 写入用 TOML 解析器定位字符串值，保留注释、多行内容、无关表和权限；符号链接写向实际目标。首次修改前在目标旁保存 0600 的 `.aicat-backup`，后续不覆盖；临时文件同步后原子替换。配置内容版本冲突拒绝覆盖。存在显式 `profile` 或非法 TOML 时只显示错误，不猜测目标或覆盖文件。
- 这些是 Codex 的用户级默认配置，不承诺切换正在运行的会话；项目级配置、启动参数和会话选模仍可能覆盖它。Agent 展开后也可复制代理命令、进入网关设置；最近接入方式和请求时间只作为一行辅助信息，响应模型放在提示中。
- 网关开关写入唯一配置 `~/.aicat/aicat.yaml` 并重启运行体。停止之前先恢复系统代理。
- 首次生成 CA 后，“安装并信任证书”将公开 CA 导入当前用户默认钥匙串（通常为 login），请求 macOS 当前用户的所有用途 Always Trust，再实查用户级无限制信任和 SSL 链；旧版 SSL-only 配置可通过同一按钮升级。不请求管理员提权，第一版仅保留这一项证书操作。系统代理独立开启，关闭和退出时恢复原设置。
- 主窗口的供应商页支持添加、编辑、删除、启停，获取模型并选择展示项，也可手动补充模型 ID。已保存 Key 不回显，编辑留空保持原 Key；模型管理不放入托盘面板。
- 路由页展示原有 ChatGPT 与已启用供应商的分流。模型按 Magpie 的 `provider/model` 方式追加，原有条目和登录保持不变；其他 CONNECT 隧道直通、WebSocket 426 回退。
- 账号入口位于左上角品牌旁，打开账号设置；使用系统浏览器登录 tiny.yun，支持取消、恢复、刷新和退出。
  主窗口和面板共享后端账号状态。凭据保存到 `~/.aicat/auth.json`（0600），不进入 Keychain；退出只撤销 aicat 的令牌。
- 暂缓额度、自动获取登录账号的私云 Provider 和更新服务；当前供应商由用户手动添加，只接 Responses，不引入协议转换。

## 重复验证

```sh
make verify
NODE_PATH=/path/to/node_modules node scripts/ui-smoke.cjs
python3 scripts/native-ui-smoke.py
# 原生剪贴板检查：脚本等待原生窗口点击，逐项读回系统剪贴板
python3 scripts/native-ui-smoke.py --clipboard
```

Agent 设置的完整链路验收：

```sh
CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0 \
  go build -tags desktoptest -o bin/aicat-connection-check ./cmd/aicat-connection-check
NODE_PATH=/path/to/node_modules node scripts/agent-settings-smoke.cjs
python3 scripts/native-ui-smoke.py --agent-settings --keep-open
```

前者使用真实 UI 和生产配置接口，在 `~/.aicat/tests/` 的独立 HOME/CODEX_HOME 中选择模型、调整强度，
并通过真实 Codex app-server 的 `config/read` 读回。覆盖恢复默认、注释/表/多行值保留、符号链接、
版本冲突、无效 TOML、失败 UI、缺少目录、键盘焦点、暗色和小窗口菜单。
`CODEX_BINARY` 可指定验收用 Codex 二进制；不发推理请求或使用真实凭据。
报告和截图在 `build/agent-settings-smoke/`。后者为原生 WebView 准备两个合成模型，供实际交互验收，
不会读取或修改真实 Codex 配置、钥匙串或系统代理。

UI 脚本需要 `playwright`，默认在 macOS 使用已安装的 Google Chrome，也可通过 `CHROME_PATH` 指定浏览器。
UI 脚本使用确定性的 API fixtures 驱动真实前端页面，检查延迟加载、展开、原生复制 API 请求与失败重试、标签等宽、
跨标签高度稳定、空态、长列表滚动、窄窗口、设置页和网关开关请求；它不替代真实 MITM 验证。

原生脚本复制构建出的 App 到临时目录，为该实例准备独立 HOME 和随机空闲端口，
通过真实 CONNECT + TLS 请求检查 Codex addon 的 426 响应及 JSONL 记账。只信任本次测试 CA，
不修改系统代理或真实用户钥匙串。加 `--keep-open` 可继续通过原生 UI 验证启停，Ctrl-C 结束实例。

加 `--clipboard` 时，按终端提示依次点击主窗口 Agent 页的“复制代理命令”、网关页的“复制地址”和
“复制命令”（可由原生 UI 自动化工具代为点击）。每一步先写入不同的剪贴板标记，最长等待 180 秒，
再将系统剪贴板与该实例随机端口对应的完整命令或地址逐字比对。此检查会覆盖当前剪贴板内容。
结果保存为 `build/ui-smoke/native-clipboard-report.json`，包括构建二进制 SHA-256 和各项期望值、实测值。
此报告只覆盖主窗口；托盘面板的请求路径由浏览器 UI 检查覆盖，不计作原生面板验证。

报告和截图在 `build/ui-smoke/`。构建和既有网关 e2e 通过 `make verify` 运行。

供应商与模型分流验收：

```sh
CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0 \
  go build -tags desktoptest -o bin/aicat-connection-check ./cmd/aicat-connection-check
NODE_PATH=/path/to/node_modules node scripts/provider-smoke.cjs
```

这是隔离的 UI → 配置 → 真实网关 → TLS MITM → Responses SSE/用量流程。上游和凭据是本地合成 fixture，
不是私云网络验收；若有 Codex 二进制，还以其真实 app-server `model/list`（合成 ChatGPT 身份）和
`exec`（合成 API Key 身份，避免访问真实 Workspace 服务）验证目录解析和选模。
报告、合并模型列表和截图保存到 `build/provider-smoke/`。无真实 Keychain 或系统代理写入。

用户在 App 保存真实供应商后，再执行真实私云验收：

```sh
python3 scripts/provider-live-smoke.py --model netcat/deepseek-flash --native-model gpt-6-luna
```

该脚本不提取或直接使用供应商 Key、不改 Codex 配置，不设置测试 CA 环境变量，仅让临时 Codex 子进程经本机代理；
对本机配置和 CA 文件计算摘要，以检查验证前后没有被改动。
复用真实 ChatGPT 登录的临时副本，以真实 `model/list` 检查原有和私云模型共存，然后分别运行私云与订阅模型，
把 HTTP 200、输出标记和 token 用量与网关 JSONL 对齐。原始凭据副本及测试 Home 在退出时删除。
证据保存在 `build/provider-smoke/live-report.json`，包含模型列表、两次回答和对应账本记录。

证书与代理流程另用生产控制逻辑和测试专用系统适配器跑通：

```sh
CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0 \
  go build -tags desktoptest -o bin/aicat-connection-check ./cmd/aicat-connection-check
NODE_PATH=/path/to/node_modules node scripts/connection-smoke.cjs
```

该流程覆盖证书授权取消、安装失败、安装后未信任不能误报成功、自动安装成功不自动开启代理、外部信任变更检测、代理设置失败、HTTP/HTTPS 开启、PAC/SOCKS/绕过列表恢复、外部修改冲突、
停止网关、正常退出与异常退出后的恢复，报告和截图位于 `build/connection-smoke/`。
测试专用适配器不进入正常构建。它验证控制流程，不代表已在用户真实钥匙串安装证书。

`python3 scripts/system-proxy-read-smoke.py` 直接编译并调用生产 macOS 适配器，读取真实网络服务并检查
JSON 类型（尤其 `disabled` 必须为布尔值），不写系统设置。报告为 `build/connection-smoke/native-proxy-read-report.json`。

真实 Codex 验证（需要已有 ChatGPT 登录，模型名使用账号实际可用项）：

```sh
python3 scripts/codex-flow-smoke.py --model <model>
```

该脚本为 Codex 准备临时目录，完成后删除临时凭据副本；仅给该子进程配置测试代理与测试 CA，
检查真实模型列表、426 回落、成功回答和 token 记账，结果写入 `build/connection-smoke/codex-flow-report.json`。
若已经通过 `native-ui-smoke.py --keep-open` 启动原生测试实例，可加
`--native-session build/ui-smoke/native-session.json`，让请求直接经过该 App，随后在原生用量页查看结果。
