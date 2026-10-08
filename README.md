# BiliPDJ Go · 轻量直播排队工具

全新独立实现（v0.3.0，技术预览）：**Go 标准库后端 + Vue 3 前端（随程序离线打包）**。参考旧版 BiliPDJ 协议规则但不直接引入 Python 运行时。

## 本版功能范围

- **Bilibili**：通过直播间解析 + WBI 签名发现带 token 的弹幕节点，优先加密 WSS `/sub`、TCP 备用；鉴权确认、心跳、zlib 解析 `DANMU_MSG` 和自动重连。匿名 uid=0；新增同会话 Cookie、buvid、queue_uuid/support_ack 兼容鉴权及服务端关闭码。真实直播验证仍必要。
- **抖音**：从 `https://live.douyin.com/<live_id>` 解析房间参数，通过 `webcast/im/fetch` HTTP 拉取 Protobuf `WebcastChatMessage`；断线重试。Cookie 可选，但某些房间需要。**这是轮询方案，不是 WS 直连**。
- **双平台同时开启**：两路接入独立 Goroutine，合并到统一事件流（SSE）与排队。
- **排队管理**：默认排队与官服/B服/超级/米服分类、取消/修改备注和基本管理员新增/删除/完成；按配置开关启停，Web 手动添加、移除、移动、编辑；10 个独立存档槽位持久化。
- **Web UI + OBS**：Vue 3 离线控制台 `http://127.0.0.1:9816/`，OBS `http://127.0.0.1:9816/overlay.html`（建议 800×600）。
- **更新检查与安全暂存**：检查本仓库 GitHub Release，下载 **匹配系统平台**的 ZIP 并校验对应 `.sha256`，暂存于 `data/updates`。**不会悄悄修改运行中的程序**；用户自行替换并重启。Docker 则更新镜像。
- **Docker / 原生运行**：无需 Python、Node 或外网前端 CDN。

未实现：Python/JS 插件市场、礼物特权、原有 Tk 前端、完整管理员权限模型、WebDAV 存档、真正自动替换重启。旧配置支持安全导入，并增加功能开关和最多 10 个原版排队 CSV 槽位读取；仍仅部分功能字段能直接生效；其余原始资料会被完整备份。这仍是新项目技术预览，不宣称原项目全量功能完全兼容。

> **重要：** 两平台线上协议可能随时调整。本项目带有报文解析、队列和 API 单测，但离线构建环境下**未进行真实直播间连通性验证**；尤其抖音可能因 Cookie/风控/页面结构变化而无法接入。出现断线时请先看平台状态及日志。

## 本地运行

源码（Go 1.22+）：

```sh
go run .
# 或者：go build -o bilipdj-go . && ./bilipdj-go
```

下载对应发行包后运行 `bilipdj-go` / `bilipdj-go.exe`，访问：

- 控制台：`http://127.0.0.1:9816/`
- OBS：`http://127.0.0.1:9816/overlay.html`
- 服务：`GET /health`、`GET /api/status`、`GET /api/queue`、`GET /api/events`（SSE）

选择平台、填写直播间号、开启监听并保存，双平台可同时启用。默认 `127.0.0.1:9816` 只允许本机访问；Cookie 将保存在本机 `data/state.json`，请妥善保护该目录（文件权限 0600）。

## Docker

```sh
# 生成强随机 Token 并写入 .env，例如：BILIPDJ_ADMIN_TOKEN=<LONG_RANDOM_TOKEN>
docker compose up -d --build
```

Docker 映射端口到**宿主机** `127.0.0.1:9816`，容器内监听 `0.0.0.0`。首次 Web 保存配置时会提示输入 `.env` 中的管理员 Token；浏览器使用 `sessionStorage` 暂存 Token。不要把此服务的管理端口直接暴露公网。

## 版本更新 / 自动打包

GitHub Actions `.github/workflows/release.yml` 在推送 `v*` 标签时编译 Windows / Linux / macOS（AMD64 + ARM64 可用组合），上传 `bilipdj-go-<goos>-<goarch>.zip` 与同名 `.sha256`。软件从 `releases/latest` 检查与暂存更新；发布版本前更新检查可能返回 HTTP 404（正常）。

**保护数据：** Go 版使用 `data/state.json`。在「平台配置 → 导入旧版配置」选择旧版 `config.yaml` 或含有多个旧版配置文件的 ZIP，可先预览，再确认导入。旧版原件永不被修改；导入时会在 `data/migration-backup/` 保存当前新版状态与旧版原始文件（0600 权限），避免数据不可逆丢失。保存过的 Python/JS 插件不会被执行。

#### 可直接导入的旧版资料

| 原始文件 | 实际迁移结果 |
| --- | --- |
| `core/config.yaml`、`config.yaml` | B站 `roomid`/Cookie、抖音 `live_id`/Cookie 和 enabled、排队上限 `paidui_list_length_max`、管理员列表、语言、当前队列槽位；未实现的配置保留原样备份 |
| `core/quanxian.yaml`、`core/kaiguan.yaml` | 可识别部分管理员设置；未兼容的原有开关保留原文件 |
| `style.json` | 完整 JSON 保存到新 Go 状态，并在 OBS 端应用部分基本样式 |
| `appearance.json` | 完整 JSON 保存，供后续 Aurora 界面外观继续迁移 |
| `blacklist.csv` | 导入黑名单，并在新排队处理时拦截 |
| `core/cd/*.csv` | 导入识别到的当前槽位队列 CSV；其余槽位保存原始档案 |

如果旧版设置散落在多个文件中，请将这些文件压缩成 ZIP 再导入；仅导入一个 `config.yaml` 不会自动读取同机其他旧版文件。导入前建议停止旧版后端，避免同时使用同一直播间和重复写入资料。

## API 最小契约

- `POST /api/config`：一次性保存 `{"bilibili":{"enabled":true,"room":"6","cookie":""},"douyin":{"enabled":false,"room":"","cookie":""},"auto_queue":true,"command":"排队"}`；Cookie 留空保留已存值。
- `POST /api/queue`：`{"action":"add","name":"Alice"}` / `{"action":"remove","key":"bilibili:1"}` / `{"action":"clear"}`。
- `GET /api/messages`：最近 150 条消息。
- `GET /api/events`：`text/event-stream`；事件类型 `danmu`、`queue`、`status`。
- `GET /api/update`：GitHub Release 与可更新状态。
- `POST /api/update/download`：仅下载并 SHA-256 校验到更新暂存目录，不直接安装。

管理写操作限本机 loopback+localhost Host 与同源 Origin；Docker 远程地址管理请求必须携带 `X-Admin-Token`，服务使用恒定时间比较验证。日志和读取 API 不返回 Cookie。

## 开发 / 测试

```sh
go test ./...
go test -race ./...
go vet ./...
```

协议测试使用构造的 B站 WSS/TCP、鉴权、WebSocket 分片、WBI、HTTP 发现及抖音 Protobuf 测试报文，不需要连接真实直播服务。请在两个真实直播间进一步验收双源持续在线、收弹幕、重连、重复加入与停播恢复。

本项目代码依据旧版 BiliPDJ（GPL-3.0）兼容实现，发布遵守 GPL-3.0；Vue.js 随包单独按 MIT 授权，见 `NOTICE.md`。

## 兼容 API

- `POST /api/legacy/preview` 和 `POST /api/legacy/import`：表单 `multipart/form-data`，字段名 `file`；需要本机授权，ZIP 解压大小上限 12 MiB。
- `GET /api/style`、`POST /api/style`、`GET /api/appearance`、`POST /api/appearance`：保留主题/样式 JSON。
- `/control` 跳转新版控制台；旧 OBS 地址 `/index` 跳转 `/overlay.html`。
- `GET /api/runtime-status`、`GET /api/queue/state`、`GET /api/platforms/active` 提供基础只读兼容字段，不保证旧版全量字段一致。
- 队列手工操作现支持 `add`、`remove`、`clear`、`edit`（修改备注）和 `move`（从零开始计数的 `index`）。

### 迁移验收说明

`go test ./...` 覆盖旧版 YAML/ZIP/CSV 解析、导入接口、权限、备份权限和持久化。**尚未完成真实 B站/抖音直播间持续收弹幕验收，用户应当把 v0.2.2 视为技术预览。**

新增默认值说明：新安装时保留旧版预设 B站直播间 `3049445`，但默认不主动建立连接；旧版的 `style.json`、`appearance.json` 默认设计值已并入 Go 版，并可导入后在 Vue/OBS 的受支持外观属性中生效。

### v0.2.2 B站 EOF 修复说明

本版优先使用 WSS 端口（根据服务器 `host_list.wss_port`），并从 `getDanmuInfo` 获取 token、以 op=8 确认真正鉴权成功。旧版 Go 在 TCP 套接字写完鉴权包时就误报“已连接”，随后经常返回 `EOF`。

如果更新后仍提示连接失败，请检查：1. B站直播间号正确且直播间未失效；2. 如提示 token/WBI/鉴权拒绝，尝试在平台配置中提供你本人账号的有效 B站 Cookie；3. 当前网络可访问 B站 `*.chat.bilibili.com` 的 WSS 端口。不要将 Cookie 发给开发者，错误日志也不要包含凭据。

**验证边界：** 已运行离线单元测试、并发竞态测试、静态检查及跨平台编译；由于测试环境无法连接 B站公网直播服务器，还不能宣称真实直播间收弹幕已经完全修复。


## 迁移功能差异审核

逐项审核见 [`docs/FEATURE_PARITY.md`](docs/FEATURE_PARITY.md)。特别注意：B站 :2245 的 EOF 可能来自鉴权关闭；此版改进了鉴权字段和诊断，不保证在未经线上验收时一定连接成功。
