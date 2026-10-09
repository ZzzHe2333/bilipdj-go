# BiliPDJ Go 独立用户数据目录（与 Python PR #314 共存）

Python 继续使用目录名 **`bilipdj`**，Go 改用独立目录名 **`bilipdj-go`**。本变更只修改 Go 仓库，**不会移动、删除或写入 Python 的存档/配置**。

| 平台 | Go 配置、Cookie 与 Web 主题 | Go 队列 | Go 备份 | Go 更新缓存 / 日志预留 |
| --- | --- | --- | --- | --- |
| Windows | `%APPDATA%/bilipdj-go/` | `%LOCALAPPDATA%/bilipdj-go/archives/go-queue-state.json` | `%LOCALAPPDATA%/bilipdj-go/backups/` | `%LOCALAPPDATA%/bilipdj-go/cache/`、`log/` |
| macOS | `~/Library/Application Support/bilipdj-go/` | `archives/go-queue-state.json` | `backups/` | `cache/`、`~/Library/Logs/bilipdj-go/` |
| Linux | `$XDG_DATA_HOME/bilipdj-go/`（默认 `~/.local/share/bilipdj-go/`） | `archives/go-queue-state.json` | `backups/` | `cache/`、`$XDG_STATE_HOME/bilipdj-go/log/` |
| 显式 `-data` / `BILIPDJ_DATA_DIR`，含 Docker | 指定目录 `/data/` | Go `/data/state.json`（原便携格式） | 原指定目录，不自动拆分 | 原指定目录 |

Python 仍保留 `%APPDATA%/bilipdj`（配置）和 `%LOCALAPPDATA%/bilipdj/archives`、`backups`（队列/备份），macOS/Linux 仍保留原 `bilipdj` 路径。Go 在默认模式下没有向这些路径的后台写入。两套服务同时运行时仍需配置不同端口（默认都可能使用 9816）。

## Go 旧数据迁移

1. 首次使用 Go 默认目录时，只检查原 Go 便携 `data/state.json` 和旧共享 `bilipdj/state.json`，且文件必须是合法 Go 状态（含 JSON `config` 对象）。**不存在 Go 状态标记时不会碰 Python 数据**。
2. 只复制 Go `state.json`、已存在的 Web 主题 `style-web.json` / `appearance-web.json`；如果旧 Go 状态显式标记了 `queue_external`，再从旧 Local `bilipdj/archives/go-queue-state.json` **复制** Go 独立队列、仅迁移 `go-*.csv` 的 Go 历史快照。Python 的 `core/config.yaml`、五列 `queue_archive_slot_N.csv`、`-win.json`、插件和 Python 备份**不会自动迁移**。
3. 旧目录始终保留原件。新目录已存在有效 Go 状态时以它为准，不用旧状态覆盖；若内容不同会出现警告，可确认继续使用 Go 目录。两个不同的旧 Go 状态同时存在且内容不一致，启动迁移直接中止，要求用户先人工备份并选定来源。
4. 迁移过程支持重新启动恢复：只有新 Go 状态与旧 Go 状态仍完全相同，才允许补复制遗漏的旧 Go 队列；新的 Go 队列一旦投入使用，就绝不由旧文件回写覆盖。
5. `queue_external` 标记已出现但 Go `archives/go-queue-state.json` 丢失或损坏时拒绝空队列启动。已有的 30 分钟 Go 快照与 96 份轮转策略不变。Go 下载缓存与后端临时数据不会写入 Python 的 Roaming 目录。
6. Go 与 Python 的排队仍是**两个独立后端**。若要迁移 Python 队列，请在 Go「数据与存档」中明确发起只读扫描和手动导入；导入前会备份 Go 数据，原始 Python CSV 不修改。

## 与旧版功能的差异

- 以前 Go 与 Python 共用 `bilipdj`，甚至能选择旧根目录继续写；现在 **Go 默认模式始终只写 `bilipdj-go`**，不支持重新选择 Python 的目录作为持久化根。
- 存储冲突时不再用“旧目录”回退启动：Go 新目录是唯一活动目录。确认冲突只抑制提示，不触发复制或覆盖。
- Docker 与手动设置目录是显式例外：`-data` / `BILIPDJ_DATA_DIR` 保持原行为，但**用户必须避免主动指定同一个目录给 Python 和 Go**。
- `style-web.json`、`appearance-web.json` 依然作为 Go Web 的样式文件，仅保存在 Go 独立目录，不会修改 Python `bilipdj` 的同名文件。

**升级前建议**备份原 `bilipdj`、旧 Go `data`，以及已有的 `bilipdj-go`（如存在）；若两个旧 Go 状态不一致，先人工选定权威副本再启动新版。迁移是复制，不删除源文件。此变更尚未发行新的安装包，已在仓库代码中准备。
