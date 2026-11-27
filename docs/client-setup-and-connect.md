# RustDesk 客户端接入与网页连接

适用于 Hub 管理同一套 RustDesk Server OSS，客户端兼容基准为 macOS 和 Windows 上的原生 RustDesk 1.5.3。

## 用户操作

| 场景 | 操作 |
| --- | --- |
| 从 Hub 网页连接设备 | 登录 Hub，在「个人通讯录」找到目标，点击「连接」。浏览器打开本机 RustDesk，用户在客户端完成设备授权。链接携带目标的 ID 服务器和公钥，新主控端可以直接用这套参数发起本次连接。 |
| 在 RustDesk 客户端同步通讯录 | 在 Hub 的客户端接入页点「复制 RustDesk 配置」。打开客户端「设置 → 网络 → ID/中继服务器」，点弹窗标题旁的「导入服务器配置」，确认并在「账号」页登录。打开「个人通讯录」后即可拉取该账号已保存的条目。 |
| 浏览器拦截打开客户端 | 在该设备的操作菜单点「复制连接地址」，在已安装的 RustDesk 中粘贴到目标 ID 输入框并连接。 |

「连接」作为设备行的首个操作。在线状态继续显示查询结果和时间，用户可以从任意状态发起连接。浏览器端可提示正在打开 RustDesk，连接结果由客户端显示。复制配置失败时展示可手动选中的配置串。 [RustDesk 配置导入](https://rustdesk.com/docs/zh-cn/self-host/client-configuration/#3-使用导入或导出设置)

## Hub 配置

管理员在网页「系统设置」保存 RustDesk 服务器参数。API 服务器地址由 Hub 当前的 `public-url` 生成。

- 「ID 服务器」填客户端可访问的 `hbbs`，例如 `rustdesk.example.com:21116`。「中继服务器」可留空，由 RustDesk 选择中继。「服务端公钥」填写同一套 OSS 的 `id_ed25519.pub`。 [RustDesk 客户端网络配置](https://rustdesk.com/docs/zh-cn/self-host/client-configuration/)
- 「启用在线查询」默认使用客户端的 ID 服务器。「在线查询地址」可以填 Hub 能访问的容器地址或局域网主机名。
- 保存时校验地址和公钥。客户端接入页重新打开后可以复制新的配置，通讯录刷新后使用新的连接地址。已部署的 RustDesk 客户端可重新导入配置来采用新参数。
- 尚未配置完整客户端参数时，接入页可复制 API 地址，通讯录可复制设备 ID。本机 HTTP 预览可用于同机测试，跨设备使用对应的 HTTPS 地址。

## 兼容接口

**配置串。** Hub 用 RustDesk 导出的四个 JSON 字段 `host`、`relay`、`api`、`key` 生成配置串。按 1.5.3 的格式做 URL-safe Base64 编码，再反转字符顺序。客户端通过内置导入读取并确认四项设置。1.5.3 的配置类链接处理面向移动端，桌面端使用已有导入界面。 [配置串编码](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common.dart#L2975-L3005) · [配置类链接处理](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common.dart#L2424-L2445)

**连接链接。** 网页生成 `rustdesk://connect/<设备ID>@<ID服务器>?key=<URL编码的公钥>`。RustDesk 1.5.3 将服务器和公钥用于本次连接，并按服务端返回的中继地址处理需要中继的会话。复制到客户端目标输入框的文本为 `<设备ID>@<ID服务器>?key=<公钥>`。链接只使用这三个值，设备密码或手动确认由客户端处理。 [链接解析和公钥参数](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common.dart#L2469-L2499) · [连接服务器选择](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/src/client.rs#L469-L489) · [中继地址处理](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/src/client.rs#L1014-L1037)

接入页使用已登录会话调用 `GET /api/v1/client-setup`，取得配置串和可手动填写的参数。网页通讯录按当前设置在有效条目上附加连接链接和可粘贴的连接地址。内容按会话认证和通讯录归属返回。

生成链接时使用本账号已保存并规范化的设备 ID，保留前导零。连接模块识别标准 RustDesk ID 和自定义 ID，结构化处理服务器地址，为公钥参数做 URL 编码。RustDesk 1.5.3 从 Dart URI 的编码路径提取目标，IPv6 地址的括号可能以转义形式传给连接解析器，所以 IPv6 ID 服务器提供可直接粘贴的连接地址。历史条目如包含自带服务器的完整地址，提供复制原值的操作。 [Dart URI 路径规则](https://api.dart.dev/dart-core/Uri/path.html)

## 实机验收

1. 在开启 Hub 的测试环境配置一套可达的 OSS。把 Hub 复制出的配置串导入原生 RustDesk 1.5.3，核对四项字段，登录后看到指定的通讯录条目。用客户端导出样例核对编码，覆盖空中继、自定义端口以及含 `+`、`/`、`=` 的公钥。
2. 在 macOS 和 Windows 安装注册了 `rustdesk:` 的原生客户端，分别用常用浏览器测试客户端已打开和未打开时的唤起。新主控端使用默认服务器配置，在 Hub 点击「连接」，通过目标测试设备授权后查看桌面。分别验证直连和 OSS 中继。 [macOS 协议声明](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/macos/Runner/Info.plist#L22-L35) · [Windows 安装时注册协议](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/src/platform/windows.rs#L1615-L1620)
3. 核对浏览器禁止唤起、剪贴板不可写、服务器使用域名或 IPv6、配置不完整、旧条目以及设备状态为未知时的操作。浏览器拦截时，复制连接地址并在客户端粘贴可以继续完成设备授权。

已核对 1.5.3 的源码和当前 Mac 安装包的协议声明。配置串由 Hub 生成后的导入，以及从浏览器实际发起连接的双端结果，需按上述场景验收。
