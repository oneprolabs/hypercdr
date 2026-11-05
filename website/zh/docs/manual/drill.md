---
title: 容灾演练
description: 在隔离环境中验证恢复点可用性，包括恢复缓存预检与校验要点。
---

# 容灾演练

入口：**Drill**。

演练用于**在不影响生产的前提下验证恢复点能否恢复为可运行的应用**。

任务成功只说明备份流程已经结束，业务能否在恢复后运行由演练确认。

## 演练与恢复的区别

| | 演练 | 恢复 |
| --- | --- | --- |
| 目的 | 验证能力 | 处理真实故障 |
| 范围 | 隔离环境 | 业务目标环境 |
| 风险 | 低，可反复执行 | 高，可能覆盖现有资源 |
| 频率 | 定期 | 按需 |

## 演练前

- [ ] 集群 online，Agent 正常
- [ ] 存在可用的恢复点
- [ ] 目标范围已隔离，不会涉及生产资源
- [ ] 恢复缓存预检通过（见下文）

## 选择演练目标 {#target}

![Drill 向导中的目标集群与恢复模式](/images/user-guide/drill-target.png)

向导按 **Restore Point → Target → Restore Options → Review** 四步走。

| 选项 | 说明与风险 |
| --- | --- |
| Target cluster | 实际执行恢复的目标集群，不能只看源集群是否在线 |
| Clone to new namespace | 恢复到新 Namespace；隔离演练优先用这个，并确认该 Namespace 没有被生产使用 |
| Full restore to original namespace | 恢复到目标集群的同名 Namespace，可能和现有资源冲突，不该作为隔离演练的默认选择 |
| Target namespace | 克隆模式下的目标命名空间；示例里的 `demo-nextcloud-drill` 只是表单当前值 |

Restore Point 页要确认状态为 Available，并核对源集群、Namespace 和备份时间。

::: danger Drill 这个名字不保证隔离

叫 Drill 不代表目标自动隔离。提交前必须核对恢复模式、目标集群和 Namespace。选成原 Namespace 时，演练会直接影响目标端已有的工作负载。

:::

进入向导、点 **Next** 都不代表已经提交任务，**Review** 之后才真正下发。

## 恢复缓存预检

演练提交恢复之前，Agent 会读取实际 ConfigMap 和 StorageClass 执行预检：

| 预检结果 | 行为 |
| --- | --- |
| 未启用缓存 | 记录使用临时磁盘，继续 |
| 缓存配置有效 | 记录 StorageClass、阈值和缓存上限，继续 |
| 已启用但配置不可用 | 在创建 Restore 之前以 `RESTORE_CACHE_PREFLIGHT_FAILED` 返回 |

预检失败不能忽略：Velero 在运行时创建缓存 PVC 失败**不会**自动回退到临时盘。

预检可能失败的情形：StorageClass 缺失、没有动态 provisioner、`reclaimPolicy` 不是 `Delete`、ConfigMap 无法解析。详见[Velero 恢复缓存](/docs/operations/restore-cache)。

## 演练的校验项

不要只看任务状态标签。按以下顺序检查：

1. **工作负载**：Deployment/StatefulSet 是否就绪，副本数、镜像版本是否符合预期。
2. **PVC**：是否绑定成功，容量和 StorageClass 正确。
3. **应用数据**：连接应用检查数据，抽查关键记录或文件。
4. **对外服务**：Service/Ingress 是否可用（在隔离范围内验证）。

::: warning 部分成功也属于失败

Velero 的恢复可能出现「部分完成」：大部分资源已恢复，少数资源失败。任务状态可能显示正常，因此需要逐项核对资源清单。

:::

## 演练结果的处理

| 结果 | 含义 | 下一步 |
| --- | --- | --- |
| 全部通过 | 恢复能力已验证 | 记录时间点，按周期复演 |
| 工作负载异常 | 镜像、配置或依赖缺失 | 补齐依赖，重新演练 |
| 数据不完整 | 备份范围或一致性有问题 | 检查保护范围与存储 |
| 预检失败 | 集群存储能力不满足 | 调整 StorageClass 或缓存配置 |

记录每次演练的时间、恢复点和结果。发生故障时，这份记录可作为恢复能力的依据。

## 相关

- [任务与恢复点](/docs/manual/tasks)
- [Velero 恢复缓存](/docs/operations/restore-cache)
- [限制与安全建议](/docs/reference/limitations)
