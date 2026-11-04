---
title: 集群注册与管理
description: 注册 Native Kubernetes、CCE 与 OpenShift 集群，检查在线状态，处理常见的注册问题。
---

# 集群注册与管理

入口：**Configuration → Clusters**。

## 注册前确认

| 检查项 | 说明 |
| --- | --- |
| 集群类型 | Native Kubernetes、Huawei CCE 或 OpenShift，决定安装哪套组件 |
| kubeconfig | 具备创建命名空间、CRD、工作负载等资源的权限 |
| 网络出口 | 集群节点能访问平台入口和镜像仓库 |
| 镜像可达 | 节点能拉取清单中指定的组件镜像 |

## 选择注册方式

注册页提供两种方式，产出的 Agent 相同，区别只在**由谁执行安装**：

| 方式 | 执行位置 | 适用条件 |
| --- | --- | --- |
| **Install for me** | 平台注册执行器 | 中控能通过网络访问 kubeconfig 中的集群 API |
| **Run command** | 一台能访问集群的主机 | 平台无法直连集群，或希望人工审阅安装命令 |

按页面第一步准备所需文件，再执行当前生成的命令。

::: danger 凭据安全

不要把访问 token 或 kubeconfig 分享到公开 issue，也不要提交到仓库。注册命令里包含访问凭据，分享前务必脱敏。

:::

## 注册成功的判断标准

安装完成后，回到集群列表确认：

- 集群状态为 **online**；
- 节点清单已上报，数量与实际一致；
- namespace 清单已上报；
- Agent 版本可读，与平台清单中的组件版本匹配。

只看到"注册完成"提示不算完成，清单上报才是 Agent 真正连通的证据。

## 集群类型的影响

| 类型 | 执行组件 | 注意点 |
| --- | --- | --- |
| Native Kubernetes | 社区 Velero | 依赖 CSI Snapshot 与存储插件能力 |
| Huawei CCE | 社区 Velero + CCE 专项 provider | 不能据此推断 CCE Autopilot 或所有节点架构可用，见 [CCE 专项](/docs/reference/cce) |
| OpenShift | OADP | 安装路径与验证清单独立，见 [OpenShift 与 OADP](/docs/reference/openshift) |

## 集群 offline 怎么办

按这个顺序排查：

1. Agent 的 Pod 是否在运行，日志里有没有反复重连；
2. 集群到平台 WSS 地址的网络是否通，证书是否被信任；
3. 注册凭据是否有效、平台侧是否有对应的集群记录；
4. 平台入口地址是否变更——已安装 Agent 的连接地址**不会**随平台改址自动迁移。

详细对照见[故障排查](/docs/operations/troubleshooting)。

## 组件升级

集群详情里，**Comm Agent** 与 **Velero/OADP** 组件分别升级；两者与平台升级是三件独立的事。

升级前确认：该集群当前没有运行中的任务、镜像仓库可达、目标版本与当前平台兼容，并且已经准备好回滚方式。升级完成后，核对 Agent 上报的组件版本和任务结果，不要只看任务状态。

::: warning 平台升级不覆盖组件升级

平台蓝绿升级只切换中控自身的镜像与流量；集群里的 Agent、Velero/OADP 仍需要单独发起升级任务并验收。

:::

## 下线集群

正常下线应从控制面发起解除注册，平台会先处理平台侧和对象存储的生命周期，再通知在线 Agent 清理 Kubernetes 资源。

**只有 Agent 或平台都无法重连时**，才使用离线卸载脚本，见[卸载集群 Agent](/docs/manual/agent-uninstall)。

## 相关

- [对象存储](/docs/manual/storage)
- [Velero 恢复缓存](/docs/operations/restore-cache)：注册时自动配置的恢复缓存策略
