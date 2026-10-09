# BiliPDJ Go · 轻量直播排队工具

全新独立实现（v0.10.0，技术预览）：**Go 标准库后端 + Vue 3 前端（随程序离线打包）**。参考旧版 BiliPDJ 协议规则但不直接引入 Python 运行时。

## 本版功能范围

- **Bilibili**：通过直播间解析 + WBI 签名发现带 token 的弹幕节点，优先加密 WSS `/sub`、TCP 备用；鉴权确认、心跳、zlib 解析 `DANMU_MSG` 和自动重连。匿名 uid=0；新增同会话 Cookie、buvid、queue_uuid/support_ack 兼容鉴权及服务端关闭码。真实直播验证仍必要。
- **抖音**：从 `https://live.douyin.com/<live_id>` 解析房间参数，通过 `webcast/im/fetch` HTTP 拉取 Protobuf `WebcastChatMessage`；断线重试。Cookie 可选，但某些房间需要。**这是轮询方案，不是 WS 直连**。
- **双平台同时开启**：两路接入独立 Goroutine，合并到统一事件流（SSE）与排队。
- **排队管理**：默认排队与官服/B服/超级/米服分类、取消/修改备注和主播 UID / 房管身份 / 最高管理员与普通管理员分级操作、黑名单管理、舰长插队及每日次数限制；按配置开关启停，Web 手动添加、移除、移动、编辑；10 个独立存档槽位持久化。
- **B站礼物资格**：解析直播服务器的 `SEND_GIFT` / `GUARD_BUY` 事件，支持礼物白名单、已知礼物电池阈值、资格授予、消费、去重与持久化（默认关闭）；礼物“插队”仅消耗真实 B站用户资格，不支持手工仿冒事件。
- **B站扫码登录**：内置离线二维码生成与官方轮询；v0.7.0 修复 `passport.biligame.com` 官方扫码回调，兼容旧版 URL Cookie 和新版 `crossDomain?ticket` → 302 `Set-Cookie`；仅允许固定 HTTPS 官方回调，不跟随外部跳转，通过 `nav` 核验后才保存 Cookie，支持主动退出登录。
- **旧版 `/ws` WebSocket 只读推送兼容**：推送 `PDJ_STATUS`、`QUEUE_UPDATE`、B站基础 `DANMU_MSG` 与抖音 `DOUYIN_DANMU`；**不允许 WebSocket 客户端执行管理指令**，不宣称所有旧第三方协议完全兼容。
- **Web UI + OBS**：Vue 3 离线控制台 `http://127.0.0.1:9816/`，OBS `http://127.0.0.1:9816/overlay.html`（建议 800×600）。
- **应用内自更新（v0.10.0）**：Web「软件更新」提供检查、官方/第三方加速下载、SHA-256 校验、确认安装并自动重启。临时更新助手在主程序退出后备份原 EXE/二进制并替换；新版启动异常时自动回滚。**仅经管理员主动确认触发安装**；Docker 必须重建或拉取镜像。
- **内置 MCP（AI 工具接口）**：HTTP `/mcp` 与 `--mcp-stdio` 支持 AI 查询 B站/抖音监听状态、统一队列及 10 个槽位；写操作需独立 `BILIPDJ_MCP_WRITE_TOKEN`，默认只读。兼容传统和 2026 MCP 协议，详见 [MCP 接入指南](docs/MCP.md)。
- **Docker / 原生运行**：无需 Python、Node 或外网前端 CDN。

未实现：Python/JS 插件市场、原有 Tk 前端、礼物目录动态价格获取、WebDAV 存档。旧配置支持安全导入，并增加功能开关和最多 10 个原版排队 CSV 槽位读取；仍仅部分功能字段能直接生效；其余原始资料会被完整备份。这仍是新项目技术预览，不宣称原项目全量功能完全兼容。

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

Windows 发行版会自动打开网页；新安装可按首次使用向导配置或选择跳过。其他系统请手动访问 Web 地址。选择平台、填写直播间号、开启监听并保存，双平台可同时启用。默认 `127.0.0.1:9816` 只允许本机访问；Cookie 将保存在当前生效的数据目录 `state.json`，请妥善保护该目录（文件权限 0600）。


## Go 独立数据目录（不改 Python 项目）

Python `bilipdj` 保持 [PR #314](https://github.com/ZzzHe2333/bilipdj/pull/314) 的目录约定；本项目的 Go 默认数据全部迁入独立的 **`bilipdj-go`** 用户目录，两个程序不再共用持久化写入位置。详情见 [独立存档迁移说明](docs/USER_DATA_COMPAT.md)。

- **Windows**：Go 配置、Cookie 和 Web 主题位于 `%APPDATA%\bilipdj-go/`，Go 队列与更新缓存分别位于 `%LOCALAPPDATA%\bilipdj-go/archives/`、`cache/`；备份在 `backups/`。
- **macOS**：`~/Library/Application Support/bilipdj-go/`，队列与备份在其 `archives/`、`backups/` 子目录。
- **Linux**：`$XDG_DATA_HOME/bilipdj-go/`（默认 `~/.local/share/bilipdj-go/`）；队列、备份同样在 `archives/`、`backups/`。
- **升级迁移**：仅识别旧 Go `state.json`（包含 Go 专属 `config` 对象）并 COPY-only 到新目录；旧的 `archives/go-queue-state.json` 与 `go-*.csv` 历史备份在需要时一并复制。原来的 `bilipdj` 文件一律保留，**不复制 Python 配置、CSV、插件或备份**。发现两份不同 Go 状态时不自动覆盖，默认使用新的 Go 目录。
- **跨项目导入**：「数据与存档」可以只读扫描 Python 的 `bilipdj/archives/queue_archive_slot_N.csv`，需要用户确认后才会**复制数据到 Go**；默认不存在自动双向同步。
- **便携/Docker**：已明确设置的 `-data` 或 `BILIPDJ_DATA_DIR` 仍按原指定位置工作，例如容器 `/data`。两套程序要完全隔离时，请确保没有人为把这两个显式参数指向同一个目录。
- Windows Tk 使用的 `style-win.json`、`appearance-win.json` 仅由 Python 管理。Go 只写自己目录下的 `style-web.json`、`appearance-web.json`。

## Windows v0.8.0：无黑框托盘模式与新手配置向导

- **Windows 发行包**使用 `-H=windowsgui` 构建，双击 EXE 不再弹出黑色终端窗口；服务后台运行，在任务栏右下角系统托盘（或折叠的隐藏图标区）显示 BiliPDJ Go 图标。
- 程序监听端口成功后，自动在系统默认浏览器打开 Web 管理界面。**双击托盘图标**再次打开界面；**右键托盘图标**可打开界面或**退出程序**。仅关闭浏览器标签页不会终止后台服务。
- **新安装首次打开网页**会出现可跳过的分步向导：B站、抖音启用和直播间地址、B站扫码登录、排队关键词、队列上限与每日次数限制。完成或跳过写入当前数据目录 `state.json`；以后不会自动打扰。升级已配置的旧版本默认跳过向导；需要时使用侧边栏底部的「新手配置引导」重新打开。
- **Go 独立数据目录**：Windows `%APPDATA%\bilipdj-go`（配置）和 `%LOCALAPPDATA%\bilipdj-go`（队列/备份/缓存），Linux `~/.local/share/bilipdj-go`，macOS `~/Library/Application Support/bilipdj-go`。已识别的旧 Go 状态 COPY-only 迁移，不动 Python `bilipdj`；显式 `-data` / `BILIPDJ_DATA_DIR` 不变。
- 从源码手动编译 Windows 无黑框版本：`go build -ldflags="-H=windowsgui" -o bilipdj-go.exe .`。普通 `go run .` 不会自动启用 GUI 子系统。Linux/macOS/Docker 保持控制台运行，不主动打开浏览器。
- 如果托盘图标没有直接显示，请点击任务栏右下角的「显示隐藏的图标」。目前使用 Windows 通用应用图标；将在后续版本添加独立品牌图标。此模式仍需在真实 Windows 桌面验收托盘菜单与浏览器打开行为。

## Docker

```sh
# 生成强随机 Token 并写入 .env，例如：BILIPDJ_ADMIN_TOKEN=<LONG_RANDOM_TOKEN>
docker compose up -d --build
```

Docker 映射端口到**宿主机** `127.0.0.1:9816`，容器内监听 `0.0.0.0`。首次 Web 保存配置时会提示输入 `.env` 中的管理员 Token；浏览器使用 `sessionStorage` 暂存 Token。不要把此服务的管理端口直接暴露公网。

## 版本更新 / 自动打包

GitHub Actions `.github/workflows/release.yml` 在主分支推送或推送 `v*` 标签时编译 Windows / Linux / macOS（AMD64 + ARM64 可用组合），上传 `bilipdj-go-<goos>-<goarch>.zip` 与同名 `.sha256`。软件支持从 `releases/latest` 检查更新。v0.10.0 起 Release 还提供 `update-manifest.json`（六平台 ZIP 的 SHA-256 和大小），当 GitHub API 不可达时，通过可选 GH-Proxy 第三方加速代理获取清单并下载附件。详见 [自更新安全与使用说明](docs/UPDATER.md)。

**保护数据：** Go 版使用当前生效用户数据目录下的 `state.json`。在「平台配置 → 导入旧版配置」选择旧版 `config.yaml` 或含有多个旧版配置文件的 ZIP，可先预览，再确认导入。旧版原件永不被修改；导入时会在 `data/migration-backup/` 保存当前新版状态与旧版原始文件（0600 权限），避免数据不可逆丢失。保存过的 Python/JS 插件不会被执行。

#### 可直接导入的旧版资料

| 原始文件 | 实际迁移结果 |
| --- | --- |
| `core/config.yaml`、`config.yaml` | B站 `roomid`/Cookie、抖音 `live_id`/Cookie 和 enabled、排队上限 `paidui_list_length_max`、管理员与最高管理员名单、舰长名单、每日排队上限和重置时间/已用次数（有限兼容）、语言、当前队列槽位、礼物白名单/电池阈值/插入位置/单礼物资格数；旧版已消费礼物资格不自动搬迁，未实现的配置保留原样备份 |
| `core/quanxian.yaml`、`core/kaiguan.yaml` | 识别管理员、最高管理员、舰长和黑名单，迁移主要开关（含房管管理及舰长插队） |
| `style.json` / `style-web.json` | 完整 JSON 保存在 Go 状态；OBS 端增加字体、行高、字距、间距、描边、序号、自动滚动等映射（仍非原版所有 CSS 动画） |
| `appearance.json` / `appearance-web.json` | 完整 JSON 保存，供后续 Aurora 界面外观继续迁移 |
| `blacklist.csv` | 导入黑名单，并在新排队处理时拦截 |
| `core/cd/*.csv` | 导入识别到的 1–10 号槽位 CSV（含第 5 列来源平台），并支持切换和重启恢复 |

如果旧版设置散落在多个文件中，请将这些文件压缩成 ZIP 再导入；仅导入一个 `config.yaml` 不会自动读取同机其他旧版文件。导入前建议停止旧版后端，避免同时使用同一直播间和重复写入资料。

## AI / MCP 接入

正常运行程序后，即可由支持 MCP 的 AI 客户端连接 `http://127.0.0.1:9816/mcp`，或在本地 AI 客户端里将已安装的 `bilipdj-go` 二进制作为 `--mcp-stdio` 命令启动（仅连接现有服务，不创建第二份存档写入进程）。默认只允许本机读取，写操作必须设置 `BILIPDJ_MCP_WRITE_TOKEN` 并使用 Bearer 授权；Docker/远程读取可设置 `BILIPDJ_MCP_READ_TOKEN`。工具有状态、队列、槽位、近期弹幕，以及经授权的增加/修改/移动/删除/切换/清空。不要将 MCP 管理端口直接暴露在公网。详细接入配置：[docs/MCP.md](docs/MCP.md)。

## API 最小契约

- `POST /api/config`：一次性保存 `{"bilibili":{"enabled":true,"room":"6","cookie":""},"douyin":{"enabled":false,"room":"","cookie":""},"auto_queue":true,"command":"排队"}`；Cookie 留空保留已存值。
- `POST /api/queue`：`{"action":"add","name":"Alice"}` / `{"action":"remove","key":"bilibili:1"}` / `{"action":"clear"}`。
- `GET /api/messages`：最近 150 条消息。
- `GET /api/gifts/state`：管理员授权查看礼物资格计数和最后事件（不返回 UID 名单）。
- `GET /ws`：旧版只读 WebSocket 推送桥接，不执行客户端发来的管理指令。
- `POST /api/bili/qr/start` / `POST /api/bili/qr/poll`：管理员授权启动扫码及查询状态；扫码登录后验证账号再保存 Cookie。`POST /api/bili/logout` 清除 Cookie。
- `GET /api/onboarding` / `POST /api/onboarding`：首次使用引导状态和完成/跳过持久化；只允许本机或管理员 Token，POST 仅接受 `{"completed":true}`。
- `GET /api/events`：`text/event-stream`；事件类型 `danmu`、`queue`、`status`、`gift`。
- `GET /api/update?source=auto|official|accelerated`：GitHub Release 与可更新状态（需要本机授权）。
- `POST /api/update/download`：`{"source":"auto"}`，下载并 SHA-256 校验到更新暂存目录；
- `POST /api/update/install`：管理员确认后安装已验证的暂存包，等待旧进程退出、自动备份并重启；Docker 返回不支持。

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

`go test ./...` 覆盖旧版 YAML/ZIP/CSV 解析、导入接口、权限、备份权限和持久化。**用户已反馈手动 Cookie 下 B站弹幕流可正常工作；v0.7.0 的二维码回调修复仍需用户实测，抖音与长时间运行的验收尚未完成。**

新增默认值说明：新安装时保留旧版预设 B站直播间 `3049445`，但默认不主动建立连接；旧版的 `style.json`、`appearance.json` 默认设计值已并入 Go 版，并可导入后在 Vue/OBS 的受支持外观属性中生效。

### B站 EOF 修复与后续验收说明

Go 版已优先使用 WSS 端口（根据服务器 `host_list.wss_port`），并从 `getDanmuInfo` 获取 token、以 op=8 确认真正鉴权成功。旧版 Go 在 TCP 套接字写完鉴权包时就误报“已连接”，随后经常返回 `EOF`。

如果更新后仍提示连接失败，请检查：1. B站直播间号正确且直播间未失效；2. 如提示 token/WBI/鉴权拒绝，尝试在平台配置中提供你本人账号的有效 B站 Cookie；3. 当前网络可访问 B站 `*.chat.bilibili.com` 的 WSS 端口。不要将 Cookie 发给开发者，错误日志也不要包含凭据。

**验证边界：** 已运行离线单元测试、并发竞态测试、静态检查及跨平台编译；由于测试环境无法连接 B站公网直播服务器，还不能宣称真实直播间收弹幕已经完全修复。


## 迁移功能差异审核

逐项审核见 [`docs/FEATURE_PARITY.md`](docs/FEATURE_PARITY.md)。特别注意：B站 :2245 的 EOF 可能来自鉴权关闭；此版改进了鉴权字段和诊断，不保证在未经线上验收时一定连接成功。


### v0.6.0 排队权限与每日配额迁移

- `daily_queue_limit` 默认 **0（不限制）**；`daily_queue_reset_time` 默认 **04:00**，使用**运行机器的本地时区**。达到当日上限后，取消排队不会返还配额；达到重置时间才清零。按 **平台 + 用户 UID** 分开计数，随 `data/state.json` 保存，重启不会重置。
- B站 `room_init` 返回的主播 UID 会与弹幕用户 UID 对照；B站房管身份来自弹幕 `info[2][2]`，舰长等级来自 `info[3][10]`。**这些权限需要平台已成功接收真实弹幕后才能自动生效**。
- 普通管理员可做基础队列管理（添加/删除/完成、暂停/恢复），最高管理员或 UID 验证的主播还可以添加/取消管理员；房管管理员权限与舰长插队均需要单独开启。管理员操作不受每日自助排队次数限制。
- 原版 `quanxian` 管理员/最高管理员/舰长名单和 `kaiguan` 房管/舰长开关可导入并生效。旧版计数 `数字UID=次数` 只能保守归到 B站用户；**跨平台身份无法无损推断**，原文件保存在迁移备份里。
- **兼容限制：** 按昵称填写的管理员/最高管理员属于旧版兼容机制；昵称不是强身份认证凭据，涉及高权限时建议优先依赖 B站认证 UID/房管身份，不要公开暴露后端管理端口。新安装版不会默认赋予任意昵称最高管理员权限；旧版若导入了特定最高管理员昵称则保留。
- 尚未迁移：礼物价值积分、充值/舰长购买事件、所有插件系统、原版全部 OBS 样式、旧 `/ws`。尤其 B站鉴权 EOF 仍需要真实直播间验证；本次迁移不代表解决连接问题。


### v0.6.0 礼物资格和 OBS 样式迁移

- 礼物资格默认为关闭，仅使用来自 B站已连接弹幕服务器的 `SEND_GIFT` 消息；`GUARD_BUY` 只作为观测事件记录，**不会自动生成礼物资格**。
- 礼物名称名单或内置电池目录达到门槛时才发放资格；目前为了避免免费礼物误生成付费资格，要求消息的 `coin_type=gold`。该行为较旧版更保守，请按真实礼物测试后启用。
- 支持一次礼物多资格、可否重复发放、礼物专用排队及消费位置。`插队` 消费后写入 `data/state.json`；按平台与 UID 区分，不依赖昵称确认礼物所有权。
- 识别 `tid` / `rnd` 做最近 512 个礼物消息的去重（跨重启持久）；对没有事件 ID 的报文不能保证严格幂等。
- 旧版 `myjs.gift_queue_*` 基础规则可导入，但旧版已使用 UID、资格余额**不会自动迁移**。原始备份继续保留，避免误给或重复发放已消耗资格。
- OBS 新支持字体、大小、行距、字距、颜色、背景、描边、序号、滚动与 Vue 队列展示；部分 CSS 存档、自定义动画及 WebSocket 协议仍不兼容。
- **B站实时鉴权 EOF 尚未通过公开直播间实测恢复**：该版本的礼物资格只有在 B站弹幕连接正常且能收到事件时才能工作。

### B站多个节点均在鉴权后 EOF 的排查（v0.6.0）

如果 WSS `2245` 和 TCP `2243` 在多个节点都出现 `等待鉴权结果失败: EOF`，优先检查账户会话与鉴权参数，**不要猜测或随意更改弹幕端口**。

- v0.4.x 的 Go 客户端在获取已登录会话 token 后仍固定使用匿名 `uid=0`，v0.6.0 已修正：优先使用相同 Cookie 下 B站 `nav` 返回的已登录 UID；`nav` 明确未登录时不沿用可能过期的 `DedeUserID`。
- 在本机管理页面配置自己已登录 B站账户的 Cookie（通常含 `SESSDATA`、`DedeUserID`、`buvid3`）；不要把 Cookie、token 或完整 HTTP 报文发布到 Issue 或聊天。
- 无有效 Cookie 时仍会尝试匿名模式，但部分房间/网络条件下可能被平台关闭；若 Go 版继续全部 EOF，应对照旧版 Python 是否在**同一电脑、同一直播间、同一登录状态**下可连接。
- 此更新已通过离线鉴权链路与数据处理测试，但尚未获得公网真实直播间的鉴权成功证据；不能保证解决所有 EOF。


### v0.6.0 迁移：扫码登录及旧版 WebSocket

- 在「平台配置 → B站扫码登录」生成二维码，使用 B站手机 App 扫码并确认。二维码在浏览器本地渲染（`web/qrcode.js`），不会上传到第三方二维码网站。后端只保留二维码 key 在内存中最多四分钟；扫码回调的 Cookie 只有通过 B站 `nav.isLogin` 验证后才写入本地 `data/state.json`。后台不会把 Cookie、二维码 key 返回给其他客户端。
- 登录成功后如果 B站已开启监听，只重启 B站接入，不主动重启抖音。清除 Cookie 也提供独立按钮。若线上 B站扫码接口有变化，登录接口会明确报错；该接口尚未对真实登录做验收。
- `/ws` 基于原版只读事件格式，支持队列快照、后续队列变化、B站与抖音聊天弹幕、平台连接状态；保留 SSE `/api/events`。此桥接不支持原版任意客户端指令、插件双向消息或完整原始业务事件。WebSocket 服务端控制帧支持 Ping/Pong，拒绝跨域 Origin 和未掩码客户端帧。
- `VERSION` 现在作为 GitHub Actions 的发版单一版本来源，CI 会核对 `main.go` 版本是否一致，且拒绝复用已存在的发布标签，防止发版资源错误覆盖旧版标签。

**兼容性边界：** 本轮已通过模拟 B站扫码回调、会话校验、WebSocket 握手/推送等离线回归；仍然需要实际 B站客户端扫码及原有第三方 WebSocket 使用方测试，不能保证旧版完整协议和 B站实时鉴权已修复。

### v0.7.0 扫码回调修复

- 修复手机扫码确认后 Go 后端拒绝 B站官方 `passport.biligame.com` 跨域回调的问题。
- 兼容两种成功响应：`crossDomain?...&SESSDATA=...` 直接提供 Cookie，以及 `crossDomain?ticket=...` 需由后端从 302/HTTP `Set-Cookie` 提取 Cookie 的新响应。
- 严格限定 HTTPS 官方回调域名和路径，禁止自动跨域重定向、不会向浏览器暴露 ticket 或 Cookie，`nav.isLogin=true` 后才会保存并重连 B站。
- 保留当前手动配置的 Cookie；扫码失败或登录态验证失败不会覆盖现有登录信息。
- **B站扫码功能的实际手机端验收尚未完成**；如仍异常，请提供不含凭据的错误提示。


## v0.7.0 Windows 风格 Web 控制台

- **运行日志是首页**：两平台状态卡片、系统/弹幕/队列事件实时打印（SSE）、级别筛选、来源筛选、关键词搜索、自动滚动、复制、TXT 导出与清空浏览器当前显示。后端 `/api/logs` 提供最近最多 500 条脱敏日志，支持刷新页面恢复；此版本**不提供历史日志持久化/长时归档**。
- **排队管理为独立一级菜单**：表格式显示序号、用户名、内容/类别、平台、用户 ID 和入队时间；支持切换 10 个存档、搜索、追加、新成员插入到选中项下方、上移、下移、编辑备注、删除、完成首位和清空队列。所有操作复用原来的队列持久化和事件通知，双平台保持同一存档。
- **Windows 功能分区**：左侧「运行日志 / 排队管理 / 平台与规则 / 权限管理 / OBS 展示设置 / 软件更新」。权限名单、房管权限和 OBS 样式从平台长表单拆成独立页面。OBS 展示仍是 Web 浏览器源，而不是 Windows 原生透明悬浮窗。
- 新增 `GET /api/logs`（仅本机或管理员令牌访问），已有 `/api/events` 会发送 `type=log` 实时事件。日志不携带 Cookie、token、扫码 ticket 等凭据；需要在不可信网络部署时配置反向代理访问控制。
- 未改动 B站/抖音协议、扫码登录验证或礼物处理逻辑。
