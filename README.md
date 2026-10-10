<div align="center">

<img src="./web/bilipdj-go.svg" alt="BiliPDJ-Go 项目图标" width="104" height="104">

# 🎬 BiliPDJ-Go · 轻量直播排队姬

面向 **Bilibili / 抖音** 直播间的轻量级弹幕排队、权限控制、队列存档与 OBS 展示工具

**Go 标准库后端 + Vue 3 离线 Web 控制台** · Windows / Linux / macOS / Docker

<p>
  <a href="https://github.com/ZzzHe2333/bilipdj-go/releases"><img alt="Release" src="https://img.shields.io/github/v/release/ZzzHe2333/bilipdj-go?style=flat-square&color=6366f1"></a>
  <a href="https://github.com/ZzzHe2333/bilipdj-go/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/github/license/ZzzHe2333/bilipdj-go?style=flat-square"></a>
  <a href="https://github.com/ZzzHe2333/bilipdj-go/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/ZzzHe2333/bilipdj-go?style=flat-square&color=yellow"></a>
  <a href="https://github.com/ZzzHe2333/bilipdj-go/issues"><img alt="Issues" src="https://img.shields.io/github/issues/ZzzHe2333/bilipdj-go?style=flat-square"></a>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat-square">
  <img alt="Vue" src="https://img.shields.io/badge/Vue-3-42b883?style=flat-square">
</p>

<p>
  <a href="https://github.com/ZzzHe2333/bilipdj-go/releases/latest">下载发行版</a> ·
  <a href="#-快速开始">快速开始</a> ·
  <a href="./docs/FEATURE_PARITY.md">与 Python 版差异</a> ·
  <a href="./docs/UPDATER.md">更新说明</a> ·
  <a href="https://github.com/ZzzHe2333/bilipdj-go/issues">问题反馈</a>
</p>

</div>

---

## ✨ 这是什么

**BiliPDJ-Go** 是 [BiliPDJ · 弹幕排队姬](https://github.com/ZzzHe2333/bilipdj) 的**独立 Go 后端实现**，不是音乐播放软件，也不是简单把 Python 打包为 EXE。它将直播间弹幕监听、自动排队、管理员操作、OBS 展示与多平台共用的队列状态放在同一套轻量服务里，通过内置 Vue 控制台管理。

与原版的主要区别是：**不依赖 Python 运行环境、可单文件运行、适合 Docker 和跨平台部署**；Windows 版可隐藏控制台并常驻托盘，启动时自动打开浏览器控制台。

> **功能仍处于技术预览阶段。** 当前实际接入 **Bilibili 和抖音**，两路可同时监听且共用当前队列存档；虎牙、快手、斗鱼等仅有部分人工来源标签或兼容配置，**不代表可以监听这些平台**。完整功能对照见 [迁移差异审核](./docs/FEATURE_PARITY.md)。

## 🚀 快速开始

普通用户无需安装 Go、Python 或 Node。到 **[最新发行版](https://github.com/ZzzHe2333/bilipdj-go/releases/latest)** 选择与设备系统及架构匹配的 ZIP，下载并解压：

| 运行环境 | 发行包命名示例 | 使用方式 |
| --- | --- | --- |
| 🖥️ Windows x64 | `bilipdj-go-windows-amd64.zip` | 解压后运行 `bilipdj-go.exe`，自动打开 Web 页面，程序驻留托盘 |
| 🖥️ Windows ARM64 | `bilipdj-go-windows-arm64.zip` | 运行相应 EXE，操作方式相同 |
| 🐧 Linux x64 / ARM64 | `bilipdj-go-linux-<arch>.zip` | 解压后赋予可执行权限，运行 `./bilipdj-go` |
| 🍎 macOS Intel / Apple Silicon | `bilipdj-go-darwin-<arch>.zip` | 解压后在终端运行 `./bilipdj-go` |
| 🐳 Docker | 源码构建镜像 | 使用下文 `docker compose` 命令部署 |

**Windows 上手三步：** 解压并启动 `bilipdj-go.exe` → 首次使用引导中设置 B站/抖音直播间与排队规则（可跳过） → 在 OBS 添加“浏览器源”，填入 `http://127.0.0.1:9816/index`（可从 800 × 600 开始调整）。

打开浏览器：

- **主控制台**：`http://127.0.0.1:9816/`，首页是实时运行日志。
- **独立排队管理**：`http://127.0.0.1:9816/queue.html`，可在手机/平板浏览器操作，但远程访问须自行配置安全网络及授权。
- **OBS 透明队列**：`http://127.0.0.1:9816/index`；`/overlay.html` 继续兼容。

> ZIP 名称代表打包目标，不代表任何指定版本已发布。当前源码版本为 **0.10.3**；下载与更新请以 [Releases](https://github.com/ZzzHe2333/bilipdj-go/releases) 的实际资产为准。Windows 包另带 `bilipdj-go-mcp.exe` 供 stdio MCP 客户端使用。

## 🐳 Docker：使用同一套 Vue Web 控制台

项目自带 `Dockerfile` 和 `docker-compose.yml`，无需额外安装 Python 或搭建静态网站。

~~~bash
git clone https://github.com/ZzzHe2333/bilipdj-go.git
cd bilipdj-go
# Linux / macOS：生成用于管理写操作的强随机 Token
printf 'BILIPDJ_ADMIN_TOKEN=%s\n' "$(openssl rand -hex 32)" > .env
docker compose up -d --build
docker compose ps
~~~

默认只将端口映射到宿主机 **`127.0.0.1:9816`**，Web 控制台和 OBS 使用同一地址。容器中的配置与存档位于 `/data`，由 Docker 命名卷持久化；重建容器不会自动删除该卷。需要远程管理时，请先设置安全鉴权并使用可信局域网、SSH 隧道或 VPN，不要直接开放公网。

**Docker 更新请重建或更新镜像**，不要在容器内使用应用内二进制自更新器。详见 [用户数据说明](./docs/USER_DATA_COMPAT.md) 与 [更新器说明](./docs/UPDATER.md)。

## 🧩 功能一览

| 功能 | BiliPDJ-Go 当前能力 |
| --- | --- |
| 🔀 多平台弹幕 | Bilibili 使用 WSS 优先、TCP 备用；抖音为 HTTP 轮询 + Protobuf；两路独立连接，合并到同一个排队队列 |
| 🙋 排队与权限 | 关键词自动排队、人工追加/移动/完成/删除、黑名单、主播/管理员分级、房管权限与舰长插队（按开关配置） |
| 🗂️ 存档 | 10 个排队槽位，同一槽位内 B站和抖音共用队列；Go 数据与原 Python `bilipdj` 目录隔离 |
| 📋 运行日志 | 首页显示平台连接、排队与系统事件；支持筛选、搜索、复制和导出 TXT |
| 🪟 Web 与 Windows | Vue 3 离线控制台、独立排队管理页、Windows 无黑框启动与品牌托盘图标、首次使用向导 |
| 🎬 OBS 展示 | 旧版 `/index` 地址兼容；默认**只显示用户名**，可选序号、显示人数、自动滚动速度、字号与背景样式 |
| 📈 性能监测 | 默认每 2 秒采样，可选 0（关闭）或 1–1200 秒；显示受平台支持的 CPU、内存、进程 I/O、磁盘与网络口径；**不虚报 GPU/NPU 数据** |
| 🎁 B站礼物资格 | 可配置礼物白名单、资格去重及插队消费；默认关闭，依赖真实有效的直播事件 |
| 🔄 应用内更新 | GitHub 官方 / 第三方公益加速线路、下载进度、SHA-256 校验、确认后更新与异常回滚；可查看最近 10 个正式版本并手动降级 |
| 🤖 AI / MCP | `/mcp` 和 `--mcp-stdio`；默认只读，写操作需要独立授权 Token |

**尚未提供：** Python 版 Tk UI、Python/JS 插件市场、WebDAV 存档、动态礼物价格目录与完整的原版双向 WebSocket 控制协议。`/ws` 仅提供兼容性**只读事件推送**，不能用它执行管理指令。

## 🏗️ 项目结构

<details>
<summary>点击展开 Go 项目目录</summary>

~~~text
bilipdj-go/
├─ main.go                    # 服务启动、HTTP 路由及 Web 静态资源内嵌
├─ desktop_windows.go         # Windows 无黑框启动、系统托盘
├─ internal/
│  ├─ core/                   # 队列、权限、管理 API、事件
│  ├─ live/                   # B站和抖音直播协议
│  ├─ storage/                # 数据目录与存档迁移
│  ├─ update/                 # 应用内更新与校验
│  └─ mcp/                    # AI / MCP 服务
├─ web/                       # Vue 3 控制台、独立排队页、OBS
├─ assets/                    # SVG、Windows ICO 图标资源
├─ docs/                      # 功能差异、性能、更新、MCP 与迁移文档
├─ third_party/               # 第三方资源及版权说明
├─ Dockerfile
├─ docker-compose.yml
├─ VERSION                    # 发行构建使用的版本来源
└─ .github/workflows/release.yml
~~~

Go 通过 `//go:embed web/*` 内嵌前端资源；运行时不需要 Node、外部 CDN 或独立的 Vue 构建服务器。Windows 程序图标与托盘图标使用不同的 ICO 资源，Vue 标识来自 `web/bilipdj-go.svg`。

</details>

## 🎨 Web / OBS 设置与数据位置

Web 控制台管理同一个 Go 后端，`/queue.html` 与 OBS 看到当前已选队列槽位的同一份状态。人工添加成员可以设置**展示用来源标签**，但不能伪造真实平台账号身份或启动不存在的平台监听。

OBS 默认是**纯透明且只显示队列用户名**，不显示“实时排队 N 人”、“槽位”和“无来源”。如有需要，可主动打开标题与序号，设置可见人数（1–100）、自动滚动及速度（1–300 像素/秒）；设置页可实时预览，点击保存后才应用到正式 OBS 浏览器源。

| 环境 | Go 配置、Web 样式 | Go 队列、备份及缓存 |
| --- | --- | --- |
| Windows | `%APPDATA%\bilipdj-go` | `%LOCALAPPDATA%\bilipdj-go` 下的 `archives/`、`backups/`、`cache/` |
| macOS | `~/Library/Application Support/bilipdj-go` | 同一目录下的子目录 |
| Linux | `$XDG_DATA_HOME/bilipdj-go`（默认 `~/.local/share/bilipdj-go`） | 同一目录下的子目录 |
| Docker / 显式指定 | `-data` 或 `BILIPDJ_DATA_DIR`（镜像默认为 `/data`） | 随显式数据路径保存 |

**与 Python 版相互隔离。** Go 版不自动写入或删除 Python 的 `bilipdj` 数据目录；跨项目导入必须用户明确确认。旧 Go 数据迁移采取保留原件的复制方式。详见 [独立存档与迁移](./docs/USER_DATA_COMPAT.md)。

## 📡 默认地址

| 地址 | 用途 |
| --- | --- |
| `http://127.0.0.1:9816/` | Vue 控制台、运行日志首页 |
| `http://127.0.0.1:9816/queue.html` | 独立排队管理 |
| `http://127.0.0.1:9816/index` | OBS 透明队列（历史兼容地址） |
| `http://127.0.0.1:9816/overlay.html` | OBS 同功能新地址 |
| `GET /health` | 健康检查与版本 |
| `GET /api/queue/state` | 当前槽位队列状态 |
| `GET /api/events` | SSE 实时事件 |
| `GET /ws` | 兼容只读 WebSocket 推送 |
| `POST /api/queue` | 鉴权后队列写操作 |
| `/mcp` | MCP 工具服务 |

`/control` 会跳转到 Go 主控制台。服务默认只监听本机 `127.0.0.1:9816`；监听局域网地址并不代表自动获得公网级安全防护。

## 🧭 架构

~~~mermaid
flowchart LR
    B[Bilibili] --> G[Go Live Adapters]
    D[Douyin] --> G
    G --> Q[Go Queue / Permissions / Archives]
    Q --> API[HTTP API + SSE + Read-only WS + MCP]
    API --> WEB[Vue Console / Queue UI]
    API --> OBS[OBS Browser Source]
    API --> WIN[Windows Tray / Browser]
~~~

Go 后端是**唯一业务状态源**；Vue 控制台、独立管理页面和 OBS 复用同一份数据，不另建隐藏队列。MCP 使用显式授权控制写操作。

## 🛠️ 开发与构建

源码需要 **Go 1.22+**：

~~~bash
git clone https://github.com/ZzzHe2333/bilipdj-go.git
cd bilipdj-go
go run .
# 或编译成独立可执行文件
go build -o bilipdj-go .
~~~

Windows 无黑框程序可通过 `go build -ldflags="-H=windowsgui" -o bilipdj-go.exe .` 构建；若希望 EXE 文件自身也有品牌图标，需要额外生成 Windows 资源（步骤见 [原 README 技术说明归档](./docs/README_TECHNICAL_ARCHIVE.md#bilipdj-go-品牌图标vue--windows)）。普通构建的托盘运行时仍可加载内嵌的专用图标。

~~~bash
go test ./...
go test -race ./...
go vet ./...
~~~

[Build and Release](./.github/workflows/release.yml) **仅在手动触发时运行**，构建 Windows / Linux / macOS 的 x64、ARM64 目标；`publish=false` 只生成构建工件，只有明确选择 `publish=true` 才创建 GitHub Release。版本来源为 `VERSION`。

## 🔒 安全提示

- Cookie、扫码回调、`SESSDATA`、管理员 Token 与 MCP Token **不要提交 GitHub，也不要粘贴到公开 Issue 或日志**。
- 默认监听本机。主动开放局域网端口时，队列读取接口可能向可达设备暴露排队信息；管理写操作需要强随机 `BILIPDJ_ADMIN_TOKEN`，同时应配置防火墙及可信网络。
- MCP **默认只读**，写操作使用独立的 `BILIPDJ_MCP_WRITE_TOKEN`；不要把 MCP 接口直接暴露公网。
- 自更新会短暂停止弹幕监听；**降级前先单独备份用户数据**。二进制回滚并不代表旧版能够读取新版存档。
- Bilibili / 抖音的直播协议可能变化；离线报文测试不等于真实直播间长期稳定性验证。

## 📚 文档

| 文档 | 内容 |
| --- | --- |
| [功能差异审核](./docs/FEATURE_PARITY.md) | 原 Python 版与 Go 版的支持差异和未验证部分 |
| [用户数据与迁移](./docs/USER_DATA_COMPAT.md) | Windows / Linux / macOS 数据目录、Python/Go 隔离 |
| [更新器说明](./docs/UPDATER.md) | 公益加速节点、进度、历史版本、回滚 |
| [性能监测](./docs/PERFORMANCE.md) | CPU、内存、I/O、网络监测口径与采样设置 |
| [MCP 接入指南](./docs/MCP.md) | HTTP/stdio MCP、安全授权与使用示例 |
| [README 技术说明归档](./docs/README_TECHNICAL_ARCHIVE.md) | 重排前完整的迁移记录、兼容 API、鉴权排障和版本历史 |
| [第三方授权](./NOTICE.md) | 内嵌 Vue 3 等第三方版权声明 |

## 🤝 参与贡献

可以通过 [Issues](https://github.com/ZzzHe2333/bilipdj-go/issues) 提交问题或需求。报告直播连接问题时请提供**不含 Cookie 与其他凭据**的错误信息，尽可能说明操作系统、Go 版版本、直播平台以及是否能够在同一网络下复现。

## ⭐ Star History

[![Star History Chart](https://api.star-history.com/svg?repos=ZzzHe2333/bilipdj-go&type=Date)](https://star-history.com/#ZzzHe2333/bilipdj-go&Date)

## 📄 许可证

本项目遵循 [GNU General Public License v3.0](./LICENSE)。内嵌 Vue.js 按其自身 MIT 许可使用，见 [NOTICE.md](./NOTICE.md)。
