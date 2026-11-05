---
title: 任务与恢复点
description: 查看备份与恢复任务的状态、排查失败原因，并在 Restore Points 中核对可用的恢复点。
---

# 任务与恢复点

入口：**Backup & Recovery Tasks**（执行记录）和 **Restore Points**（可恢复时间点）。

## 任务与恢复点的关系

```text
保护策略调度 → 产生任务 → 任务成功 → 生成恢复点
                                ↘ 任务失败 → 无恢复点（或部分失败）
```

任务记录单次执行的结果，恢复点表示可恢复的时间点，两者不能互相替代。

## 任务字段的用途

| 字段 | 用途 |
| --- | --- |
| 状态 | 区分成功、失败和进行中；失败任务需要查看具体阶段 |
| 时间 | 与策略调度时间对比，判断是否延迟 |
| 详情 | 错误信息与执行阶段，用于定位存储、Agent 或资源映射问题 |
| 日志 | 具体错误堆栈，用于深入排查 |

::: tip 先查任务详情，再看 Agent 日志

任务详情通常已指出问题属于存储访问、PVC/StorageClass 还是资源映射。直接查看 Agent 日志容易被无关输出干扰。

:::

## 任务进度与事件 {#task-events}

任务详情里有两类信息：**进度**（百分比、耗时、起止时间）和**事件时间线**（每一步发生了什么）。排查时以后者为准，百分比只是概览。

![任务详情中的进度、耗时与起止时间](/images/user-guide/task-summary.png)

图中的进度来自一个已经完成的任务，不能当作实时进度示例。三个容易看错的地方：

- **Cluster 是实际执行任务的集群**，可能和你在页面上选中的源集群不是同一个，跨集群恢复时尤其要注意；
- **Started / Completed 按你选择的时区显示**，与集群节点的本地时间不一定一致；
- 记录问题时把任务 ID、类型和时间一起留下，不要把不同任务的事件拼在一起看。

### 事件怎么读

![已完成 Drill 任务的事件时间线](/images/user-guide/drill-events.png)

一次恢复类任务的事件大致按下面的顺序出现：

```text
存储预检查 → 下发到 Agent → Agent 接受 → 等待恢复 → 工作负载就绪检查 → 完成
```

| 事件 | 含义 | 能否当作成功依据 |
| --- | --- | --- |
| `accepted` | Agent 已接受任务，还没开始恢复 | 不能 |
| `workload readiness changed` | 应用就绪检查发生变化 | 不能，要结合 Pod 和副本数判断 |
| `completed` | 该任务报告资源恢复完成且应用就绪 | 只能证明这一个恢复点、这一个目标 |

历史事件不会因为任务最终成功而消失。曾经出现 `ContainersNotReady`、随后变成 Ready，属于正常过程。判断结果要看后续事件和最终结果，不能凭一条旧的等待消息就认定任务失败。

### 四种详情页各看什么

| 入口 | 看什么 |
| --- | --- |
| **Recovery Details**（DR → Application DR → Recovery Status） | 恢复阶段的完整时间线；展开工作负载事件能看到 Pod、节点和等待原因 |
| **Sync Details**（同一行的 Sync Status） | 同步阶段、最新恢复点和下一次调度 |
| **DR Configuration Details** | 存储准备相关的失败与重试 |
| **Task Details → Task events** | 平台、Agent、Kubernetes 和后端事件 |

::: tip 任务事件不是组件原始日志

这里的事件只到「哪一步失败、报了什么」。需要资源级排障时，去看[诊断日志](/docs/operations/logging)里的 comm-agent、Velero 或 node-agent 输出。`Technical payload` 是技术数据，不要直接当成操作步骤，也别未经脱敏就贴出去。

:::

## 常见失败类型

| 错误特征 | 含义 | 处理方向 |
| --- | --- | --- |
| 存储访问被拒 | 凭据失效、Bucket 权限不足、endpoint 不可达 | [对象存储](/docs/manual/storage) |
| PVC / StorageClass 相关 | 目标集群缺少对应存储类，或动态供给失败 | 核对目标集群存储能力 |
| 资源映射冲突 | 目标 Namespace 已有同名资源 | 调整恢复目标或映射 |
| `RESTORE_CACHE_PREFLIGHT_FAILED` | 演练前恢复缓存预检未通过 | 见 [Velero 恢复缓存](/docs/operations/restore-cache) |
| Agent 超时 | 集群侧网络或负载问题 | 查看集群状态与 Agent 日志 |

## 恢复点

在 **Restore Points** 中确认：

- 恢复点的时间与任务记录对应；
- 恢复点关联的存储与应用正确；
- 保留规则没有将其清理。

恢复点数量不增长时，先确认是**任务失败**还是**保留规则清理**。

![恢复点列表的时间、仓库和容量信息](/images/user-guide/restore-points.png)

| 列 | 用法 |
| --- | --- |
| Namespace | 恢复点所属应用的 Namespace，恢复前必须和保护对象核对 |
| Restore Point | 显示名称，时间按你选择的时区展示 |
| Repository | 备份所在的存储仓库，恢复时仍需可访问 |
| Original Size | 页面已获取的原始数据大小，不等于对象存储的实际占用 |
| Storage Size | 增量等信息拿不到时显示 `Unknown`；`Unknown` 不等于 0，也不能据此判断备份失败 |
| Recovery Task | 关联的恢复任务及状态；没有恢复任务不代表恢复点不可用 |

截图为列表局部，右侧列需要横向滚动查看。选恢复点前先核对源集群、Namespace、时间和仓库，不要只看最近一条就下判断。

## 删除恢复点

入口：**DR → Restore Points → More → Delete Restore Points**。

删除会向源集群发起 Velero 备份删除请求，不只是把平台里的一行记录隐藏掉。删掉之后，对应时间点就无法再恢复。

### 什么时候可以删

- 保留策略、审计要求和业务恢复要求已确认，删除已获授权；
- 源集群 Agent 和存储可访问；
- 这个恢复点不再承担当前的恢复或演练需要。

### 操作

1. 进入 **DR → Restore Points**，核对当前源集群，选中要删除的恢复点。
2. 打开 **More → Delete Restore Points**，在确认窗口核对所选数量、恢复点名称、备份标识和时间。
3. 不准备删除就点 **Cancel**。确认已授权且范围正确后，才点 **Delete from Velero**。
4. 看到 **Velero deletion is running** 说明还在执行，等结果，不要反复点击。
5. 检查任务结果并刷新列表，确认预期恢复点已清理。失败时先保留错误信息和任务标识再排查。

![恢复点删除确认窗口](/images/user-guide/restore-point-delete.png)

### 确认窗口里的字段

| 信息 | 含义 |
| --- | --- |
| Selected Restore Points | 本次选中的恢复点，批量删除前逐一核对 |
| Velero 备份标识 | 源集群关联备份的身份，用于核对和排障。标识较长时列表会截断显示，要结合恢复点时间一起核对，不能只看开头几位 |
| Delete from Velero | 真正提交删除请求，不是清除筛选或取消选择 |
| 提交 / 执行 / 完成状态 | 删除是异步的，发起成功不等于备份数据已经清理 |

::: danger 不要用客户端清理代替平台删除

删除失败时，不要手工删对象存储里的 Bucket 内容来「代替」清理。这样平台记录和备份数据会对不上，后续的排查、演练和审计都会失真。

:::

## 执行恢复之前

::: danger 恢复会覆盖目标

执行恢复之前必须核对**目标与资源映射**，避免覆盖现有业务。恢复操作会改写目标环境。

:::

建议顺序：

1. 先在[演练](/docs/manual/drill)中验证同一恢复点可用；
2. 确认目标集群、目标 Namespace、存储类映射；
3. 确认目标侧同名资源允许被覆盖；
4. 执行恢复，并跟踪任务进度；
5. 恢复后校验工作负载、PVC 和应用数据。

## 恢复后的校验项

任务成功不代表应用可用。至少检查：

- [ ] 工作负载（Deployment/StatefulSet 等）已就绪，副本数符合预期
- [ ] PVC 已绑定，数据可读
- [ ] 应用能对外提供服务
- [ ] 数据内容与恢复点时间相符

## 相关

- [容灾演练](/docs/manual/drill)
- [正式接管](/docs/manual/takeover)
- [故障排查](/docs/operations/troubleshooting)
