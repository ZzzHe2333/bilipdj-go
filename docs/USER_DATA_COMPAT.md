# BiliPDJ Go — Python PR #312 数据兼容

本文件对应 Python 仓库 `ZzzHe2333/bilipdj` PR #312（审阅 head `6aff17a`），只修改 Go 仓库。

| 环境 | 用户数据默认目录 | Python 日志目录约定 |
| --- | --- | --- |
| Windows | `%APPDATA%/bilipdj` | `%LOCALAPPDATA%/bilipdj/log` |
| macOS | `~/Library/Application Support/bilipdj` | `~/Library/Logs/bilipdj` |
| Linux | `$XDG_DATA_HOME/bilipdj` 或 `~/.local/share/bilipdj` | `$XDG_STATE_HOME/bilipdj/log` 或 `~/.local/state/bilipdj/log` |

Go 暂时只保存最多 500 条内存日志，未提供磁盘日志归档。因此表格的日志路径是供未来归档及 Python 互操作的约定，**不是 Go 已在该目录持久写日志的声明**。

## 数据格式及安全边界

- `state.json`：Go 专有配置、跨平台统一队列及 10 个槽位。不要交给 Python 直接编辑。
- `core/config.yaml`、`core/cd/queue_archive_slot_N.csv`：Python 专有资料，Go 不自动修改。
- `.storage-choice.json`：与 PR #312 相同的 `{"schema":1,"choice":"user|legacy"}` 策略文件。Python 与 Go 对 `legacy` 的解析会随各自程序安装位置变化。
- `style-web.json`、`appearance-web.json`：Go Web/OBS 和 Python Web 的可移植 JSON 外观。Go 不读取/写入 Tk 专属 `*-win.json`。
- 兼容 Python 5 列 CSV：`序号,id,内容,最后操作时间,来源平台`。同名 B站/抖音用户会保留 `bilibili`/`douyin` 平台来源；但 CSV 不含原始 UID，因此导入后无法恢复 Go 原生 UID，请勿将历史 CSV 用户误作可信已验证 UID。
- 通过 Web `数据与存档` 页面预览和确认导入 CSV；写入 Go state.json 前先生成 `migration-backup/previous-go-state-*.json`，不修改 Python 文件。导出的 CSV 供手动导入 Python，不执行自动双向同步。

## 启动与冲突

1. `-data` 或 `BILIPDJ_DATA_DIR` 显式路径始终有效；容器原有 `/data` 仍在，绝不自动重定向到宿主机或容器 home。
2. 无显式目录、只有旧程序数据：按白名单复制旧 Go state.json 和 Python 允许的旧文件/目录到 OS 用户目录，旧文件保留，并在用户目录记录 `choice=user`。
3. 无显式目录、只有 OS 用户目录数据：直接使用 OS 用户目录。
4. 两份都有数据但没有明确选择：不合并、不比较时间戳、不覆盖；服务启动时沿用 Go 旧 `data/` 路径，Web 弹窗提示选旧目录或新目录。保存选择不会改变运行中进程，需重启。取消时依旧使用旧数据。
5. 如果 Python PR 已经选好了 `.storage-choice.json`，Go 遵循它，但 Python `legacy` 是 Python 源码/便携目录，Go `legacy` 是 Go 旧 `data/` 目录。建议各自先备份并使用 Web 导入独立状态。

不要让 Python、Go 在运行时将同一个 `state.json` 作为主状态（Python 当前不使用它）；两个后端的不同格式不能被视作自动双向实时同步。不要将配置 Cookie、备份文件及管理 API 公开到公网。
