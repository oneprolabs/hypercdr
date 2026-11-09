# Velero 恢复缓存策略

HyperCDR 在注册集群时自动配置 Velero 1.18.2 Data Mover 恢复缓存。该缓存只用于 CSI Snapshot Data Movement 和 File System Backup 的恢复阶段，不用于备份上传。

## 与节点缓存持久化的区别

本文的恢复缓存 PVC 与集群菜单中的节点缓存持久化不是同一配置。节点缓存持久化默认开启，大小为每个节点 5 GiB，使用节点目录 `/var/lib/hypercdr/kopia-cache`，挂载到 `/var/cache/hypercdr`。

正式镜像 `registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:velero-v1.18.2-hcdr.5` 及 `.6` 支持节点缓存持久化，不要求镜像标签带 `-cache-v1`。安装和升级应标记该能力；更新后的 Agent 在应用缓存设置时也会修复这些镜像缺失的能力标记。其他未声明支持的镜像不会自动启用。

验证不能只检查中控的默认值：还需确认 Node Agent 的 `HCDR_CACHE_PERSISTENCE=true`、`HCDR_CACHE_BUDGET_BYTES=5368709120`、上述目录挂载，以及 Agent 的 `cache settings applied` 日志。该日志表示配置和滚动更新完成，不表示某次业务任务一定命中缓存；缓存实际使用还需结合任务日志验证。仓库维护任务的缓存路径需单独检查，不能据此认定维护任务已持久化。

Agent 心跳的可选 `cacheState` 字段上报 Node Agent 的实际配置。仅在镜像能力、环境变量、目录挂载及完整滚动更新均通过检查后，返回 `phase=applied` 和真实的 `enabled`、`maxGiB`；未完成更新返回 `applying`，缺失配置返回 `unconfigured`，读取或配置异常返回 `unknown`。该检查只访问 Kubernetes API，不访问对象存储。

`.6` 配套 Agent 支持空闲期停止扫描、任务前按需访问，并为维护 Job 传递可用的节点缓存配置，详见 [对象存储按需访问](storage-on-demand.zh.md)。旧版本仅开启缓存不等于停止空闲扫描。

中控将 `cacheState` 持久化到集群 metadata，并以服务端 UTC 时间记录 `observedAt`。缓存设置接口保留 `settings` 表示期望配置，另外返回 `observed` 表示实际配置；集群离线或心跳过期时不返回可确认的实际配置。旧 Agent 不带该字段时，实际状态保持未知，不以默认开启值替代。设置抽屉读取 `observed`，保存后须同时确认任务成功及实际配置吻合才提示成功。

## 默认策略

- 用户无需在 DR Config 或 Drill 页面选择 PVC、路径或 StorageClass。
- 安装器优先复用注册时确定的默认 StorageClass，但只有存在动态 provisioner 且 `reclaimPolicy=Delete` 时才启用缓存。
- 不满足条件时不配置 `cachePVC`，Velero 使用 Data Mover Pod 的临时磁盘。
- `residentThresholdInMB` 默认为 `1024`；小于该数据量的恢复不创建缓存 PVC。
- Kopia `cacheLimitMB` 默认为 `5120`。Velero 会增加 20%并向上取整，因此通常申请 6 GiB 临时 PVC。
- PVC 由 Velero 针对每个 `DataDownload` 或 `PodVolumeRestore` 动态创建并在终态清理，HyperCDR 不预创建共享 PVC。

Node Agent 同时读取：

- `--node-agent-configmap=node-agent-config`
- `--backup-repository-configmap=backup-repository-config`

配置变更后必须滚动 Node Agent；Velero 1.18.2 不会热更新控制器启动时读取的缓存配置。

## Drill 前预检

Agent 在提交 Velero Restore 前读取实际 ConfigMap和 StorageClass：

- 未启用缓存：记录使用临时磁盘并继续。
- 缓存配置有效：记录 StorageClass、阈值和缓存上限并继续。
- 已启用但 StorageClass缺失、无动态 provisioner、不是 `Delete`，或 ConfigMap无法解析：在创建 Restore 前以 `RESTORE_CACHE_PREFLIGHT_FAILED` 返回准确错误。

Velero在运行时创建缓存 PVC 失败不会自动回退临时盘，因此预检失败不能被静默忽略。后续集群高级设置可以调整 StorageClass和缓存上限，但业务弹窗继续保持无缓存选项。
