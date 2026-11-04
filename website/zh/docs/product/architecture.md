---
title: 中控架构
description: HyperCDR 控制面的组件构成、请求路径、集群连接方式与升级链路。
---

# 中控架构

控制面由 Go API、React 前端、PostgreSQL、HTTPS Edge、注册执行器和宿主机升级执行器组成。根目录 `docker-compose.yml` 是正式运行拓扑的依据，其他资产（如 `charts/`）不等同于在线安装路径。

## 组件与职责

| 组件 | 职责 | 形态 |
| --- | --- | --- |
| Edge | 在 12443 终止 TLS，把页面与 API/WebSocket 请求转发给活动颜色 | Nginx 容器 |
| 前端 | 控制台界面，蓝、绿各一份 | React 静态资源 |
| API | 业务接口、认证、调度、数据库访问 | Go 容器 |
| PostgreSQL | 持久化业务状态 | 共享数据库容器 |
| 注册执行器 | 执行 Install for me 注册，使用 kubeconfig 访问集群 API | 独立内部服务 |
| 升级执行器 | 读取平台升级任务，调用蓝绿脚本 | 宿主机 systemd 服务 |
| Agent | 集群内组件，出站连接平台并执行任务 | 集群内工作负载 |

蓝、绿两套应用各有独立网络并共享 PostgreSQL，API 另外接入出站网络。数据库、API、前端和注册执行器都不直接发布宿主机端口。

## 请求与数据路径

```text
浏览器        → HTTPS 12443 → Edge → 活动蓝/绿 前端 + Go API → PostgreSQL
集群 Agent    → 出站 WSS   → 平台任务与资源清单
集群 Agent    → Kubernetes API → Velero / OADP → 对象存储
```

可选官网独立运行在 12445，与中控前端不混用。

## 集群如何接入

集群 Agent 日常通过**出站** WebSocket 接入平台，采集资源并执行持久化任务。平台不需要主动访问业务集群，这对网络分区和防火墙策略更友好。

注册有两条路径：

- **Install for me**：注册执行器使用 kubeconfig 访问集群 API 完成安装，要求中控到集群 API 网络可达。
- **Run command**：在一台能访问集群的主机上执行生成的命令，适合中控无法直连集群 API 的环境。

两条路径产出的 Agent 相同，区别只在由谁执行安装动作。

### 三类集群的注册差异

注册接口明确支持 `native-kubernetes`、`huaweicloud-cce` 和 `openshift` 三类集群，它们共享中控的任务模型，但注册检查、组件镜像、超时和后端适配各不相同。

| 类型 | 注册方式与约束 |
| --- | --- |
| Native Kubernetes | 读取标准 Kubernetes API。kubeconfig 必须内嵌凭据、不超过 1 MiB，不能依赖外部 exec 插件或凭据文件 |
| Huawei Cloud CCE | 使用独立的 kubeconfig 检查与注册会话：先建立临时上传会话并检查 context，再创建注册任务；会话绑定组织和上传者，平台重启后需重新上传 |
| OpenShift | 走 OADP 兼容路径，受 OADP 版本、项目权限、SecurityContextConstraints、CSI 快照和对象存储配置约束；向导对默认资源范围有明确限制 |

CCE 仍然通过 Agent、Kubernetes API、Velero 和对象存储执行保护，不等同于直接调用云厂商备份 API。OpenShift 也不能按 Native Kubernetes 的全部资源选择能力理解。上线前应在目标发行版完成存储测试、同步和隔离恢复验证。

### Agent 与任务模型

Agent 主动通过出站 WSS 连接中控，上报 Kubernetes 版本、节点、namespace、资源和组件状态，并接收平台下发的任务。当前任务类型包括 `inventory`、`storage-sync`、`backup`、`restore`、`drill`、`takeover`、`retention-cleanup`、`upgrade` 和 `unregister`。

Agent 负责通信、编排和结果回报；Velero/OADP 负责 Kubernetes 资源与持久化数据；对象存储保存备份数据；PostgreSQL 保存平台元数据和任务状态。注销脚本不会删除应用 namespace、Longhorn、外部对象存储或其他命名空间的 Velero 资源。

## 版本与升级链路

平台以 GitHub Release manifest 交付组件版本。安装和成功的蓝绿升级会激活一份本地清单，集群注册与组件升级从这份清单读取应安装的版本。

平台升级的流程是：升级页面获取发布信息并创建任务 → 宿主机 upgrade runner 读取队列 → 生成目标清单 → 调用蓝绿部署脚本切换流量。

::: warning 平台升级不等于集群组件升级

平台完成升级后，集群侧组件仍需单独执行升级任务并核对 Agent 上报的版本。数据库迁移也不会因为流量回滚而自动撤销。

:::

## 持久化与回退边界

PostgreSQL 是唯一的持久化实现，没有 MemoryStore 之类的内存回退。用户主题偏好、任务、资源、版本信息全部来自数据库。

因此：恢复一份完整备份需要**数据库 + `HCDR_SECRET_KEY`**。只有数据库文件而没有密钥，不能视为可恢复。

相关阅读：[蓝绿部署与回滚](/docs/operations/blue-green)、[配置参考](/docs/deployment/configuration)、[运维](/docs/operations/)
