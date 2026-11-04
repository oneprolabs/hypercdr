---
title: 第一次恢复验证
description: 从注册集群到 Drill 验证：用测试 Namespace 走完 HyperCDR 的保护、恢复点与恢复验证全流程。
---

# 第一次恢复验证

装完控制台只是拿到了操作面。真正要确认的是「保护 → 恢复点 → 恢复」这条链路能跑通。

请先用测试 Namespace 走完整流程，再保护业务。整条路径不需要额外组件，但需要一个可注册的集群、一个可写的对象存储，以及一次真实的备份与恢复。

## 1. 注册集群

进入 **Configuration → Clusters**，选择注册方式与集群类型（Native Kubernetes、Huawei CCE 或 OpenShift），按页面要求准备 kubeconfig。

- **Install for me**：中控注册执行器能访问 kubeconfig 里的集群 API 时使用，由平台代为注册。
- **Run command**：执行命令的主机具备集群网络与管理权限时使用，在本地执行平台生成的命令。

按实际发行版选择类型，不要凭集群名字猜。注册完成后确认集群显示 **online**，节点与 Namespace 清单已上报。上传的 kubeconfig 必须内嵌凭据，最大 1 MiB，不能依赖外部 exec 插件。

## 2. 准备存储与策略

在 **Configuration → Storage** 配置对象存储地址、Bucket 和凭据，确认集群侧能访问该存储；在 **Configuration → Policies** 设置保护计划、恢复点配额与保留规则。

也可以在保护向导里顺带创建这两类资源——向导与管理页复用同一套创建界面，创建后返回并选中新增记录即可。

资源保存成功不代表链路可用。存储权限、网络和凭据只有跑过一次真实备份才算验证过。

## 3. 配置应用保护

进入 **DR → Application DR**。三个页签分别对应可保护的源应用、保护配置和目标侧恢复状态。

选择源 Namespace 后启动保护向导，依次确定范围、目标集群、对象存储和策略。多个应用需要不同恢复范围时分别建保护配置，不要因为它们在同一个 Namespace 就当成一件事。

## 4. 运行保护并检查恢复点

触发或等待策略任务，在 **DR → Backup & Recovery Tasks** 查看进度、阶段和事件；成功后到 **DR → Restore Points** 确认恢复点状态与内容清单。

判断标准是恢复点可用，不是列表上的成功标签：

- 恢复点不能处于创建中；
- 源集群与保护配置要能对应上，已删除的保护配置不会给出可恢复的恢复点；
- 对象存储必须当前可访问。

恢复点处于创建中、来源不匹配、保护配置已删除或仓库不可访问时，不要启动恢复。

## 5. 用 Drill 验证恢复

在 **DR → Restore Points** 选中一个恢复点，用 **Drill** 在隔离目标里验证恢复。Drill 用来验证恢复流程和恢复点可用性，不是生产接管。

演练要检查工作负载、PVC、Service 和应用数据，而不只是任务状态；目标 Namespace 与网络必须和生产恢复计划隔离。操作名称不保证目标自动隔离，隔离范围要自己规划。

需要正式接管时走 **Takeover**，它属于容灾切换流程，必须先通过校验和变更审批。当前已核验的恢复点操作入口就是 Drill 和 Takeover，没有独立的 Restore 创建按钮。

::: tip Drill 与 Takeover 的区别

Drill 失败只是演练失败，改完再跑；Takeover 一旦执行就是业务切换。先用 Drill 把不确定性消耗掉，再考虑接管。

:::

## 验收清单

| 检查项 | 通过标准 |
| --- | --- |
| 集群接入 | Agent online，节点与 Namespace 清单已上报 |
| 对象存储 | 连接测试通过，且至少一次真实备份写入成功 |
| 保护配置 | 源 Namespace、目标、仓库、策略四项齐备 |
| 恢复点 | 状态可用，时间与保留策略符合预期 |
| 演练 | 隔离目标里的工作负载、PVC 和应用数据均通过检查 |
| 任务记录 | 阶段、事件与日志能解释本次结果，不只看成功标签 |

## 下一步

<div class="hcdr-grid hcdr-grid-3">
  <a class="hcdr-card" href="/docs/manual/clusters"><b>集群与资源</b><span>注册细节、Agent 状态、组件版本与日志。</span></a>
  <a class="hcdr-card" href="/docs/manual/application-dr"><b>应用容灾</b><span>保护范围、目标映射、Hooks 与恢复操作。</span></a>
  <a class="hcdr-card" href="/docs/manual/drill"><b>容灾演练</b><span>演练目标规划、隔离范围与结果核对。</span></a>
</div>
