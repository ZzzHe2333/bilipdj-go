# BiliPDJ Go

独立 Go 后端 + Vue 3 前端的轻量弹幕排队项目（v0.1.0 技术预览）。

## 目标功能

- Bilibili 直播弹幕 TCP 接收、心跳、zlib 解码与断线重连
- 抖音 webcast/im/fetch HTTP 轮询、Protobuf 解析与失败重试
- 双平台同时接入、统一弹幕事件、自动排队、手动队列管理
- Vue 3 本地离线控制台与 OBS 队列展示
- GitHub Releases 检查、按操作系统下载 SHA-256 校验的更新包

## 当前代码交付状态

仓库已经初始化。**v0.1.0 的完整源码与已编译的 Windows、Linux、macOS AMD64/ARM64 程序交付于本次 ChatGPT 会话的 `bilipdj-go-v0.1.0-complete.zip` 附件**，尚未全部提交到此 GitHub 仓库。

如需将附件中的完整源码同步到此仓库：

1. 从会话下载 ZIP，解压其中的 `release/bilipdj-go-v0.1.0-source.zip`。
2. 再解压源码 ZIP 到克隆仓库根目录，保留 `.github/workflows/release.yml` 等文件。
3. 执行 `go test ./...`、`go build .`，确认后提交代码。

原版 Python 项目：https://github.com/ZzzHe2333/bilipdj

> 两平台真实直播间持续接收仍待在线验证，抖音协议可能因 Cookie、风控或页面变化出现不可用。更新模块仅检查、校验、暂存；不自动覆盖可执行程序。
