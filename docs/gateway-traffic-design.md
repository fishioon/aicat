# gateway 与 traffic 接口设计

更新日期：2026-11-10。状态：原型、规则与插件后端已实现，后续能力仍按本文演进。

aicat 使用 MyGo + WebView、go-mitmproxy 和 moejs，从零实现。WebView 界面经私有协议调用进程内服务；`127.0.0.1:9981` 承载代理和对外 API/MCP。本文细化 [一期设计](phase-one-design.md) 中 gateway、traffic 及两者之间的接缝，不增加历史兼容层。

## 包边界

| 包 | 职责 | 依赖边界 |
| --- | --- | --- |
| `internal/app` | 应用装配、mygo 窗口与页面、UI 状态 | 调用 gateway 生命周期及 traffic 查询与订阅 |
| `internal/gateway` | 装配代理、连接生命周期、CA 注入、解密选择与出站策略 | 使用 go-mitmproxy，不依赖 mygo |
| `internal/gateway/addons/api` | 将 HTTP handler 接入 9981 | 识别 aicat 请求，不处理代理站点的同名路径 |
| `internal/gateway/addons/traffic` | 将代理阶段转成流量观测，并衔接规则和脚本 | 使用 go-mitmproxy 与 traffic 写入能力，不依赖 UI |
| `internal/rewrite` | 管理规则与插件文件、固定启动版本、事务化四阶段执行 | 调用 plugins，不依赖 UI、proxy.Flow 或 traffic |
| `internal/plugins` | moejs 编译、隔离调用及宿主绑定 | 不依赖 gateway、traffic 或 mygo |
| `internal/mcpserver` | 规则/插件管理与流量查询的 MCP 适配 | 复用进程内服务，在入口认证 |
| `internal/traffic` | 流量模型、记录、body 读取、查询及变化订阅 | 不依赖 gateway、go-mitmproxy、mygo 或 moejs |

macOS 应用归属由流量 addon 识别。首次记录回环连接时，把客户端及监听端点加入有界后台查询。按两端地址、端口和方向匹配 `lsof` 的 TCP socket；归属不唯一或连接已关闭时留空。应用 bundle 使用 bundle ID 分组，命令行程序使用进程名。记录可选的 `clientApp` 只含名称和 bundle ID，经 Revision 更新并随 UI、HTTP API 和 MCP 的记录返回。

根 `ui/` 放独立的 Vue 3/TypeScript/Vite 前端和薄资源嵌入入口；Go 侧文件结构如下。文件先按职责划分，实际大小不足时可以合并，不为每个类型新增包。

```text
internal/
├── app/
│   ├── app.go
│   ├── desktop.go
│   ├── desktop_traffic.go
│   └── runtime.go
├── gateway/
│   ├── gateway.go
│   ├── options.go
│   ├── lifecycle.go
│   └── addons/
│       ├── api/addon.go
│       └── traffic/addon.go
└── traffic/
    ├── service.go
    ├── model.go
    ├── recorder.go
    ├── body.go
    └── subscription.go
```

`app` 负责组合依赖；业务包不回调 `app`。HTTP/MCP handler 和 WebView 界面使用同一个 traffic 服务实例，外部授权在 HTTP/MCP 入口完成。底层 `proxy.Flow` 只在代理适配层使用。

## gateway 接口

以下 Go 声明对应当前原型接口。

```go
type Options struct {
    Addr              string
    CA                cert.CA
    ShouldIntercept   func(*http.Request) bool
    BufferedBodyLimit int64
    Addons            []proxy.Addon
}

func New(options Options) (*Gateway, error)

func (g *Gateway) Run(ctx context.Context) error
func (g *Gateway) Ready() <-chan struct{}
func (g *Gateway) Status() Status
func (g *Gateway) Shutdown(ctx context.Context) error
```

`cert.CA` 和 `proxy.Addon` 直接使用 go-mitmproxy 的接口，不再包装一套 addon 框架。与早期草案相比，移除 `Options.LocalHandler`：handler 通过 `api` addon 注入，gateway 不再有另一种服务注册机制。

### 参数与生命周期

| 接口或参数 | 语义 |
| --- | --- |
| `New` | 校验参数、构造对象、固定 addon 列表；不监听端口、不安装证书、不修改系统代理 |
| `Addr` | 默认 `127.0.0.1:9981`；支持 `0.0.0.0` 和本机网卡 IP，端口为 1–65535 |
| `CA` | 应用提供的本地 CA，负责签发叶子证书；生成、保管和系统信任不属于 gateway |
| `ShouldIntercept` | 判断 CONNECT 目标是否解密；未提供时由 aicat 显式选择全部透传，不继承库的默认全部解密行为 |
| `BufferedBodyLimit` | 完整 body 的缓冲上限；默认 1 MiB，超过后按原读取路径流式转发并截取有界前缀 |
| `Addons` | 构造时复制并固定顺序，运行中不增删 |
| `AddressAddons` | 每个监听实例构造与当前地址相关的 addon；API authority 随监听同步，流量采集服务保持同一实例 |
| `Run` | 阻塞运行；启动失败返回错误；正常停止返回 nil，异常失败返回原因；同一实例只运行一次 |
| `Ready` | 只有本实例可处理请求后才关闭；启动失败不关闭，调用方同时等待 Run 的错误结果 |
| `Status` | 返回独立状态快照，包含阶段、监听地址和失败信息；不用返回内部 Proxy 指针 |
| `Rebind` | 保持 Gateway、CA 和采集服务身份，串行切换底层监听；就绪后提交系统代理/配置变更，失败回退 |
| `Shutdown` | 幂等停止，阻止新连接并在 deadline 内处理在途请求，清理 CONNECT 和其他长连接 |

Gateway 生命周期对象保持稳定，内部每个 go-mitmproxy 实例只运行一次。不同端口/不冲突的地址先启动新入口，提交设置后再关闭旧入口；同端口地址重叠时先释放旧监听，失败重新创建旧入口。应用层的 connection 串行协调系统代理、配置保存和退出，gateway 不直接执行系统设置操作。保存失败或系统代理写入失败会尝试回退；系统代理回退自身失败时，保留恢复所需的监听，显示错误，直到用户恢复或退出。`Run` 的 context 取消进入有界清理。

已知上游限制：go-mitmproxy v1.9.4 的 `Close`/`Shutdown` 只关闭入口 server，没有关闭内部 attacker server/等待 listener。反复切换会累积内部等待协程；aicat 已关闭旧对外监听及其连接，此内部释放缺口仍需上游修复，不以反射或私有 fork 绕过。依据：[上游 Proxy 生命周期](https://github.com/lqqyt2423/go-mitmproxy/blob/v1.9.4/proxy/proxy.go)、[内部 listener](https://github.com/lqqyt2423/go-mitmproxy/blob/v1.9.4/proxy/attacker.go)。

go-mitmproxy v1.9.4 的公开 `Start` 自行创建 listener，没有公开就绪回调。`Ready` 是 aicat 需要适配的语义，不能通过启动 goroutine 或发现端口可连接就报告成功。首轮须验证能确认本实例身份的就绪探测，并处理启动失败和停止竞态；必要时向上游补接口。

底层 `Shutdown` 调用 HTTP server 的关闭流程，aicat 仍需跟踪和关闭已接管的 CONNECT、WebSocket 等长连接。先阻止新连接，deadline 内等待普通 HTTP handler，再主动关闭没有有限请求边界的已接管隧道；超时也强制关闭剩余连接；就绪与清理都需要真实 socket 验证。

### 对外 HTTP 服务与直接出站

`api` addon 接收上层组装的 `http.Handler` 和允许的本机 authority。`AccessProxyServer` 同时核对 TCP peer 为 loopback 或本机实际地址、authority 和 Origin；监听 `0.0.0.0` 时，本机 API 客户端使用 `127.0.0.1`，不会将控制接口开放给其他机器。普通代理流量不因路径与 API 相同而被接管；MCP 继续核对 Bearer token。

gateway 显式配置直接出站，不继承指向自己的 `HTTP_PROXY`/`HTTPS_PROXY`。aicat 或插件主动发起的业务 HTTP 请求仍可显式使用 9981，但最终转发与 WebView 界面的进程内调用不再绕回代理入口。

## traffic 读取接口

traffic 服务同时提供列表、详情、body 和变化订阅。实现先使用具体 `Service` 类型；消费方按需要声明小接口，不预先建立多套 repository、manager 或 event bus 抽象。

```go
func New(options Options) (*Service, error)

func (s *Service) List(ctx context.Context, query Query) (Page, error)
func (s *Service) Get(ctx context.Context, id ID) (Record, error)
func (s *Service) OpenBody(ctx context.Context, id BodyID) (Body, error)
func (s *Service) Watch(ctx context.Context, query Query) (Page, *Subscription, error)
func (s *Service) Recorder() *Recorder
func (s *Service) Close() error
```

`List` 只返回摘要。`Get` 返回 headers、body 引用、处理阶段和规则/插件执行信息；body 内容通过 `OpenBody` 按需读取。列表不会因一次刷新重复传输所有 body。

`Watch` 一次建立初始列表快照与后续变化订阅。UI 打开页面时使用 Watch；仅查询当前数据的 API/MCP 调用使用 List 或 Get。通知只表示哪些记录发生变化，不提供历史通知补发或断点续传。

`Close` 在网关结束、观测写入完成后调用，关闭订阅并释放存储。取消 UI 订阅不关闭服务，服务也不因主窗口关闭而停止采集。

流量功能由 `features.traffic` 控制，缺省开启；关闭后采集 addon 不再创建新记录，既有观测可继续完成，有界记录与只读查询继续可用。关闭、重新开启功能不重建 gateway，不关闭代理端口，也不切断在途连接。临时暂停与功能开关分开管理：同一会话重新开启功能时遵循先前暂停状态；启动时根据持久功能开关设置采集状态。

### 模型轮廓

```go
type ID uint64
type BodyID string

type Summary struct {
    ID         ID
    Revision   uint64
    StartedAt  time.Time
    Method     string
    URL        string
    State      State
    StatusCode int
    Duration   time.Duration
}

type Page struct {
    Items    []Summary
    NextPage string
}

type Record struct {
    Summary
    RequestReceived  Request
    RequestSent      *Request
    ResponseReceived *Response
    ResponseSent     *Response
}

type Body struct {
    Info   BodyInfo
    Reader io.ReadCloser
}
```

这是字段轮廓，连接信息、字节计数、脚本执行详情和 HTTP DTO 字段在实现对应能力时补齐。`Query` 首轮支持有上限的分页、目标、method、状态和摘要文本过滤；不默认对全部 body 执行全文搜索。

`Request` 包含 method、URL、协议、`http.Header` 与 body 引用；`Response` 包含状态码、协议、headers 与 body 引用。多值 headers 原样保留，body 不强制转换为 UTF-8 文本。查询及导出使用统一的可见性和脱敏策略。

`RequestReceived` 表示客户端请求的语义快照，`RequestSent` 表示准备发往上游的版本，`ResponseReceived` 表示上游响应，`ResponseSent` 表示准备返回客户端的版本。nil 表示该位置尚未发生或被短路，不用空对象伪装。快照不证明目标已收到字节。

每条记录在请求 headers 到达时创建，状态随后进入响应中、完成、失败或取消。状态码尚未到达时为 0，不能据此把请求判为失败；SSE 在连接结束前保持响应中。连接和 CONNECT 外层事件与 HTTP 交换分别关联，不能重复记为一次完整业务请求。

请求 ID 从 1 开始自增，对应 UI 的 `#` 列。同一个请求的 headers、body 和响应更新沿用同一个 ID；筛选、排序、移除记录不改变或复用已有编号。清空列表不重置计数，重新创建内存 traffic 服务时从 1 开始。重启应用后内存记录清空，UI 重新建立 Watch，不沿用上次运行的记录 ID。

`Revision` 仅标识单条记录的版本，用于识别过时的查询结果，不是事件编号。返回值中的 map、slice 和 headers 是独立副本或不可变快照，不能被调用方修改后影响内部记录。

`BodyInfo` 区分待采集、完整、截断、未采集和采集失败，并携带已观察及已保留字节数。空 body 是完整的零字节内容。`OpenBody` 返回打开时已提交内容的固定长度只读视图和对应 Info，不跟随后续追加，调用方必须关闭 Reader。UI 收到更新后可重新读取；记录或 body 已过期时返回明确错误。

## traffic 观测写入接口

`Recorder` 是代理 addon 的写入接缝。UI 与 HTTP/MCP handler 使用读取接口，不接收 Recorder。

```go
func (r *Recorder) Begin(start Start) (ID, error)
func (r *Recorder) Update(id ID, observation Observation) error
func (r *Recorder) Finish(id ID, completion Completion) error

type Observation struct {
    Point    Point
    Request  *Request
    Response *Response
}
```

`Start` 提供开始时间、连接关联和客户端请求 headers。`Begin` 分配从 1 自增的请求 ID，立即建立可查询记录。addon 保存 `proxy.Flow` 到该 ID 的内部映射，原始指针不进入 traffic 服务。

`Point` 有四个观测位置：客户端请求、发往上游的请求、上游响应、返回客户端的响应。它们对应修改前后的快照，不是另一套脚本钩子；同一位置可先提交 headers，随后补齐 body。`Observation` 在请求位置只能携带 Request，在响应位置只能携带 Response，服务验证并按记录递增版本提交。

`Completion` 包含结束时间与完成、失败或取消原因。终结后不再接受普通阶段更新；终结需幂等，取消的请求也要有终态，不能因使用已经取消的请求 context 而跳过收尾。

写端的短临界区只提交有界元数据与已采集数据引用，不等待订阅者、不执行脚本、不等待 UI。body 采集由适配层在原读取路径旁保存有上限的数据，存储接缝在 body 实现时细化；既不把大 body 放入事件，也不把 body I/O 放进持有服务锁的区域。

没有启用采集或容量不足时，明确标记省略、拒绝新记录或采集失败，不能用无界队列承诺永不丢弃。具体预算另定。观测失败进入诊断，不中断实际代理转发，也不能破坏已生效的规则和脚本修改。

## 变化通知与同步

```go
type Change struct {
    Kind ChangeKind
    ID   ID
}

type Subscription struct {
    Changes <-chan Change
}

func (s *Subscription) Err() error
func (s *Subscription) Close() error
```

变化种类包括新增、更新、移除和重新加载。前三种携带受影响的请求 ID；重新加载通知使用 `ID = 0`，表示重新读取当前列表。通知不携带完整记录或 body，也不分配独立事件编号。

消费者合并同一请求的多次通知，批量调用 List 刷新当前列表，必要时调用 Get 更新选中详情。读取的是服务当前状态，无需逐条重放通知中的中间状态。筛选与分页仍由 traffic 查询统一执行。

Watch 的一致性约定如下：

1. 服务在同一同步边界内登记订阅并取得初始列表快照，成功时同时返回 Page 和 Subscription。
2. 初始快照形成之后发生的变化进入该订阅，即使它们发生在 Watch 返回之前，也不会出现订阅空窗。
3. 服务先提交可查询状态，再通知订阅者；消费者收到通知后能读取到对应变化或更新的状态。
4. 页面重新打开、服务重新启动或订阅失去同步时，重新调用 Watch，取得最新列表和新的订阅。

每个订阅有容量上限，允许合并同一请求的重复通知。仍然放不下时关闭该订阅，结束后 `Err` 返回 `ErrSlowSubscriber`，消费者重新调用 Watch。不阻塞写端、不无限缓存，也不静默漏通知后继续使用旧状态。主动关闭、context 取消及服务关闭有可区分的结束原因。

第一期不维护供消费者续接的历史事件日志，不提供事件 Cursor。列表分页与变化订阅独立；翻页期间记录仍可更新，严格一致的归档导出另行设计。

## addon 到 UI 的过程

```text
代理钩子
  → Begin 创建记录
  → Update 提交各阶段快照
  → Finish 提交终态
  → traffic 服务发送变化通知
      ├─ app 通过 Watch 取得初始列表和 Go 订阅，批量查询并更新窗口
      └─ API/MCP 读取同一服务，按协议输出结果
```

流量 addon 在规则或脚本前复制原始内容，执行后复制提交结果。外层 CONNECT 事件不能误调用户的 HTTP 内容钩子。普通、流式及短路响应都必须得到相应的记录状态，不能只在 `Response` 完整 body 钩子中才收尾。

`internal/app/Desktop.WatchTraffic` 在后台调用 Watch，先返回原子快照，再每 100ms 合并摘要增量；订阅背压关闭时在同一 Channel 内重新 Watch 恢复。页面通过官方 MyGo Channel 更新响应式 Map 和 Vue 虚拟行。切换选中项时，返回结果要核对请求 ID、记录版本和当前页面请求代次，避免慢查询覆盖新选中项。查询及 UI 提交串行或按请求代次校验，避免较旧列表覆盖新结果。关闭页面时取消订阅，重新打开时重新调用 Watch。

UI、查询或 body 读取的延迟不回压代理流量。为了守住内存边界，服务允许在明确记录状态的前提下限制采集；实际网络转发仍按连接自身的背压运行。

## 装配与首轮范围

`app` 先创建 traffic 服务，再把 Recorder 注入流量 addon，把读取能力注入 WebView 界面与 API/MCP handler。handler 通过 `api` addon 接到 gateway。启动后等待 Gateway Ready；退出时先停止入口并完成写端收尾，再关闭 traffic 服务。

首轮实现目标是内存中的有界记录、上述查询与订阅、HTTP/CONNECT/MITM 接入、API handler 挂载及可靠停止。规则与 moejs 执行现已接入下述接缝；SQLite 与完整导出仍未实现，不影响查询与订阅的调用方式。

重放在 traffic 业务侧组织，通过注入的代理 HTTP client 重新经过 9981，形成新记录并关联来源；不由 gateway 提供专门的重放 API。重放参数、规则是否重执行、请求关联机制及执行结果句柄在重放能力设计时确定，本轮不虚构一个同步等待完整响应的接口。

## 实现前需验证的接缝

- 确认所选 go-mitmproxy 版本的就绪适配能识别本实例，停止能释放 CONNECT 与 WebSocket 长连接。
- 确认 headers、完整 body、流式与错误路径都能生成正确的开始和终态，并验证写端与查询快照没有数据竞争。
- 验证请求 ID 从 1 自增、阶段更新沿用原 ID、清空列表不复用编号，以及应用重启后重新编号和加载列表。
- 验证 Watch 初始快照与订阅之间没有通知空窗，重复通知可合并，慢订阅者能通过重新 Watch 恢复。
- 验证 body 采集和 UI 都变慢时，采集有界、转发正确，截断状态可见。

上述验证通过真实网络和 UI 的 E2E 完成，输出可复跑报告，不新增事后单元测试。

## 参考资料

- [go-mitmproxy Proxy](https://github.com/lqqyt2423/go-mitmproxy/blob/v1.9.4/proxy/proxy.go)：构造、启动、停止、解密选择和出站代理接口。
- [go-mitmproxy entry](https://github.com/lqqyt2423/go-mitmproxy/blob/v1.9.4/proxy/entry.go)：listener、CONNECT 与 `AccessProxyServer` 的处理位置。
- [go-mitmproxy Flow](https://github.com/lqqyt2423/go-mitmproxy/blob/v1.9.4/proxy/flow.go)：阶段数据、stream 标志和生命周期。
- [mygo 状态更新](https://github.com/egoist/mygo/blob/main/docs/ui/views.md#change-the-state-from-other-goroutines)：后台结果通过窗口更新回到 UI。
- [mitmweb 数据同步](https://github.com/mitmproxy/mitmproxy/blob/992052ee92d7ff4d240eafbe1afdf87c94b3f080/web/src/js/backends/websocket.tsx)：初始数据加载与新增、更新、移除、重置通知。
- [HTTP Toolkit 变化处理](https://github.com/httptoolkit/httptoolkit-ui/blob/ee9bc97626af72de1cc2fb32d910c1cb34c61d8b/src/model/events/events-store.ts)：按请求关联阶段变化，批量更新界面。

## 2026-11-09 原型实现说明

- 默认保留 2,000 条记录、单 body 256 KiB、全局 body 64 MiB；订阅缓冲 256 条。超过记录容量按最旧 ID 淘汰，body 预算不足时标明截断。
- 改写前后的 headers 独立；未修改 body 共用引用，发生修改则分别保存原始与实际发送字节。`Response.Proto` 因库未公开而省略，不能伪造为已确认的协议。
- `Recorder.NewBody`、`AppendBody` 是内存 body 的写入接缝；`Service.Clear` 清空记录但保留编号计数。`Summary.ResponseBytes` 提供列表大小列，避免 UI 为每行读取完整详情。
- 分页采用数值 `Query.Before`；HTTP `nextPage` 返回下一页的 `before` 值。没有订阅事件 cursor。
- WebView 界面每 100ms 在 `internal/app` 合并变化，通过官方 Channel 同步摘要；详情/正文分别读取；选中详情带请求代次校验。
- Ready 通过随机路径与响应标记确认当前实例，未把端口可连接当作启动成功。
- go-mitmproxy 的直通 CONNECT 可能因上游半连接而推迟 `Flow.Done`；addon 同时监听客户端断开作为该条隧道记录的结束边界。库内部等待可能持续到对端关闭或进程退出，尚不能保证同进程反复重建后的全部内部资源释放，待上游修复。
- 原型 HTTP API 仅只读查询；拒绝未知 authority 和浏览器 Origin。规则/脚本管理与查询通过 `/mcp` 提供，HTTP API 仍只读；重放、导出和持久数据库未实现。
- 验收通过 `make e2e` 的真实 socket、TLS、SSE、容量淘汰和 WebView 窗口操作完成；报告与截图写入隔离工件目录。

## UI 与按域名解密补充

- 未选择请求时不构建详情标签、正文编辑器和操作按钮；Escape、清空、筛选掉或记录淘汰时统一取消选择，递增请求代次，使尚未返回的旧 Get 结果失效。
- CONNECT 记录显示实际连接目标、连接请求头、加密提示及“解密此域名”。未解密时不把 CONNECT 的 200 响应当作站点的 HTTP 内容，也不显示空白的普通正文编辑器。
- UI 的解密操作通过进程内 interception 策略，先原子更新 aicat.yaml 的 gateway.interceptHosts，再更新受锁保护的运行名单。配置写入与 CONNECT 查询使用不同的锁，文件 I/O 不阻塞名单读取。配置保存失败不启用解密。
- 解密匹配针对后续 CONNECT 的精确主机名/IP，覆盖该主机的新连接；不自动扩大到子域名。当前隧道不重新协商 TLS，历史 CONNECT 记录继续显示加密状态。客户端仍需显式信任 aicat CA。
- 进行中的连接在列表显示“进行中”，完整方法列可以显示 CONNECT。列表与详情使用同一请求 ID 和生命周期状态。


应用使用 `internal/certificate` 创建或复用与 go-mitmproxy 文件布局兼容的根身份，签发叶子及 TLS 处理继续交由上游库。证书系统信任和 `internal/systemproxy` 的系统设置操作均不属于 gateway。app 在退出前完成代理恢复，恢复失败不执行原生退出；启动与设置变更共享串行化的连接控制，配置变更沿用唯一 aicat.yaml。显式代理无需启用系统代理。


根证书删除时，app 提供的 `certificate.Authority` 会停止签发并丢弃持有叶子缓存的上游 CA；解密选择同时检查 Authority 是否可用。`Gateway.ResetConnections` 仅负责断开已有会话，监听及普通转发保持可用。CA 缺失或删除未完成时所有新 HTTPS 连接使用 CONNECT 直通。证书系统清理、文件状态、重新创建及授权仍不属于 gateway。

## 规则与插件执行接缝（2026-11-10）

`app` 创建 rewrite 服务，将它与 Recorder 一同交给 traffic addon；9981 的 api addon 先处理本机 API/MCP，控制调用不进入采集与脚本。traffic 服务只接收独立 DTO，不依赖 moejs 或 rewrite。WebView 界面后续直接调用同一规则管理服务，本轮不新增页面。

每条普通 HTTP 或已解密 HTTPS 交换持有一个 Session，固定启动配置；每阶段按当前 URL/method 及规则列表重新匹配。四个钩子由同一 addon 编排，原始 body 在脚本前保存，成功结果在校验后提交，最终写入四个观测位置。未解密 CONNECT 与 Upgrade 不执行插件。`Record.ruleRevision` 标识运行版本，`executions` 保存阶段、规则、插件、耗时、有界日志及错误；`synthetic` 标识自定义响应。插件错误不把成功转发的流量标成失败。

moejs 编译模块共享，每次钩子调用独立 runtime；每条规则通过显式 context 传递状态。默认每阶段总预算 200ms（含排队和该阶段整条规则链）、并发 8、单 runtime 32 MiB、解压后 body 1 MiB。普通错误继续后续规则；阶段预算耗尽则结束该阶段，保留此前成功结果并继续转发。MCP 编译与离线试运行另设并发上限，不与代理争抢执行槽。

流式请求/响应不缓存为完整 body，body 钩子记录 skipped；go-mitmproxy 的 flow 级 stream 标记会使大请求的响应也进入流式路径。SSE 在响应 headers 处强制保持流式。完整 body 有界解压，修改后移除旧编码/摘要并修正长度；未修改保留原始编码与字节。自定义响应在 onRequest 产生，短路上游但显式进入两个响应阶段一次。

管理接口由 `internal/rewrite.Service` 提供 Status、规则增删/启停/排序、插件读取/保存/删除与 Test；配置写入复用 config 的串行原子更新。规则保存在唯一 aicat.yaml 的 rewrite 中，插件代码保存在 Home/plugins；删除未引用插件移入 Home/trash/plugins。管理操作可携带 expectedRevision 防止覆盖并发编辑，已保存与启动版本分开，规则和插件均重启生效。

`internal/mcpserver` 使用官方 Go SDK 的 Streamable HTTP，路径 `/mcp`，Bearer 凭据为 Home/mcp-token；沿用 authority/Origin 检查，JSON 请求上限 2 MiB。工具、base64 数据格式、清单 v1 和 AI 工作流以 [SDK 说明](../internal/mcpserver/guide.md) 为准。定向验收命令与隔离工件见 [开发约定](development.md)。
