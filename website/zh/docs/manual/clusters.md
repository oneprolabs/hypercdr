---
title: 集群注册与管理
description: 注册 Native Kubernetes、CCE 与 OpenShift 集群，检查在线状态，升级集群组件，并完成集群注销。
---

# 集群注册与管理

入口：**Configuration → Clusters**。

## 注册前确认

| 检查项 | 说明 |
| --- | --- |
| 集群类型 | Native Kubernetes、Huawei CCE 或 OpenShift，决定安装哪套组件 |
| kubeconfig | 具备创建 Namespace、CRD、工作负载等资源的权限 |
| 网络出口 | 集群节点能访问平台入口和镜像仓库 |
| 镜像可达 | 节点能拉取版本清单中指定的组件镜像 |

## 注册方式

注册页提供两种方式，安装的 Agent 相同，区别只在于**由谁执行安装**：

| 方式 | 执行位置 | 适用条件 |
| --- | --- | --- |
| **Install for me** | 平台注册执行器 | 控制台能通过网络访问 kubeconfig 中的集群 API |
| **Run command** | 一台能访问集群的主机 | 平台无法直连集群，或需要人工审阅安装命令 |

按页面第一步准备所需文件，再执行当前生成的命令。

![选择由平台安装并准备 kubeconfig](/images/user-guide/cluster-direct-install.png)

截图是 Install for me 方式下的 kubeconfig 准备步骤。两条路径装出来的 Agent 相同，区别只在谁执行安装命令。

::: danger 凭据安全

不要把访问 token 或 kubeconfig 分享到公开 issue，也不要提交到代码仓库。注册命令包含访问凭据，分享前需要脱敏。

:::

## 注册完成的判断标准

安装完成后，回到集群列表确认：

- 集群状态为 **online**；
- 节点清单已上报，数量与实际一致；
- Namespace 清单已上报；
- Agent 版本可读，与版本清单中的组件版本一致。

页面出现「注册完成」提示不能作为完成依据，清单上报才能证明 Agent 已连通。

## 集群类型的影响

按实际发行版选类型，不要凭集群名字猜。默认选中的是 Native Kubernetes。

![注册向导中的三类集群选择](/images/user-guide/cluster-types.png)

| 类型 | 执行组件 | 注意点 |
| --- | --- | --- |
| Native Kubernetes | 社区 Velero | 依赖 CSI Snapshot 与存储插件能力 |
| Huawei CCE | 社区 Velero + CCE 专项 provider | 不能据此推断 CCE Autopilot 或所有节点架构可用，见 [CCE 专项](/docs/reference/cce) |
| OpenShift | OADP | 安装路径与验证清单独立，见 [OpenShift 与 OADP](/docs/reference/openshift) |

## 集群处于 offline 状态

按以下顺序排查：

1. Agent 的 Pod 是否在运行，日志中是否出现反复重连；
2. 集群到平台 WSS 地址的网络是否可达，证书是否受信任；
3. 注册凭据是否有效，平台侧是否有对应的集群记录；
4. 平台入口地址是否变更。已安装 Agent 的连接地址**不会**随平台改址自动迁移。

详细步骤见[故障排查](/docs/operations/troubleshooting)。

## 组件升级

集群里的 **Comm Agent** 和 **Velero/OADP** 分别升级，两者与平台升级是三项独立操作。更新控制台之后，要逐个集群检查有没有可用更新，不要默认 Agent 已经跟着升了。

![集群卡片中的组件版本、更新状态和连接状态](/images/user-guide/cluster-components.png)

图中的 **Up to date** 表示当前页面没有该组件的新版本，**Ready** 和 **Last Seen** 用来判断连接状态。示例版本不代表你的部署应该固定用这个版本。

::: warning 平台升级不覆盖组件升级

平台蓝绿升级只切换控制台自身的镜像与流量；集群中的 Agent、Velero/OADP 仍要单独发起升级任务并验收。

:::

### 升级前确认

- 目标集群和变更窗口已确定，当前版本和回退方式已记录；
- 集群 online，节点能拉取目标镜像，资源够完成滚动更新；
- 没有正在运行的备份、演练、恢复、清理或升级任务。

### 操作

1. 进入 **Configuration → Clusters**，找到目标集群。
2. 在对应组件的 **Update** 入口检查更新：通信组件走 **Upgrade Agent**，Velero 走 **Upgrade Velero**。没有更新入口时，不要拿另一个组件的升级顶上。
3. 核对当前版本、目标版本和镜像标识。Velero 还要核对服务端和 Node Agent 的范围。
4. 确认维护条件后提交。**只打开升级窗口不会创建任务**，不执行就取消。
5. 在集群任务记录里跟踪，等通信恢复、组件就绪。

### 字段说明

| 信息 | 含义 |
| --- | --- |
| Current / Current Version | 当前版本。`Unknown` 表示没拿到版本信息，不等于必须升级 |
| Target / Target Version | 本次目标版本，提交前核对与平台发行版的兼容性 |
| 镜像摘要 | 用于确认实际镜像身份；标签相同不代表内容相同 |
| Upgrade Scope | Velero 升级窗口里 Server Deployment 与 Node Agent DaemonSet 的范围 |
| Node Agent ready | 已就绪的节点代理数 / 期望数；升级后要确认达到期望值 |

### 结果确认

升级任务成功、组件上报目标版本、Agent 恢复 online、Velero 与 Node Agent 就绪，四项都满足才算完成。然后在维护窗口内按验证计划检查保护和恢复功能 —— 任务创建成功不能当作升级验收。

### 常见问题

- Agent 滚动更新期间会短暂离线。持续离线时查 Pod、镜像拉取和出站连接，不要反复提交升级；
- OpenShift 的组件路径与 Native Kubernetes 不同，以页面实际提供的组件和适配范围为准；
- **平台回滚不会回滚集群组件。**

## 下线集群（注销）

正常注销解除平台对该集群的管理，并清理受管的 HyperCDR 与 Velero 资源。**强制移除不能证明集群端组件和备份数据已清理**，两者不要混用。

![注销前的关联配置、恢复点和数据清理确认](/images/user-guide/unregister-precheck.png)

图中即使显示 **Ready**，仍有源保护配置和恢复点，且数据清理选项没有勾选。`Ready` 只说明可执行，不能代替数据删除的授权。

### 注销前确认

- 该集群不再承担当前的保护或恢复任务，数据清理已获授权；
- 正常注销要求 Agent online，且没有冲突的运行任务；
- 已审阅该集群作为源端和目标端的保护关系、恢复点与备份保留要求。

### 操作步骤

1. 在 **Configuration → Clusters** 找到集群，打开操作菜单里的 **Unregister Cluster**。
2. 核对 **Selected Cluster**，等 **Readiness check** 跑完。
3. 阅读阻塞原因，检查源保护配置、目标引用、恢复点和活动任务数量。不要绕过未完成的任务。
4. 如果页面要求 **Clean up related data**，先看清楚下表的后果。拿到清理授权后才能勾选，不要为了让按钮可用而直接勾。
5. 条件满足后点 **Confirm Unregister**，到 **Recent Tasks** 跟踪结果。
6. 只有 Agent 或集群永久不可用、正常注销走不通时，才考虑 **Force remove**。按页面要求输入 `Yes` 执行，并另外安排残留资源清理。

### 字段与选项

| 字段或选项 | 含义与影响 |
| --- | --- |
| Source configurations | 该集群作为保护源的配置数量 |
| Target references | 其他保护配置引用该集群作为目标的数量 |
| Restore points / Active tasks | 关联恢复点与活动任务数量，用于判断清理风险和阻塞条件 |
| Clean up related data（源集群） | 清理源保护配置、恢复点和备份数据，可能失去恢复能力 |
| Clean up related data（目标集群） | 保留其他源端的保护配置与备份数据，只清除它作为默认目标的引用 |
| Historical storage data detected | 检测到历史存储使用，正常注销会检查并清理该集群的存储前缀 |
| Force remove | 用于永久不可用场景，不等于完成过集群端的正常清理 |

### 结果确认

注销任务成功后，平台不再把这个集群当作可用的受管集群，相关保护引用按已确认的选项处理。集群运维侧要确认受管组件真的清理掉了、业务应用仍在；强制移除之后要单独登记并处理残留。

### 常见问题

- **注销不删业务 Namespace、工作负载和 PVC**，但勾选关联数据清理会删备份数据 —— 这两件事不要混为一谈；
- 注销不会删掉整个外部对象存储服务，但也不能因此认为里面的备份前缀一定保留；
- 注销失败时保留任务 ID 和第一个错误，先修权限、Agent 或存储，再重试正常注销。

**只有 Agent 或平台都已无法重连时**，才改用离线卸载脚本，见[卸载集群 Agent](/docs/manual/agent-uninstall)。

## 相关

- [对象存储](/docs/manual/storage)
- [Velero 恢复缓存](/docs/operations/restore-cache)：注册时自动配置的恢复缓存策略
- [卸载集群 Agent](/docs/manual/agent-uninstall)：Agent 失联时的兜底清理
