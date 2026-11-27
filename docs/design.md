# RustDesk Hub 首版技术设计

以 [首版需求](requirements.md) 为基准。RustDesk Hub 提供账号、会话和个人通讯录。原生客户端的 ID 服务器、中继服务器及 Key 对应 RustDesk Server OSS，设备连接通过 RustDesk 的协商、中继及被控端授权流程完成。

原生接口以 **RustDesk 1.5.3** 的提交 `95cca3f661f635b3cc85c04935fe8d96d8f432ef` 为兼容基准。下文的 HTTP 方法、参数和解析规则来自该版本源码。存储、会话有效期和错误映射属于本项目设计。Mac 与 Windows 实机联调是发布验收。 [RustDesk 1.5.3 发布记录](https://github.com/rustdesk/rustdesk/releases/tag/1.5.3)

## 技术栈选型

| 技术栈 | 版本号 |
| --- | --- |
| React | [19.3.1](https://github.com/react/react/releases/tag/v19.3.1) |
| Vite | [8.3.8](https://github.com/vitejs/vite/releases/tag/v8.3.8) |
| antd | [6.7.1](https://github.com/ant-design/ant-design/releases/tag/6.7.1) |
| Go | [1.27.4](https://go.dev/doc/devel/release#go1.27.0) |
| Echo | [v5.4.7](https://github.com/labstack/echo/releases/tag/v5.4.7) |
| GORM | [v1.31.2](https://github.com/go-gorm/gorm/releases/tag/v1.31.2) |

## 部署与模块

Hub 启动时读取工作目录中的 `config.yaml`，也可在子命令前用 `-config` 指定文件。设置按内置默认值、文件、显式命令行参数的顺序覆盖。YAML 字段与启动参数同名，未知字段会使启动失败。相对路径按启动目录解析。

Vite 构建完成后，Go 通过 `go:embed` 将 `web/dist` 纳入 Hub 可执行文件。同一个进程提供 API 和管理页。服务启动时校验内嵌的 `index.html`、它引用的资源、Vite 的 `.vite/manifest.json` 及清单中的所有文件和分块依赖。校验失败时提示重新构建网页并编译 Hub。SQLite 数据存放在配置的文件中，运行时由 `/healthz` 检查其可用性。 [Vite 生产构建](https://vite.dev/guide/build.html) · [Vite 构建清单](https://vite.dev/config/build-options.html#build-manifest)

首页使用 `Cache-Control: no-store`，成功响应的指纹资源使用一年不可变缓存。网页遇到异步资源加载错误时保留导航，并提供整页重新加载。

使用 HTTPS 反向代理时，默认监听回环地址作为上游。跨主机代理使用 `-behind-proxy` 显式启用远端 HTTP 上游，监听接口及网络策略应限制为代理可达。直接监听非回环地址的 HTTPS 服务须配置 `-tls-cert` 与 `-tls-key`。本机预览可以使用回环 HTTP。

客户端打开「设置 → 网络 → ID/中继服务器」，在弹窗的「API 服务器」填写 Hub 对外的 HTTPS 地址，例如 `https://hub.example.com`。各原生接口从该地址拼接 `/api/...`。建议通过 HTTPS 443 暴露 Hub。RustDesk 1.5.3 默认从 HTTPS 地址移除显式端口 21114，Hub 因此拒绝该公开地址。公开 URL 和浏览器 Origin 使用相同规则处理域名、IPv6 和默认端口。API 地址为空时，客户端可能从 ID 服务器推导出另一地址。Windows 安装文件名携带 API 配置时也可能优先选用该地址，联调时核对请求实际到达的主机。 [客户端 API 地址选择](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/src/common.rs#L1270-L1312)

Hub 使用与域名匹配、两端客户端都信任的 HTTPS 证书。首版以原生 1.5.3 的默认 HTTP 通道为验收基准，两端的「允许不安全的 TLS 回退」设为关闭。发布验收同时确认受信证书下可以登录，以及证书校验失败时客户端拒绝连接。 [RustDesk TLS 回退设置](https://rustdesk.com/docs/en/self-host/client-configuration/advanced-settings/#allow-insecure-tls-fallback)

| 模块或适配器 | 接口与职责 |
| --- | --- |
| 账号 | 校验密码、建立和验证会话、单端退出、改密并失效旧会话。网页和原生客户端共用账号，分别签发各自的会话。初始管理员可以创建、停用和恢复普通账号及重置密码。 |
| 个人通讯录 | 根据已登录账号读写唯一的个人通讯录。集中处理条目去重、按字段更新、标签引用和删除。 |
| RustDesk HTTP 适配器 | 按 1.5.3 的路径和报文解析请求，将 Bearer 令牌转换为已验证的账号和会话，将业务结果映射为客户端预期的状态和响应体。 |
| 网页管理适配器 | 用网页会话进入管理页，调用同一组账号和通讯录操作。提供登录、改密、通讯录维护、API 地址复制和最近请求结果。 |
| OSS 状态查询 | 按系统设置批量查询通讯录中设备的注册状态，并向网页返回结果。 |

两种入口在各自的请求格式上完成转换，鉴权后的账号归属和通讯录数据规则由共享模块执行。事务负责维持业务约束，HTTP 请求类型和数据库类型在各自的适配器内使用。入口按路径保留请求方法、Authorization 头与请求体，包括带 JSON 数组的 `DELETE`。原生 JSON 响应使用 `application/json` 和 UTF-8 编码，响应体按具体端点的格式直接返回。

## 登录与会话契约

| 原生请求 | 请求内容 | Hub 响应 |
| --- | --- | --- |
| `GET /api/login-options` | 打开登录框后查询第三方登录选项。无需会话。 | `200`，JSON 根数组 `[]`。 |
| `POST /api/login` | JSON 对象，账号密码登录的 `type` 为 `account`。字段见下例。 | 成功返回 `200` 与 `type`、`access_token`、`user`。凭据错误返回 `401` 与可显示的 `error` 字符串。 |
| `POST /api/currentUser` | `Authorization: Bearer <token>`，JSON 对象包含 `id` 和 `uuid`。客户端用本地令牌恢复账号。 | `200`，整个响应体是用户对象。令牌过期、撤销或验证失败时返回 `401`。 |
| `POST /api/logout` | 同一个 Bearer 令牌，JSON 对象包含 `id` 和 `uuid`。 | 成功撤销该令牌对应的会话，返回 `200` 空体。重复退出的效果相同。缺少令牌或令牌格式错误返回 `401`。 |

打开客户端登录框会拉取 `login-options`。返回空数组后，客户端可按内置账号密码流程登录。成功登录时客户端根据 `type: "access_token"` 保存令牌，通过 `user.name` 建立已登录状态。`currentUser` 的响应是下例中的 `user` 对象本身，`401` 会触发客户端清除本地令牌和通讯录状态。服务暂不可用时返回对应的 `5xx`，让客户端按请求失败处理。 [登录框和令牌保存](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common/widgets/login.dart#L737-L854) · [登录与恢复源码](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/user_model.dart#L53-L113) · [登录、退出和选项请求](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/user_model.dart#L170-L264) · [请求和响应字段](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common/hbbs/hbbs.dart#L133-L197)

账号密码请求使用 JSON 正文。1.5.3 默认 HTTP 服务补齐 `application/json` 请求头，Hub 按 JSON 正文解析兼容请求。 [客户端 HTTP 服务](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common/http_service.dart)RustDesk 本机的 `id` 和 `uuid` 是客户端自报信息，按字符串接收。`autoLogin` 表明该流程希望保留会话，`deviceInfo` 随请求附带。

```json
{
  "username": "owner",
  "password": "<account-password>",
  "id": "123456789",
  "uuid": "<client-uuid>",
  "autoLogin": true,
  "type": "account",
  "deviceInfo": {}
}
```

登录成功响应示例。网页管理权由网页会话在服务端判定。原生响应中的 `is_admin` 固定为 `false`。

```json
{
  "type": "access_token",
  "access_token": "<opaque-token>",
  "user": {
    "name": "owner",
    "display_name": "Owner",
    "status": 1,
    "is_admin": false
  }
}
```

### 会话实现

- 部署时由管理员在本地设置初始密码，在同一事务创建账号和个人通讯录。数据库保存带随机盐的 Argon2id 密码哈希。登录按连接来源限流，改密按已登录账号独立限流。
- 每次登录用系统安全随机源生成独立的不透明令牌。数据库保存令牌的 SHA-256 摘要、账号、入口类型、失效时间和撤销状态。原生入口通过 Bearer 头提交令牌，网页入口通过带 Secure、HttpOnly 和 SameSite=Lax 属性的 Cookie 提交会话凭据。网页写入校验 CSRF。
- 已登录页面在读取时附带该页面的会话证明。Cookie 切换后，旧页面重新校验身份。同源标签页在登录、退出或修改密码时通知其他标签页刷新会话。
- 首版默认有效期为原生会话 30 天、网页会话 24 小时。验证现有令牌即可恢复登录。每个令牌只在签发时对应的入口使用。适配器按协议接收 `id`、`uuid` 和 `deviceInfo`，账号归属由会话确定。
- 改密在同一事务更新密码哈希并递增账号的会话版本。签发时记录该版本，验证时与当前账号版本比对。登录写入前确认用于校验的密码版本，已鉴权的写操作在事务内重验会话，以处理与改密、退出并发的请求。

### 多账号管理

现有的初始账号是管理员。升级时补齐账号状态和账号名唯一约束，账号 ID、密码哈希、个人通讯录 GUID 和会话版本沿用原值。管理员权限按初始账号 ID 判断，以 `isAdmin` 字段返回给网页。原生接口继续按 1.5.3 的用户格式及通讯录协议返回。

管理员使用 `GET /api/v1/users` 查询账号和状态，使用 `POST /api/v1/users` 创建普通账号及与之一对一的空通讯录。通过 `PATCH /api/v1/users/:id` 修改状态，通过 `PUT /api/v1/users/:id/password` 重置密码。写入校验网页会话、Origin 和 CSRF。账号状态变化或重置密码在事务内递增会话版本。登录在签发令牌前再次校验当前状态和版本。

通讯录读取和写入均由会话账号确定归属。原生请求携带 GUID 时，也校验 GUID 属于该账号。条目、标签和在线查询限制在该用户的个人通讯录内。全局请求诊断通过管理员会话访问。
- 服务端收到有效退出请求后撤销该请求对应的会话。1.5.3 的退出请求有 2 秒超时，客户端在请求失败时也会清除本地登录状态。请求未送达时，服务端会话按原有效期管理，也可通过现有的网页改密流程使其失效。 [客户端退出行为](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/user_model.dart#L170-L189)

## 个人通讯录读取契约

首次读取或强制刷新列表时，客户端先请求 `POST /api/ab/personal`。以 `200` 返回非空且稳定的字符串 `guid` 后，客户端进入按条目读写的流程。该端点返回 `404` 会让客户端切换到旧版 `/api/ab` 整簿接口。空通讯录同样有 `guid`。缺失会话用 `401` 表达，存储故障用 `5xx` 表达。 [客户端选择通讯录协议](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L142-L200) · [个人通讯录探测](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L260-L293) · [旧版整簿实现](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L1008-L1097)

下列请求均携带 Bearer 令牌。客户端使用空请求体并可显式发送 `Content-Length: 0`。通讯录所有者由服务端根据会话和 `guid` 核验。

| 原生请求 | 参数 | `200` 响应体 |
| --- | --- | --- |
| `POST /api/ab/personal` | 空体。 | `{"guid":"<book-guid>"}`。 |
| `POST /api/ab/settings` | 空体。 | `{"max_peer_one_ab":0}`。`0` 在客户端表示不限制条目数。 |
| `POST /api/ab/shared/profiles` | 查询参数 `current`、`pageSize`。 | `{"total":0,"data":[]}`。 |
| `POST /api/ab/peers` | 查询参数 `ab` 为通讯录 `guid`。页码 `current` 从 1 开始，客户端发送 `pageSize=100`。 | 根对象为 `{"total":N,"data":[...]}`，`total` 是整本通讯录的条目数。 |
| `POST /api/ab/tags/{guid}` | `guid` 置于路径，空体。 | JSON 根数组，每项为 `{"name":"家里","color":4283215696}`。无标签时返回 `[]`。 |

个人条目分页示例。保留设备 ID 的字符串形式，`tags` 是标签名数组。标签列表响应中的 `color` 是 ARGB 整数。

```json
{
  "total": 1,
  "data": [
    {
      "id": "012345678",
      "alias": "书房 Windows",
      "tags": ["家里"],
      "note": "家用电脑"
    }
  ]
}
```

每页按稳定排序读取，在一次读取内取得 `total` 和页数据。首版同步以客户端主动拉取为界。客户端刷新通讯录后取得已提交的状态，分页期间发生并发修改时由下一次完整拉取收敛。 [通讯录设置及共享列表](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L230-L355) · [条目与标签读取](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L1386-L1544) · [条目字段解析](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/peer_model.dart#L26-L48) · [刷新入口](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common/widgets/peer_tab_page.dart#L212-L228)

## 个人通讯录写入契约

这些原生写操作成功时返回 **HTTP `200` 且响应体为空**。客户端的写操作解析器会把 `204` 和带 `{}` 的 `200` 判为错误。失败响应使用非成功状态码和 `{"error":"可显示的错误信息"}`。部分错误分支直接显示该字符串，返回消息使用可公开的文字。 [写操作响应解析](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L2002-L2031)

| 原生请求 | JSON 请求体 | 行为 |
| --- | --- | --- |
| `POST /api/ab/peer/add/{guid}` | 单个对象，例如 `{"id":"012345678","alias":"书房 Windows","tags":["家里"],"note":"家用电脑"}`。 | 条目不存在时新增。条目存在时重复添加返回 `200` 空体，客户端随后拉取现有条目。 |
| `PUT /api/ab/peer/update/{guid}` | `{"id":"012345678","alias":"新名称"}`，也可携带 `note` 或 `tags`。 | 按提交的字段更新已有条目。空字符串清空别名或备注，`[]` 清空条目标签。 |
| `DELETE /api/ab/peer/{guid}` | ID 数组，例如 `["012345678"]`。 | 在同一事务移除指定条目及其标签关系。 |
| `POST /api/ab/tag/add/{guid}` | `{"name":"家里","color":4283215696}`。 | 新增标签，重复新增时保留现有标签。 |
| `PUT /api/ab/tag/rename/{guid}` | `{"old":"家里","new":"家庭"}`。 | 更新标签名称并保留其条目关系。同名冲突返回 `409`。 |
| `PUT /api/ab/tag/update/{guid}` | `{"name":"家庭","color":4283215696}`。 | 更新现有标签颜色。 |
| `DELETE /api/ab/tag/{guid}` | 标签名数组，例如 `["家庭"]`。 | 在同一事务移除标签及其条目关系。 |

按字段更新保留缺席字段的值，同字段以最后成功提交的写入为准。网页标签编辑比较打开时的表单快照，只提交实际改动的名称或颜色。网页编辑和移除使用稳定标签 ID。缺少 ID 的旧网页请求返回 `409` 并提示刷新。原生请求按 1.5.3 契约继续使用名称。编辑一个已移除的条目返回 `404`，重新加入使用添加操作。删除不存在的条目按已删除处理。关联的标签名须属于同一本通讯录。

共享业务入口在添加、定位、移除及按 ID 搜索时去掉普通空格，并保留文本和前导零。按别名搜索保留搜索词中的空格。升级在同一个 SQLite 事务中逐册检测规范化后的重复 ID、更新无冲突的旧记录并完成结构迁移。遇到冲突时提示冲突 ID，保留原记录和标签关系，管理员确认后再升级。标签创建和改名返回 `400` 拒绝保留名称 `Untagged`。旧标签可以改名、移除或清理引用。已有引用可保留到用户处理时，新关联要求先改名。 [原生连接处理](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common.dart#L2626-L2629) · [客户端标签过滤](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common/widgets/peers_view.dart#L528-L538)

客户端选择标签时使用标签列表中已有的名称，网页添加新标签也走同一套规则。原生客户端批量给条目改标签时逐条发送请求，每个 HTTP 请求独立提交。 [条目添加和逐字段修改](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L1548-L1646) · [批量删除与标签操作](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L1751-L1876)

原生写请求按服务端处理时的条目状态执行。协议没有请求标识或条目版本，删除后到达的延迟添加会按新增处理。服务端提交后才返回成功。通信中断时，网页提示结果待确认，原生客户端使用已有的刷新入口先核对当前状态，再由用户决定是否重新提交。Hub 入口代理只对读取路由配置自动重试。 [原生添加请求](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L1548-L1577) · [原生刷新入口](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common/widgets/peer_tab_page.dart#L212-L228)

### 处理客户端附带字段

通讯录条目的持久化字段为 `id`、`alias`、`tags`、`note`。标签颜色对应原生标签界面的操作。1.5.3 的最近会话同步可能另外发送设备的 `username`、`hostname`、`platform` 与 `hash`。远控密码相关字段也可能出现在添加或更新请求中。

- 添加条目或更新受支持的字段时，只映射通讯录字段。其它数据在适配器中丢弃，响应中也只序列化受支持的字段。
- 更新没有包含任何可同步字段时，先校验条目存在，再检查凭据字段。附带非空 `hash` 或 `password` 返回 `422`，提示设备凭据同步未启用。仅上报其它设备属性或清空凭据字段时返回 `200` 空体。
- 个人通讯录卡片根据 `hash` 是否非空显示「忘记密码」。Hub 的通讯录响应省略该字段，本机密码管理以「最近连接」或「收藏」的入口为准。这两个入口根据本机保存状态显示菜单，执行时会清除本机密码。 [通讯录菜单](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common/widgets/peer_card.dart#L1207-L1220) · [最近连接菜单](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common/widgets/peer_card.dart#L1084-L1092) · [收藏菜单](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common/widgets/peer_card.dart#L1127-L1133)
- 管理端接入说明列出设备 ID、别名、标签和备注，并说明客户端管理远控凭据。认证和同步诊断通过元数据记录结果，请求体和凭据值留在请求处理过程内。

[客户端携带密码与备注](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L374-L395) · [设备凭据与最近会话更新](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/ab_model.dart#L1648-L1747) · [忘记本机密码](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/common/widgets/peer_card.dart#L948-L965)

## 设备在线状态

管理员通过「系统设置」启用查询。查询地址默认使用客户端的 ID 服务器，也可由管理员指定 Hub 可达的地址。Hub 使用 OSS 1.1.16 的 TCP 在线状态查询协议，连接 ID 端口减一的查询端口，默认是 `21115`。同机部署时使用非回环地址，因为 OSS 1.1.16 将回环来源交给本地控制台。 [OSS 查询入口](https://github.com/rustdesk/rustdesk-server/blob/1.1.16/src/rendezvous_server.rs#L1094-L1134) · [1.5.3 客户端查询](https://github.com/rustdesk/rustdesk/blob/1.5.3/src/client.rs#L5380-L5426)

网页独立调用 `GET /api/v1/address-book/online-status?id=012345678&id=234567890`，使用网页会话鉴权。每次允许 1 至 100 个 ID。Hub 按通讯录归属取出匹配的已保存条目，在读取事务完成后向 `hbbs` 发送一批 `OnlineRequest`。响应的位图按设备顺序、每字节从高位到低位映射。OSS 1.1.16 将最近 30 秒内注册的设备视为在线。 [OSS 状态判定](https://github.com/rustdesk/rustdesk-server/blob/1.1.16/src/rendezvous_server.rs#L785-L808) · [报文字段](https://github.com/rustdesk/hbb_common/blob/83419b6549636ee39dacef7776c473f5802e08d6/protos/rendezvous.proto#L156-L193) · [TCP 帧格式](https://github.com/rustdesk/hbb_common/blob/83419b6549636ee39dacef7776c473f5802e08d6/src/bytes_codec.rs#L114-L132)

响应包含配置标记 `configured`、查询完成时间 `checkedAt` 和按 ID 索引的 `statuses`。查询超时为 2 秒。连接失败、响应无效或未配置时，已保存条目的状态为 `unknown`。未发起查询时 `checkedAt` 为 `null`。网页在加载当前页后发起查询，结果只用于该页显示。状态和查询时间无需入库或同步，`/healthz` 继续反映 Hub 本身的可用性。

## 伴随请求

原生客户端刷新账号时会并行更新通讯录和分组模型。配置自建 API 地址后，部分桌面运行状态还会触发心跳、设备信息或审计上报。这些请求由独立路由响应，个人通讯录的状态只由已鉴权的通讯录操作更新。 [账号刷新](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/user_model.dart#L150-L167) · [分组模型](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/group_model.dart#L39-L78) · [心跳与设备信息](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/src/hbbs_http/sync.rs#L98-L135)

| 原生请求 | 首版响应 |
| --- | --- |
| `GET /api/device-group/accessible`、`GET /api/users`、`GET /api/peers` | 验证 Bearer 会话后返回 `200` 和 `{"total":0,"data":[]}`，让客户端分组页可以正常显示空结果。 |
| `POST /api/heartbeat` | 接收有上限的请求体并丢弃，返回 `200` 与 `{}`。 |
| `POST /api/sysinfo` | 接收有上限的请求体并丢弃，返回 `200` 空体。只有特定的 `SYSINFO_UPDATED` 响应会使客户端确认设备信息已入库，本设计按客户端既有节奏处理重试。 |
| `POST /api/audit/conn`、`POST /api/audit/file`、`POST /api/audit/alarm` | 接收有上限的请求体并丢弃，返回 `200` 空体。 |

后三类请求可在未登录状态下出现，响应内容固定，不发布策略、指令或设备信息。入口限制请求体大小和访问频率。这些伴随请求独立于管理端的操作诊断。 [分组查询路径](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/flutter/lib/models/group_model.dart#L103-L254) · [设备信息确认及心跳响应解析](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/src/hbbs_http/sync.rs#L211-L277) · [连接与文件审计路径](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/src/server/connection.rs#L1534-L1571) · [告警上报路径](https://github.com/rustdesk/rustdesk/blob/95cca3f661f635b3cc85c04935fe8d96d8f432ef/src/server/connection.rs#L1627-L1650)

## 数据持久化与诊断

| 数据 | 关联和约束 |
| --- | --- |
| 账号与会话 | 账号名唯一，账号持有密码哈希、启停状态和会话版本。会话通过唯一令牌摘要定位，关联账号、入口、签发版本、有效期与撤销状态。 |
| 系统设置 | SQLite 单行保存 RustDesk 客户端参数和在线查询参数。管理员读取和保存时验证网页会话，保存同时验证 CSRF。首次启动一次性迁入旧 `rustdesk` 和 `hbbs`。 |
| 个人通讯录 | 初始化时生成一个稳定 `guid`，账号与通讯录一对一。 |
| 条目 | `(通讯录, RustDesk ID)` 唯一。ID 按文本保存，别名和备注为字符串。 |
| 标签和条目标签关系 | 标签在通讯录内具有稳定 ID 和唯一名字。条目只能关联同册标签。改名保留 ID，移除标签或条目时一并移除关系。 |
| 诊断记录 | 保留最近 100 条账号登录、会话恢复、退出、改密、用户管理和通讯录读写结果。记录操作、时间、来源、状态、请求 ID 和适用的错误码。 |

通过 GORM 访问 SQLite。新增、编辑、删除和标签变更使用事务及数据库约束。去重由唯一约束保障。更新时显式指定提交的字段，空字符串按清空写入。标签关系替换与条目更新一起提交。运行时通过 SQLite 持久存储恢复账号和通讯录，保留跨重启所需的会话记录。诊断信息使用独立的结果记录，返回的业务结果以对应事务是否成功为准。已匹配的诊断路由在限流和请求体检查外层记录结果。对已知长度和分块流式请求均执行体积检查，`413` 按响应头中的请求 ID 落入诊断记录。网页可按请求 ID 查找最近记录。 网页在发送前按 JSON 正文的 UTF-8 字节数检查同一 1 MiB 上限，超限时就地提示。

原生接口错误使用 UTF-8 JSON。`400` 表示参数或类型错误，`401` 表示登录凭据或会话无效，`404` 表示 GUID 或条目不存在，`409` 表示标签命名冲突，`422` 表示凭据专用更新超出同步范围。体积超限使用 `413`，限流使用 `429`，服务故障使用 `5xx`。

## 联调与验收

临时 HTTPS 服务已完成登录、会话恢复和别名同步的模拟验证。首版功能开发与原生客户端联调并行推进。发布前用两端原生客户端完成下列实机验收。

1. 用版本固定的 HTTP 契约样例验证路径大小写、方法、头、空读请求、带数组的删除请求和空写响应。登录成功对象、`currentUser` 根对象、无条目分页和标签根数组分别验证 UTF-8 解码与类型。
2. 用 macOS 和 Windows 的原生 1.5.3 客户端连接测试 API。打开登录框，检查 `login-options` 无报错，账号密码登录后打开个人通讯录。验证服务观察到的是 `personal`、`peers` 和 `tags` 的逐条接口路径。不同版本的现用 Windows 客户端另行验收。
3. 在 Mac 添加设备，Windows 刷新查看。依次从不同端修改别名、备注和标签，再从网页核对。测试空通讯录、条目存在时重复添加、并发字段更新、跨页读取、删除后旧端编辑、标签改名和删除。另测添加请求处理一次但客户端未收到响应、另一端删除的场景，确认刷新可见删除结果，主动重新添加后恢复条目。确认服务重启后仍可读取已保存通讯录。
4. 在会话有效期内分别重启两端客户端，检查 `currentUser` 返回成功，并能直接加载通讯录。测试错误密码、在线单端退出和离线退出。在线退出后当前令牌返回 `401`，另一端可继续操作。离线退出后本机显示登录入口，服务端在原令牌到期或网页改密后拒绝该令牌。通过 `currentUser` 或个人通讯录入口验证 `401` 后的重新登录体验。
5. 从已同步的条目完成一次 Mac 到 Windows 的连接，验证原有设备授权流程。对启用了密码授权的测试设备，从「最近连接」或「收藏」清除本机已记住的密码，再连接时验证重新提示输入。另用已存在条目的空 `hash` 请求验证 `200` 空体，核对背景请求、设备凭据附带字段和管理端诊断结果。
6. 用测试用的 OSS 1.1.16 实例验证在线状态。将一个已注册 ID 和一个未注册 ID 保存到网页通讯录，确认分别显示在线和离线。停止测试实例后刷新，确认查询时间更新，两条状态均为未知，设备列表仍可加载。检查未登录请求被拒绝，并确认未保存的 ID 不会被查询。

联调只记录请求方法、路由、状态、客户端版本和必要的结构摘要。示例中的令牌、设备号与通讯录 GUID 使用测试值。
