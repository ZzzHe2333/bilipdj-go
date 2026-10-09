# BiliPDJ Go — Python PR #314 数据目录兼容

对照 Python 仓库 [PR #314](https://github.com/ZzzHe2333/bilipdj/pull/314) 已合并版本。该 PR 接续 #312，将高频队列和备份从 Roaming 分离。两项目独立维护，不会在后台实时互相覆写队列。

| 平台 | 配置、Cookie、权限及样式 | 队列 | 备份 | 日志约定 |
| --- | --- | --- | --- | --- |
| Windows | `%APPDATA%/bilipdj/` | `%LOCALAPPDATA%/bilipdj/archives/` | `%LOCALAPPDATA%/bilipdj/backups/` | `%LOCALAPPDATA%/bilipdj/log/` |
| macOS | `~/Library/Application Support/bilipdj/` | 配置目录 `archives/` | 配置目录 `backups/` | `~/Library/Logs/bilipdj/` |
| Linux | `$XDG_DATA_HOME/bilipdj/`（否则 `~/.local/share/bilipdj/`） | 配置目录 `archives/` | 配置目录 `backups/` | `$XDG_STATE_HOME/bilipdj/log/` |
| 显式 `-data` / `BILIPDJ_DATA_DIR`，含 Docker | 指定目录 `/data/` | Python `/data/core/cd/`、Go `/data/state.json` | Python `/data/backup/` | 指定目录 `/data/log/` |

默认模式下，Go **仅把用户设置写在 Roaming 的 `state.json`**，Go 自有队列与十个槽位保存在 Local 的 `archives/go-queue-state.json`；Python 的多平台共用队列是独立的 `archives/queue_archive_slot_N.csv`（五列带来源平台、操作时间）。不会把 Go JSON 直接当作 Python CSV，也不自动修改 CSV。Go 控制台「数据与存档」提供明确的只读扫描、备份后导入和导出 CSV。

## 从旧目录迁移

1. 无显式目录时，保留 #312 的来源选择：只有旧数据，按允许名单复制；双方均有数据且未选择，沿用旧目录并提示选择，重启后切换。不自动按时间覆盖。
2. 对已选择系统用户目录的用户，将原项目 `core/cd/`、旧 Roaming `core/cd/`、`backup/` 只复制到新的 `archives/` 和 `backups/`；保留来源。如果新路径已有不同文件，报告 `local_conflicts`，**不覆盖**。
3. Go 旧 `state.json` 同时存有配置与队列。首次分离时，先在 `backups/migration-backup/go-state-before-split-*.json` 备份完整原件，再写入 `archives/go-queue-state.json`。之后配置中保留 `queue_external` 标记；Local 存档丢失或损坏时拒绝以空队列启动。遇到问题需从备份恢复，不自动覆盖。
4. Go 队列的每次变动触发上一版本保护：按槽位在 `backups/queue/queue_archive_slot_N/` 以 Python 兼容五列 CSV 保存快照；**同一槽位每半小时最多一份，至多保留 96 份 Go 自有 `go-*.csv`**。不会清理 Python 自己的备份。
5. Go 更新包的下载临时文件使用 Local `cache/`；Roaming 不保存大 ZIP。Go 当前的最多 500 条日志仍为内存缓存，日志目录仅预留给未来的磁盘日志归档。
6. Windows Tk 的 `style-win.json`、`appearance-win.json` 与 Web/OBS 的 `style-web.json`、`appearance-web.json` 继续完全隔离；Go Web 不修改 `-win` 文件。

**明确限制：** 旧路径选为 `legacy` 时沿用原有 `state.json` 全量结构，不拆分；Docker/显式目录也不自动迁移。Python 和 Go 共享目录不等于共享同一队列写入进程，切勿把两个后端设置成同时写相同 JSON/CSV 文件。备份和 Cookie 属于敏感数据，请仅在本地管理界面使用。
