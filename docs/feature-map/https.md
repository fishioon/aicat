# 证书与 HTTPS 解密

用户只解密明确选择的精确域名；未选择目标保持 CONNECT 直通。证书安装、信任、导出与删除围绕同一个本机 CA 身份管理。

## Sub-features

- `https-install`：安装或重新检测根证书，展示实际信任与失败状态。
- `https-manage`：查看真实名称、有效期、SHA-256，导出公开证书，打开钥匙串。
- `https-domains`：在代理设置中增删解密域名/IP；`*.example.com` 匹配任意层级子域名，不含域名本身。
- `https-connect`：从未解密 CONNECT 详情就地启用域名，支持客户端自行信任。
- `https-delete`：确认后删除当前用户信任和本地身份，失败重试，删除后重新安装生成新身份。

## How to get to it (user POV)

- 设置 → 代理 → 本机证书：安装证书、查看、导出、在钥匙串中管理、重新检测、删除。
- 设置 → 代理 → 解密域名：添加域名，或移除已有域名。
- 流量 → 未解密 CONNECT 记录 → 响应侧 Header 或 Raw → 启用此域名 → 安装证书/客户端已单独信任。域名已被精确或通配条目覆盖时，这里显示覆盖它的条目，没有启用按钮。

## Driving it with Playwright and TLS fixture

Preconditions:

- 使用独立 Home、OS 信任适配器、测试 TLS 上游；最初解密名单为空。准备新的 CONNECT 连接，不能复用旧连接验证名单变更。

- **默认直通。** 用配置了代理的 HTTPS 客户端连接测试上游；列表显示 CONNECT，选中后响应侧显示“内容未解密”。客户端看到上游原证书。
- **安装。** 设置 → 代理，点击 `#certificate-install`；无应用内二次“继续”确认。OS fixture 模拟取消、失败和成功，每次都检查 `#certificate-status`、错误提示及重试按钮；仅成功且实际验证通过显示“已信任”。重试期间 CA 指纹保持。
- **管理。** 点击 `#certificate-details`，核对名称、有效期和指纹；`#certificate-recheck` 更新状态。点 `#certificate-export`，读取本次目录的 `aicat-root-ca.pem`，核对指纹一致且不含私钥；点 `#certificate-keychain`，在 fixture 核对打开操作被请求。真实钥匙串 UI 需要另记原生结果。
- **名单入口。** 点击 `#add-decryption-domain`，在 `#decryption-domain-input` 输入测试主机（如 `localhost`），在 `#dialog` 点击“添加”；名单和 `gateway.interceptHosts` 同时更新。用新连接再次发 HTTPS 请求，核对客户端看到测试 aicat CA、列表显示已解密 HTTP 请求与正文。
- **详情入口。** 清除目标名单，建立新的 CONNECT 并选中；分别在 Header 点击 `#enable-decryption`、在 Raw 点击 `#enable-decryption-raw`。弹窗保留目标域名；选择“安装证书”完成授权后启用，或在客户端已指定测试 CA 时选择“客户端已单独信任”。分别新建连接核对解密，历史 CONNECT 仍保持原状态。
- **移除与非法输入。** 使用 ID `remove-decryption-` 加实际域名的按钮移除；新连接恢复直通。添加 URL、带端口字符串或 `*.` 前缀以外的通配写法（`api.*.com`、`*example.com`、`*.`）时应报错且不写入配置。
- **通配条目。** 添加 `*.example.com` 后名单原样显示该条目；新建到 `a.example.com`、`a.b.example.com` 的连接被解密，`example.com` 仍直通。选中被覆盖域名的历史 CONNECT，Header 与 Raw 分别显示 `#decryption-covered`、`#decryption-covered-raw`，不出现启用按钮。匹配与校验的单元测试：`go test ./internal/config ./internal/app -run Intercept`。
- **删除与重装。** 点 `#certificate-delete`，先取消确认核对指纹不变，再确认。删除成功后本地身份与 fixture 用户信任清除，既有连接断开，新 HTTPS 直通，历史记录和域名名单保留；重启不自动重建。再次点击“安装证书”得到新指纹。模拟删除失败时显示“删除未完成”，重启仍直通，重试完成清理。

## Gotchas

- 添加域名不自动信任 CA；macOS 系统信任也不代表所有客户端都使用该信任库。客户端可显式指定公开 CA 文件。
- 名单只影响新连接；旧隧道与历史记录不会变成已解密数据。
- 当前 CA 名称可能来自既有身份；以实际证书和指纹为准，不按名称判定信任或删除。
- 导出只包含公开证书；私钥不导入钥匙串。取消安装后重试复用已生成身份，删除成功后重装才生成新身份。
- 原生保存/授权面板在验收中被 OS 边界适配，不把模拟成功称为真实 macOS 授权通过。
