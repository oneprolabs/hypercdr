# Velero 恢复缓存策略

HyperCDR 在注册集群时自动配置 Velero 1.18.2 Data Mover 恢复缓存。该缓存只用于 CSI Snapshot Data Movement 和 File System Backup 的恢复阶段，不用于备份上传。

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
