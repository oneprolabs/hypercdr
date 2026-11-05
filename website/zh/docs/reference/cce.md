---
title: Huawei CCE
description: 华为云 CCE 第一阶段的支持范围、参考部署、注册流程、存储约定与验收条件。
---

# Huawei Cloud CCE

HyperCDR 在 Native Kubernetes 之外，将华为云 CCE 作为托管 Kubernetes 类型提供支持。原有 Native Kubernetes 注册路径保持兼容。

CCE 相关工作在安装器 Provider 契约内完成，kubeconfig、身份、动态存储、证书和 Worker 连通性检查均不由 Native Kubernetes provider 执行。

## 第一阶段范围 {#scope}

- 由华为云维护的标准 CCE 集群
- Linux amd64 Worker 节点，单个 Worker 节点即可运行
- `kubectl` 可用的静态 kubeconfig 凭据
- 社区版与企业版控制台
- 通过 S3 兼容私有端点访问华为 OBS
- CCE 与 Native Kubernetes 之间的双向备份、恢复与演练
- CCE 与控制台 ECS 位于同一区域和 VPC

::: warning 第一阶段之外

CCE Autopilot、Windows Worker、ARM64 Worker 和 exec-plugin kubeconfig 不在第一阶段的兼容范围内。

:::

## 参考部署 {#deployment}

控制台与 Bootstrap Portal 以 Docker Compose 运行在 Linux ECS 上。ECS 需要一个公网 IP 供运维访问，一个私网 IP 承载 Agent 流量。

CCE Worker 不需要公网 IP，可通过出站 NAT 从阿里云 ACR 拉取 HyperCDR 镜像。OBS 通过私有端点访问。

## 注册 {#registration}

在 Register Cluster 抽屉中选择 **Huawei Cloud CCE**，将 CCE kubeconfig 下载到 Linux 运维主机（推荐路径 `~/.kube/hypercdr-cce.yaml`），复制生成的命令并执行。

安装器依次发现候选 kubeconfig、确认 context、校验 CCE、检查权限、检查 Linux/amd64 节点、检查 CSI 存储、检查镜像和连通性，随后安装共享的 HyperCDR Agent 组件栈。

自动化场景可指定 `--kubeconfig`、`--context` 和 `--interactive false`。

### 端点选择行为

注册命令可同时包含私有和公网 WebSocket 端点。Agent **优先尝试主端点**，仅在启动时使用公网端点作为后备。

::: warning 端点连接后保持固定

端点连接后，该地址在 Agent 进程生命周期内固定：重连使用同一地址，运行期**不做**网络探测，也不自动切换端点。需要重新选择端点时，重启 Agent。

也支持不配置公网 Agent 端点的部署。

:::

### 证书与预检

安装器在创建 Agent 工作负载之前下载并校验控制台证书，**不会**静默回退到不安全的 Agent TLS。

镜像拉取和 CCE 的动态 PVC 供给先在临时预检 Namespace 中执行。正式 Agent 资源创建前会删除该 Namespace；首次注册失败时，临时 Namespace 和 Agent Namespace 一并回滚。

## 存储 {#storage}

HyperCDR **不要求** Longhorn。Agent 状态、恢复缓存和被保护应用的存储按各自能力单独评估。演练默认使用目标 StorageClass，用户在高级选项中另行选择时除外。

新的对象数据使用按租户和集群划分的前缀：

```text
hypercdr/v1/tenants/<tenant-id>/clusters/<cluster-id>/
```

写在旧前缀 `hypercdr/clusters/` 下的开发数据不会迁移到新前缀。清理操作会拒绝空、根、旧格式、跨租户以及其他非预期前缀。

## 验收条件 {#acceptance}

正式支持 CCE 前，以下真实环境测试必须全部通过：

- 注册
- Agent、Velero、Node Agent 就绪
- OBS 连通性
- 备份
- 恢复
- CCE → Native 与 Native → CCE 两个方向的演练
- 注销后的清理
- 安装失败后的重试
- 完整的 Native Kubernetes 回归测试

## 相关

- [集群注册与管理](/docs/manual/clusters)
- [支持与适配范围](/docs/reference/support-scope)
- [Velero 恢复缓存](/docs/operations/restore-cache)
