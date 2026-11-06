# Velero 接口与任务进度统计核查

核查日期：2026-11-05。本文是开发分析记录，不是产品操作手册。

## 1. 核查范围与结论

核查仓库 `third_party/velero` 的 API 类型、进度生产代码、CLI、对象存储接口及指标定义，并通过 Kubernetes API discovery 核对123集群实际注册资源。实际镜像为 `velero-v1.18.2-hcdr.4`；这是 HyperCDR 定制镜像，不能直接假定其他 Velero 版本行为完全一致。

当前核查范围内，没有发现能在备份开始时提供“全部最终参与卷及其准确总逻辑字节数”的统一统计 API，也没有发现能够证明实时卷集合已完整的公共状态字段。不能只更换一个 API 就得到可靠的任务整体字节百分比。

另外，单卷运行中的 `totalBytes` 也不应被认定为不可变的准确值：Kopia 使用 `estimatedTotalBytes`，文件扫描和处理过程中可以更新估算，成功结束后才以快照结果校正最终数值。

## 2. 接口入口

Velero 的主要业务接口是 Kubernetes CRD，通过 Kubernetes API Server 的 GET、LIST、WATCH 和资源写操作访问，不是一个单独提供 `/backup/statistics` 的业务 HTTP 服务。

- v1：`/apis/velero.io/v1/namespaces/{namespace}/{resource}`。
- v2alpha1：`/apis/velero.io/v2alpha1/namespaces/{namespace}/{resource}`。
- 客户端：Kubernetes dynamic client、Velero生成客户端、controller-runtime client。
- CLI 的 get、describe、logs 等命令主要读取上述资源或下载对象存储产物，并非额外的全局实时统计来源。
- Prometheus 指标、健康检查及可配置的 pprof 属于监控/诊断接口，不等同于任务进度接口。

## 3. 集群实际注册的13类资源

| 资源 | API版本 | 用途 | 与完整统计的关系 |
| --- | --- | --- | --- |
| Backup | v1 | 备份范围、生命周期、资源处理进度、告警错误、快照及异步操作计数 | 无任务级总字节字段 |
| Restore | v1 | 恢复范围、Namespace映射、生命周期、资源进度、异步操作计数 | 无统一完整字节进度 |
| PodVolumeBackup | v1 | 文件系统备份的单Pod卷操作、快照ID、进度、增量字节 | 单卷数据来源，运行中逐步创建 |
| PodVolumeRestore | v1 | 文件系统恢复的单Pod卷操作、快照ID、恢复进度 | 单卷来源，可关联源快照 |
| DataUpload | v2alpha1 | CSI数据移动上传操作、源PVC、快照、进度 | 操作级，不是全部任务总量 |
| DataDownload | v2alpha1 | CSI数据移动下载操作、目标卷、快照、进度 | `spec.snapshotSize`可提供源快照逻辑大小，须检查是否存在 |
| Schedule | v1 | 定时备份模板和调度状态 | 不提供数据总量 |
| DeleteBackupRequest | v1 | 异步删除备份及相关数据、状态和错误 | 不提供同步任务字节总量 |
| DownloadRequest | v1 | 申请备份/恢复产物的临时下载URL | 可获取结果清单和日志，但不是提前扫描接口 |
| BackupRepository | v1 | 备份仓库、维护与可用状态 | 仓库状态不代表某次任务总量 |
| BackupStorageLocation | v1 | 对象存储位置、验证和同步状态 | 不提供某次任务完整传输总量 |
| VolumeSnapshotLocation | v1 | 云盘快照位置配置 | 配置接口，不是实时字节接口 |
| ServerStatusRequest | v1 | 服务版本、插件等服务信息 | 不提供任务数据统计 |

CSI路径还涉及 `snapshot.storage.k8s.io` 的 VolumeSnapshot、VolumeSnapshotContent、VolumeSnapshotClass。快照就绪或 `restoreSize` 不等同于对象存储新增上传字节，也不能直接替代当前FSB路径的逻辑处理总量。

## 4. 各类进度字段的准确含义

### 4.1 Backup和Restore的资源数量

- `Backup.status.progress.totalItems/itemsBackedUp`。
- `Restore.status.progress.totalItems/itemsRestored`。
- API注释明确说明进度是 best-effort；资源总数可能随插件返回关联资源、过滤等发生变化。
- 这些字段统计Kubernetes资源项，不是卷个数，更不是卷数据字节数。
- 快照 attempted/completed、异步 item operation attempted/completed/failed 是对应操作计数，也不能作为逻辑数据量百分比。

依据：`pkg/apis/velero/v1/backup_types.go`、`restore_types.go`。

### 4.2 单卷和数据移动操作

PVB、PVR、DataUpload、DataDownload使用 `status.progress.totalBytes/bytesDone`，共享类型为 `DataMoveOperationProgress`。

PVB还提供 `status.incrementalBytes`，表示相对上一备份新增或变化的数据；不能把它与完整逻辑大小混为一谈，也不能自动认定为精确网络流量或对象存储净增长。

Kopia进度实现使用 `EstimatedDataSize`、`ProgressBytes` 更新估算总量及已处理字节；成功结束时，provider将 `TotalBytes` 和 `BytesDone` 校正为快照结果大小。即使已收齐卷，运行中仍需区分估算与最终值。

依据：`pkg/apis/velero/shared/data_move_operation_progress.go`、`pkg/uploader/kopia/progress.go`、`pkg/uploader/provider/kopia.go`。

### 4.3 异步插件操作

`OperationStatus`含 `nCompleted`、`nTotal`、`operationUnits`、phase和时间。单位由具体插件操作定义，可能是bytes，也可能是其他工作量；不能不检查单位就汇总，更不能与同一卷PVB或DataUpload重复计数。

插件的BackupItemAction/RestoreItemAction、VolumeSnapshotter、ObjectStore等属于Velero扩展接口，不是另一个可直接获取全部卷准确数据量的北向统计服务。

依据：`pkg/itemoperation/shared.go`及 `pkg/plugin`。

## 5. 结果清单和下载接口

DownloadRequest支持以下产物：

- 备份：BackupLog、BackupContents、BackupResourceList、BackupResults。
- 卷和快照：BackupVolumeInfos、BackupVolumeSnapshots、CSIBackupVolumeSnapshots、CSIBackupVolumeSnapshotContents。
- 异步操作：BackupItemOperations、RestoreItemOperations。
- 恢复：RestoreLog、RestoreResults、RestoreResourceList、RestoreVolumeInfo。

创建请求后读取 `status.phase/downloadURL/expiration`，下载链接是临时链接，不应写入公开日志或产品文档。

对象存储适配层的 `BackupStore`还提供 `GetBackupMetadata`、`GetPodVolumeBackups`、`GetBackupVolumeInfos`、`GetBackupContents`、`GetRestoreResults`等读取能力。这些Go接口是内部存储抽象，不能误写成对外HTTP端点。

BackupVolumeInfo可记录PVC/PV、备份方式、是否跳过及原因、结果、PVB/快照/数据移动信息；适合备份结果核对和后续恢复规划。其文件由备份结果处理流程写入，不能假定备份刚开始就完整存在。

恢复可以优先读取选定备份的这些产物、PVB快照信息以及DataDownload.snapshotSize，再按实际恢复过滤范围计算总量。部分恢复、跳过卷、不同数据路径和元数据缺失都必须处理。

依据：`pkg/apis/velero/v1/download_request_types.go`、`pkg/persistence/object_store.go`、`internal/volume/volumes_information.go`、`pkg/backup/backup.go`。

## 6. Prometheus指标不解决提前总量问题

指标包含备份次数、成功/失败数、耗时、资源数、快照数、队列和操作时延，以及 `backup_tarball_size_bytes` 等。

`backup_tarball_size_bytes`不能当成所有卷的完整逻辑大小；备份归档、仓库对象大小、逻辑处理量、增量变化量和真实网络传输量是不同口径。指标通常也不是为精确重建某个任务的逐卷实时状态设计的。

依据：`pkg/metrics/metrics.go`。

## 7. 对当前问题的判断

Agent每次查询当前已出现的PVB/DataUpload并汇总。`allTotalsKnown`只检查这些对象是否都有总量，不证明整个任务的卷集合已完整。

149实际任务 `fbf8891d-2482-46f1-9967-fc685247724f` 的第一个PVB于北京时间10:23:57创建、10:24:07完成；第二个PVB于10:24:12才创建。因此第一个卷的完成总量不是整个任务总量。新增的Agent白盒测试和前端组件回放已复现“百分比→确定总量提示→百分比”。

需要修正之前的判断：不能仅增加一个“全部卷已发现”的标志，就宣称备份运行中具备准确不变的总量；单卷扫描估算也可能变化。

## 8. 后续设计约束

1. 统一统计口径，区分逻辑已处理量、估算总量、最终逻辑量和增量数据量。
2. 未证明卷集合完整时，不将已发现卷的百分比称为任务整体百分比。
3. 备份运行中持续展示已处理量和已发现/完成卷状态；如提供单卷百分比，应明确其范围及估算性质。
4. 恢复优先以备份结果元数据构建目标卷清单和总量，缺失时降级，不拿PVC申请容量补分母。
5. 数据传输完成后保留最终结果；恢复点发布、旧点清理、工作负载就绪继续按独立阶段显示。
6. 不修改Velero业务流程、不强制百分比单调、不通过隐藏未知状态伪装准确性。

## 9. 同步详情展示约定

界面使用英文。`Data processed` 表示逻辑已处理量，不表示网络上传量；备份阶段不显示整体百分比或整体剩余时间。逐卷百分比标注 `Estimated`，允许随扫描估算变化，不伪造单调进度。

`Volumes started` 仅统计有启动时间、处于 `InProgress`、已有处理数据或已完成的卷任务。`New`、`Accepted`、`Prepared` 等未提供启动证据的对象显示 `Waiting`；已启动但总量未知显示 `Scanning`；已启动且总量已知显示 `Syncing`。提前失败但未启动的对象显示 `Failed`，不增加启动计数。该计数是已观测启动数量，不代表整个备份的最终卷数。

Agent 在既有逐卷消息中追加 `volume`、`pod`、`namespace`、`startedAt` 字段，不改变执行流程。旧 Agent 缺少这些字段时使用备份对象名称与已有状态；缺少逐卷明细时仅展示已处理量，不推测卷数。

逐卷状态变化即使未改变备份对象的 resourceVersion 和总字节数也需要记录；仅 elapsedSeconds 变化不产生额外事件。进入创建恢复点阶段后保留最后的数据统计，旧恢复点替换清理归入 `Creating Restore Point`。本次修改不自动部署。

## 10. 发现与同步两条子事件

`Backing Up Data` 下固定展示 `Discovering Volume Data` 与 `Syncing Volume Data`。卷详情默认折叠，显示 Pod/卷名，内部任务对象名不再常驻展示；所有卷共用一条估算进度条，而非逐卷多条进度条。

文件系统备份的发现结束信号由定制 Velero 在全部 ItemBlock 处理完成后、等待 PVB 完成前发布：Backup 注解 `hypercdr.io/pod-volume-discovery=<backup UID>:<PVB count>`。发布失败仅记录信息，不改变备份结果。Agent 在查询 PVB 前读取信号，核对 Backup UID、任务数量及类型，仅匹配的 PVB 清单才报告 `discoveryComplete`、`discoveryCount`。该信号不适用于异步插件后续创建的 DataUpload；这类任务及旧版 Velero 缺少信号时保守降级，不推测发现完成。

仅明确发现结束且所有卷都有正的有效 `totalBytes` 时展示总估算百分比。总量仍是 Kopia 扫描估计，允许变化。进度条已经出现后如大小统计缺失，保留进度条区域并移除数值，显示 `Updating volume statistics`，不回到发现阶段，也不冻结虚假百分比。已完成的备份保留完成条和处理量。零字节卷在缺少可靠总量标志时不参与运行中百分比，等待完成结果。终态成功可确认发现和数据备份均已结束。
