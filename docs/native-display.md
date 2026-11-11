# 虚拟工作屏原生验证

这是第一阶段原型，不提供产品配置、管理 UI、管理 CLI、MCP 或自动搬移真实应用。
只有 `displayprobe` build tag 会编译原生验证流程；正常桌面 release 现已包含手动管理能力，
见[虚拟工作屏](work-screen.md)。纯 Go CLI 不包含私有显示器实现。

## 验证内容

同一份原生桥接分别运行在真实 Wails 托盘进程和真实 `aicat serve` 前台进程内。
两个入口都启动实际 `app.Serve`，使用隔离 HOME 中的 loopback 网关作为运行就绪证据。
`serve` 的验证构建额外启动 AppKit 主事件循环；生产 `serve` 的行为保持原样。

验证使用独立进程中的临时 AppKit 应用，检查 bundle ID、PID、唯一窗口和 AX/CG 几何匹配后才允许搬移。
不会使用微信、企业微信、用户文档或其他真实窗口。测试应用不是产品 runtime 依赖。

完整矩阵为 Wails / serve × 1920×1080 普通 / HiDPI：

1. 确认已有 Accessibility、Screen Recording 权限和实体主屏；缺少权限时停止，不请求或修改系统权限。
2. 记录原有拓扑和测试窗口原始位置，保存实体屏上的真实窗口截图。
3. 主线程创建 `CGVirtualDisplay`，后台串行校验扩展模式、分辨率与原有拓扑。
4. 主线程通过跨进程 AX 搬移测试窗口，同时读取 AX 和 WindowServer 几何直到一致。
5. 通过 `screencapture -l` 获取虚拟屏内的真实窗口截图，检查橙色和青色标记，拒绝空白图。
6. AX 恢复原始位置和尺寸，确认 CG/AX 收敛后再截图。
7. 主线程销毁屏幕，等待原有拓扑恢复，并验证移除后的状态。
8. 进程退出后用新进程独立读取显示器清单，与操作前完整比较。

## 运行

需要 macOS 的已登录、未锁定图形会话，以及 Xcode Command Line Tools 和 Go。
本地构建不执行签名；本机权限若不适用于该构建，工件会明确记录失败，不自动重置权限。
测试会临时扩展桌面，建议在无人同时调整显示设置时运行。

```sh
python3 scripts/display-native-e2e.py
```

结束时可针对指定入口、缩放重跑；每次输出独立目录，避免混用旧工件：

```sh
python3 scripts/display-native-e2e.py --mode serve --scale 1x
python3 scripts/display-native-e2e.py --mode wails --scale 2x
```

`--output /绝对路径/新目录` 可指定输出位置。脚本拒绝覆盖已有工件目录。
输出目录包含编译日志、带来源 hash 的 `summary.json`、两个临时 app bundle，以及各 case 的：

- `report.json`：每一步的时间、主线程证据、AX/CG 实测位置、显示器清单和成功状态；失败与清理结果同样保存。
- `before.png`、`virtual.png`、`restored.png`：来自实际 WindowServer 窗口的截图，不是 view 离屏渲染。
- `topology-before.json`、`topology-after.json`：独立进程读取的完整拓扑比较。
- `aicat.log`、`fixture.log` 与 `home/.aicat/`：运行日志及隔离的配置、CA 和运行数据。

工件不保存物理显示器序列号、UUID、名称或用户窗口标题。截图只针对本次测试进程的唯一窗口。
系统代理始终关闭，不安装证书、不写真实用户 Keychain。所有窗口操作与现有消息渠道使用相同 GUI 文件锁。

## 失败边界

开发前识别的失败方式包括：主线程/事件循环缺失；私有 API 不存在或改变；模式缓存未更新；
创建改变原有镜像或主屏；外部拓扑变化；权限缺失；AX/CG 窗口匹配不唯一；AX 拒绝搬移；
截图空白；恢复未确认；显示器销毁异步未结束；服务或测试进程提前退出。

普通失败仍尝试恢复测试窗口并销毁自建屏幕；未确认恢复时原生桥接拒绝主动销毁。
脚本超时后的进程终止属于兜底清理，会判失败，不能当作正常恢复的证据。
不主动恢复用户并发修改的拓扑，拓扑不同即停止后续 case 并保留报告。

本验证不证明真实微信或企业微信的适配、任意应用兼容、鼠标键盘输入、焦点隔离、熄屏、锁屏、
省电或长时间无人值守。跨进程 AX 和截图通过后，仍需针对各业务应用单独验收。

## 来源与构建边界

`internal/workscreen/VDVirtualDisplay.*`、`YMDisplayTopology.*` 来自 yomado，固定提交：
https://github.com/chekusu/yomado/tree/a09cc7e3c749a077c7b9eb1e94959dd05a8d26c4

上述上游源码仅加 Go build constraint，保留内部实现与命名。
许可证见 `internal/workscreen/YOMADO-LICENSE.txt`；本原型没有引入其 UI、更新器、Watchdog 或其他外部运行时。
Go 代码只协调顺序和记录报告，所有原生对象由 ARC 持有。

原型验证结果只证明下述范围。后续正式接入和验收分别见[虚拟工作屏](work-screen.md)。

## 2026-10-20 实机结果

在 Apple Silicon、macOS 27.2、SDK 27.0、Go 1.27.1 上通过完整四组矩阵。
原有桌面为一块实体主屏；不据此推断多实体屏、镜像组或其他 macOS 版本的兼容性。

| 入口 | 逻辑分辨率 | 虚拟屏实际像素 | 目标窗口截图像素 | 创建 / AX 搬移 / 恢复 / 销毁 |
| --- | --- | --- | --- | --- |
| Wails 托盘 | 1920×1080 | 1920×1080 | 480×332 | 全部通过 |
| Wails 托盘 HiDPI | 1920×1080 | 3840×2160 | 960×664 | 全部通过 |
| `aicat serve` 验证构建 | 1920×1080 | 1920×1080 | 480×332 | 全部通过 |
| `aicat serve` 验证构建 HiDPI | 1920×1080 | 3840×2160 | 960×664 | 全部通过 |

四组均实测：窗口从 `(90,132,480,332)` 搬至虚拟屏中的 `(1830,120,480,332)`，
再精确恢复原始位置和尺寸；AX 与 WindowServer 的观测一致。
创建、搬移、恢复、销毁在主线程执行，拓扑收敛等待在后台执行。
三个阶段的真实截图均通过颜色标记校验，退出后的独立进程读取确认拓扑与测试前完全相同。
实体屏截图为 Display P3，验证器先转换 bitmap 到 sRGB 再检查颜色，避免误报。

本次完整工件位于仓库根目录下：
`build/display-native-e2e/20261020T105107.283234Z/summary.json`。
每组子目录中的报告和三张 PNG 可单独核对，summary 包含 binary/source SHA-256 与工具链版本。
本地仅保留这组最新通过工件，历史失败和重复构建工件已清理。

正式运行体仍是托盘进程；本原型没有创建独立 server 或常驻 helper。
两个入口按顺序单独启动临时测试实例，独立的 AppKit 进程仅作为跨进程 AX 测试目标。
Wails 可直接使用现有事件循环，前台 `serve` 则需要本原型补充的原生事件循环。
