# BiliPDJ-Go 性能监测：七项精简指标

Web 控制台 →「性能监测」固定显示 **7 张指标卡**。根据用户 2026-10-10 的调整要求，不再收集或展示整机 CPU、网络收发、GPU 或 NPU 数据；删除了 Windows/Linux 后端的对应整机 CPU、网卡枚举逻辑。

## 实际保留的七项

| 指标 / API 字段 | 统计内容 | 更新及平台限制 |
| --- | --- | --- |
| 进程 CPU `cpu` | BiliPDJ-Go 当前进程的 CPU 时间增量，对所有逻辑处理器归一化 | 相邻两次采样计算；Windows GetProcessTimes，Linux/macOS getrusage |
| 内存占用 `memory` | **BiliPDJ-Go 本进程**内存 | Windows 当前 Working Set，Linux 当前 RSS，macOS 峰值 RSS（若不存在则回退 Go 堆） |
| 数据目录占用 `data_disk` | 当前 Go 配置/用户数据目录中所有普通文件总大小 | 扫描最多每 60 秒一次；Windows 通常为 Roaming 的 `bilipdj-go`，可能不含 LocalAppData 的排队存档 |
| 磁盘读取速率 `disk_read` | Go 进程 I/O 读取字节数的增量 / 秒 | Windows GetProcessIoCounters（包含部分缓存/设备 I/O），Linux /proc/self/io；macOS 暂不可用 |
| 磁盘写入速率 `disk_write` | Go 进程 I/O 写入字节数的增量 / 秒 | 同上 |
| **项目文件占用** `project_disk` | 当前 BiliPDJ-Go 可执行文件，以及同目录存在的辅助程序、图标、README、LICENSE、NOTICE、更新清单、docs 和 third_party 文件 | 使用真实文件长度，不把磁盘上其他软件算进项目；Vue 及 OBS 已嵌入二进制；最多 60 秒重新扫描 |
| **存档占用大小** `archive_disk` | 已解析的 Go 排队存档目录（storage.Plan.ArchiveDir），包含全部槽位及该目录内其他归档文件 | Windows 通常位于 LocalAppData/bilipdj-go/archives；显式 `-data` 模式使用 `<data>/core/cd`；不存在则真实显示 0B；最多 60 秒重新扫描 |

### 三项磁盘空间的区别

- **项目文件占用**只包含当前运行的二进制和同安装目录的已知发行文件，不扫描整个安装父目录（可能有其他软件）。用户用 `go run .` 启动时统计临时可执行文件，需用正式安装路径评估实际程序体积。它不包括用户数据。
- **数据目录占用**是 Go 配置文件所在数据根目录的递归统计。Windows Roaming 和 LocalAppData 物理位置分开时，两者不包含彼此；便携/显式 `-data` 模式下存档可能位于数据根目录中，因此该数值**会包含**存档占用，不应直接与存档卡数值相加。
- **存档占用大小**从当前 `storage.Plan.ArchiveDir` 获取实际 Go 原生队列存档路径，不读取 Python `bilipdj` 的 CSV 或旧备份目录。统计的是目录内的普通文件总字节数，并非队列当前人数或内存里的队列大小。

三项空间读数是普通文件**逻辑大小**（按字节），不是文件系统占用块数；稀疏文件、压缩及 NTFS 分配单元可能使系统“占用空间”与它不同。对无法读取、文件条目过多或扫描超时的目录会显示**不可用**，避免把读取失败当成 0。

## 采样设置

- 默认 **每 2 秒**检测一次；可以设为 **0（关闭）** 或 **1–1200 秒**。
- 保留不等距进度条的四个标尺：0、12、120、1200，亦支持手动输入秒数。
- 只在用户打开「性能监测」且标签页可见时请求 `GET /api/performance`。切换页面、隐藏页面或设置 0 就停止发请求；Go 后端不创建常驻监测循环。
- CPU 和磁盘 I/O 速率需要前后两个采样点，初次只显示“建立基线”；内存和空间可在第一帧展示。
- 空间目录缓存 60 秒、每个目录最多遍历 10000 个条目且扫描不得超过 150ms；不跟随符号链接。
- API 只允许现有管理员鉴权通过的访问；不会返回磁盘绝对路径、Cookie、Token。

## 与 Python Windows 版区别

Python 版的 `apps/windows/performance_monitor.py` 基于 psutil，以整机 CPU 等为主；Go 版如今刻意只提供上述七项 **BiliPDJ-Go 自身**监测能力，尤其多出了项目程序文件体积和实际 Go 排队存档占用两个单独指标。未支持的磁盘 I/O 会显示不可用，而不是误报为 0 B/s。
