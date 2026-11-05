---
title: OpenShift POC 验证清单
description: OpenShift 应用迁移验证的逐项检查表，覆盖源集群、目标集群、存储、网络与验收记录。
---

# HyperCDR OpenShift 应用迁移 POC 清单

**适用范围**：OpenShift 4.14/4.15、Linux AMD64、多节点集群；使用 HyperCDR、独立 `oadp-comm-agent`、OADP 1.3、Node Agent 和 Kopia；第一阶段仅验证全量备份、RWO PVC 恢复和完整 Drill。

::: tip 端口说明

本清单的端口沿用 POC 环境材料。实际部署以安装时配置的入口端口为准，控制台默认入口为 **12443**，源码开发环境可能使用 3002。

:::

## 1. 源集群信息采集 {#source}

- ☐ OpenShift 版本为 4.14 或 4.15，并记录完整补丁版本。
- ☐ 所有 Master 和 Worker 节点为 Linux AMD64，操作系统为 RHCOS 或 RHEL。
- ☐ 提供可访问集群 API 的 kubeconfig，账号具备 `cluster-admin`。
- ☐ 记录 API 地址、集群名称、节点数量及节点 Ready 状态。
- ☐ 确认集群未安装 OADP；HyperCDR 第一阶段不复用已有 OADP。
- ☐ 记录待迁移的 Namespace 及其 PVC、容量、AccessMode 和 StorageClass。
- ☐ 确认待迁移 PVC 全部为 `ReadWriteOnce`；RWX、ROX 暂不纳入本次 POC。
- ☐ 记录应用引用的镜像及拉取 Secret，确认目标集群能够获取这些镜像。
- ☐ 记录 CRD、ClusterRole、SCC、Operator 和外部服务等集群级依赖，用于判断全量恢复后是否仍需人工安装或配置；这些依赖不作为第一阶段的资源过滤条件。

## 2. 目标集群条件 {#target}

- ☐ OpenShift 版本为兼容矩阵中的 4.14 或 4.15。
- ☐ 所有 Master 和 Worker 节点为 Linux AMD64，操作系统为 RHCOS 或 RHEL。
- ☐ 提供可访问集群 API 的 kubeconfig，账号具备 `cluster-admin`。
- ☐ 未安装 OADP；注册时由 HyperCDR 自动安装。
- ☐ 目标 Namespace 可创建，或确认与现有 Namespace 不存在名称和资源冲突。
- ☐ 目标集群具备恢复工作负载所需的 CRD、Operator、SCC 及外部依赖。
- ☐ 目标集群可以拉取应用镜像以及阿里云中的 HyperCDR/OADP 镜像。

## 3. 对象存储条件 {#storage}

- ☐ 第一阶段使用 S3 或 S3 兼容对象存储，仅作为备份中转仓库。
- ☐ 提供 Endpoint、Bucket、Region、Prefix、Access Key、Secret Key 及 TLS 要求。
- ☐ 源集群和目标集群均可解析并访问对象存储 Endpoint。
- ☐ 凭证对指定 Bucket/Prefix 具备读、写、列举和删除权限。
- ☐ Bucket 容量满足应用资源和 PVC 全量数据，并预留重试及 Drill 空间。
- ☐ 源、目标集群使用同一仓库时，确认 Prefix 规划不会与其他 POC 或生产任务冲突。
- ☐ 对象存储使用私有 CA 时，提供完整 CA 证书链。

## 4. 网络条件 {#network}

- ☐ 源、目标集群到控制台：TCP 3002（HTTPS、API 和 WSS 共用）。
- ☐ 使用外部网关时，集群访问 TCP 443，由网关转发至控制台 3002，并支持 WebSocket Upgrade 和长连接。
- ☐ 控制台到源、目标集群 API：通常为 TCP 6443。控制台出站默认放通时，无需单独配置出站规则。
- ☐ 源、目标集群到阿里云镜像仓库：TCP 443。
- ☐ 源、目标集群到对象存储：通常 TCP 443；使用 HTTP 时按实际端口放通。
- ☐ DNS 能够解析控制台、集群 API、阿里云镜像仓库和对象存储域名。
- ☐ 控制台入站默认开放 TCP 3002；SSH 运维按需开放 TCP 22 并限制来源网段。

## 5. 目标存储条件 {#target-storage}

- ☐ 目标集群存在可动态创建 RWO PVC 的 CSI StorageClass。
- ☐ 记录源 StorageClass 到目标 StorageClass 的一一映射。
- ☐ 目标 StorageClass 支持应用要求的容量、VolumeMode 和文件系统。
- ☐ 目标存储可用容量满足所有待恢复 PVC，并预留 Drill 副本空间。
- ☐ CSI Provisioner、节点插件和挂载能力在目标 Worker 节点均为健康状态。

## 6. 部署条件 {#deployment}

- ☐ HyperCDR 控制台可用，默认入口为 `https://<host>:3002`。
- ☐ 版本清单包含 `oadp-comm-agent` 和经过校验的 OADP Catalog 镜像及摘要。
- ☐ OADP Operator、Velero、Node Agent、Kopia 和 AWS/S3 插件相关镜像已全部同步至指定阿里云镜像 Namespace，Catalog 不得回退到 Red Hat 公共仓库。
- ☐ 私有阿里云仓库的拉取凭证已配置，可供 Catalog、Operator 和 OADP 工作负载使用。
- ☐ 阿里云仓库为私有仓库时，已按 OpenShift 集群安全规范配置全局 Pull Secret 或等效镜像访问策略；`openshift-adp` 和 `openshift-marketplace` 中的 Namespace Secret 只是安装流程的补充，不能替代集群级凭证。
- ☐ 控制台具备注册执行器，且能访问源、目标集群 API。
- ☐ 集群与控制台时间同步，证书有效且系统时钟偏差可接受。

## 7. POC 验收记录 {#acceptance}

- ☐ 源、目标 OpenShift 集群均成功注册并保持 Agent 在线。
- ☐ OADP Operator、DPA、Velero 和所有目标 Worker 上的 Node Agent 均 Ready。
- ☐ 源、目标 BackupStorageLocation 均为 Available。
- ☐ 对一个包含 RWO PVC 的应用完成全量备份，Kopia 数据成功写入对象存储。
- ☐ 使用 StorageClass 映射在目标集群完成完整恢复。
- ☐ Pod、Service、配置、Secret 和 PVC 数据检查通过，应用健康检查通过。
- ☐ 完成一次完整 Drill，任务状态、事件和恢复点记录与现有流程一致。
- ☐ 记录 Catalog、CSV、DPA、Velero、Node Agent 及 Agent 实际就绪耗时，用于校准当前暂定安装超时值。

## 8. 推荐执行顺序 {#order}

1. 分别采集源、目标集群信息，确认版本、节点 OS/架构、权限、StorageClass 和 PVC AccessMode 均满足本清单。
2. 准备阿里云镜像仓库、OADP Catalog 和 S3 对象存储，并从源、目标集群分别验证拉取镜像及访问对象存储。
3. 先注册目标集群，再注册源集群；两端均选择 OpenShift，使用控制台生成的注册流程。
4. 在控制台存储配置中创建 S3 或 S3 兼容仓库，确认源、目标两端的 BSL 均为 Available。
5. 为源应用创建全量保护任务，检查 Backup CR、Kopia BackupRepository、对象存储制品和任务事件。
6. 选择目标集群执行完整恢复，检查 Namespace、工作负载、Service、Secret、PVC 及应用数据。
7. 执行完整 Drill 并保存任务详情、事件、恢复点和耗时，最后填写兼容矩阵和问题记录。

## 暂不纳入第一阶段 {#out-of-scope}

- 资源类型、标签或对象级过滤备份。
- 部分资源或部分制品恢复。
- RWX、ROX PVC。
- Azure、GCP 等非 S3 对象存储插件。
- 自动复用或自动卸载集群中已有的 OADP。
- 对 Operator/BuildConfig 等 OpenShift 特有资源作超出 OADP 实际恢复能力的额外编排。
