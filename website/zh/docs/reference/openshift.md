---
title: OpenShift 与 OADP
description: OpenShift 集群的 OADP 支持范围、兼容边界、发布资产要求和生命周期行为。
---

# OpenShift 与 OADP

HyperCDR 保持**一套控制面架构**和**一套备份/恢复任务契约**。注册时新增 `openshift` 作为集群类型。

Native Kubernetes 与 Huawei CCE 继续部署 `comm-agent` 搭配社区 Velero；OpenShift 部署独立发布的 `oadp-comm-agent` 搭配 OADP。

环境采集、前置条件、网络、存储和 POC 验收清单见 [OpenShift POC 验证清单](/docs/reference/openshift-checklist)。

## 已确认范围 {#scope}

- OpenShift 4.14 与 4.15，在已验证的源/目标兼容矩阵内。
- 两端使用**同一个**已确认的 OADP 通道（默认 `stable-1.3`），确保备份格式与 Kopia 仓库行为在 4.14/4.15 迁移对之间不产生差异。使用其他通道需要单独验证发布。
- Linux AMD64 的控制面与 Worker 节点；预期多节点集群。
- **仅全量备份**和现有的 Drill 工作流。
- 使用 OADP node-agent 与 Kopia 保护的 RWO PVC。
- HyperCDR 托管的 S3 或 S3 兼容对象存储仅作为备份中转仓库。Azure、GCP 等其他厂商专用插件不在第一阶段范围内，`oadp-comm-agent` 会拒绝。
- 集群能从 HyperCDR 的阿里云镜像仓库拉取所有已确认的 OADP 和 HyperCDR 镜像。
- 注册凭据具备 `cluster-admin` 能力。
- 注册时会检查整个集群是否已存在 OADP Subscription、DPA 或 OADP Operator CSV，第一阶段**拒绝复用**，且不会自动移除已有安装。

## 兼容边界 {#boundary}

控制面保留现有的注册、心跳、任务、状态、存储映射、对象存储凭据和 Drill 契约。

OADP 特有的 OLM、DPA、SCC、node-agent 和 Kopia 行为，归属于 OpenShift 注册 provider 和 `oadp-comm-agent` 内部。现有的 Kubernetes 与 CCE 安装路径必须保持行为不变。

## 发布资产要求 {#release-assets}

每个已确认的发布版本必须为以下组件发布不可变的 AMD64 镜像与摘要，并放在配置的阿里云仓库中：

- OADP operator / catalog 或 bundle
- Velero
- node-agent / Kopia
- 对象存储插件
- `oadp-comm-agent`

::: danger 不允许回退到公共仓库

OpenShift 安装器**绝不**回退到公共镜像仓库。

:::

发布之前，对已镜像的 catalog 运行：

```bash
scripts/release/verify-oadp-catalog.sh
```

它会拒绝缺少 `redhat-oadp-operator` 通道 `stable-1.3` 的 catalog，或镜像引用指向配置阿里云仓库之外的 catalog。

镜像同步需要使用有效的 Red Hat pull secret 和阿里云仓库凭据，并且**故意作为发布操作执行**，不在客户集群注册时进行：

```bash
scripts/release/sync-oadp-images.sh --registry <aliyun-host/namespace>
```

该命令用 `oc-mirror` 完成发布期镜像同步：只选择 `redhat-oadp-operator` 包和已确认的 `stable-1.3` 通道，同步所有相关镜像，校验结果，并输出 `HCDR_OADP_CATALOG_IMAGE` 供发布清单使用。注册过程消费这个不可变值，不访问 Red Hat 仓库。

## 生命周期行为 {#lifecycle}

保留现有的解除注册流程。对 OpenShift 集群，控制面在 `openshift-adp` 中寻址 Agent，按请求删除该专用命名空间，且**不调用**社区 Velero 的 CRD 清理路径——这些集群级资源由 OADP/OLM 拥有。

Native Kubernetes 与 Huawei CCE 继续使用 `hypercdr-agent` 及原有的卸载行为。

## 暂定安装超时 {#timeouts}

在从真实 OpenShift 4.14 和 4.15 环境收集到耗时数据之前，OADP CSV 等待上限只是**安全边界**，不是预期安装时长，也不构成兼容性声明。

| 配置 | 默认值 | 说明 |
| --- | --- | --- |
| `HCDR_OADP_INSTALL_TIMEOUT_SECONDS` | 600 秒 | OADP CSV 等待上限 |
| `HCDR_OPENSHIFT_REGISTRATION_TIMEOUT_SECONDS` | 2100 秒 | 仅 OpenShift 的注册执行器总上限 |
| Native Kubernetes / CCE 注册上限 | 1200 秒 | 保持原值 |

发布确认时必须在两个受支持的 OpenShift 版本上记录 CatalogSource、CSV、DPA、Velero、node-agent 和 Agent 的就绪耗时，并用这些测量值校准生产默认值。

## 相关

- [OpenShift POC 验证清单](/docs/reference/openshift-checklist)
- [集群注册与管理](/docs/manual/clusters)
- [支持与适配范围](/docs/reference/support-scope)
