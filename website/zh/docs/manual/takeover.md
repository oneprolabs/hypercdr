---
title: 正式接管
description: 先看恢复就绪度评估的结论，再把受保护应用切换到目标集群，并完成接管后的业务验收。
---

# 正式接管

入口：**DR → Restore Points → Takeover**；就绪度结论在 **DR → Application DR** 的配置中查看。

接管把选定的恢复点恢复到目标集群，属于正式的业务切换，不是演练。执行前要通过就绪度评估和变更审批，执行后仍要完成访问入口切换和业务验收。

::: danger 接管会改写目标环境

接管会在目标集群创建或覆盖资源。选错恢复点或选错目标，可能直接影响目标端正在运行的业务。确认窗口里的每一个选项都要核对，不要用「先点一下看看」的方式试探。

:::

## 演练与接管的区别

| | 演练 Drill | 接管 Takeover |
| --- | --- | --- |
| 目的 | 验证恢复点能不能用 | 把业务真正切到目标端 |
| 目标环境 | 隔离的 Namespace | 生产目标集群 |
| 失败代价 | 只影响本次演练，改完可以重跑 | 已经动了目标环境，需要回退方案 |

任务列表里的 **Restore** 只是记录类型，页面没有独立的 Restore 创建按钮。可用的入口只有 Drill 和 Takeover，两者不能互相替代：一次成功的 Drill 不构成接管记录，也不能代替接管。

## 一、先看恢复就绪度

就绪度回答一个问题：这个源集群的恢复点，能不能恢复到这个目标集群。备份成功和可恢复是两个独立状态，备份做完不等于能恢复。

结论分四种：

| 结论 | 含义 | 能否执行 |
| --- | --- | --- |
| Ready | 所有检查项通过 | 可以 |
| Ready with warnings | 没有确定性阻断项，但有警告或未知项需要人工判断 | 可以，但要逐条看警告 |
| Blocked | 至少一个确定性要求缺失或不兼容 | 默认拦截 |
| Not assessed | 恢复对不可用，或还没有采集到证据 | 默认拦截，先跑评估 |

### 证据什么时候采集

能力扫描不是随集群清单定时上报的，而是在下面几个时机按需触发：

- 在 DR 配置里选择目标集群时；
- 保存保护配置时；
- 手动点击 **Run assessment** 时；
- 恢复流程需要较新证据时。

平台保留最近一次扫描结果及其采集时间，用于对比和审计。证据有保质期，时间长了自己点一次 **Run assessment**，比直接相信旧结论稳妥。

### 扫描看什么

```text
目标集群对外提供的 API 资源（精确到 group / version / resource）
被保护 Namespace 里实际存在的自定义资源类型
StorageClass 与 CSI 驱动
IngressClass、RuntimeClass、PriorityClass
VolumeSnapshotClass、cert-manager ClusterIssuer（对应 API 存在时）
```

![恢复就绪度评估详情中的检查项与证据](/images/user-guide/recovery-readiness.png)

### 证据不完整不等于通过

这一点最容易误判：**空列表是数据，不是证明**。

如果采集过程中出现权限不足、超时、API 发现失败，或集群上的 Agent 版本过旧，证据就是残缺的。残缺的证据最多只能给出 **Ready with warnings**，既不能证明兼容，也不能证明不兼容。老版本 Agent 不会报错，缺失的能力项会显示为 `Unknown` —— `Unknown` 必须当成没检查，不能当成检查通过。

### 评估没通过但确实要执行

存在确定性阻断项时，Drill 和 Takeover 默认被拦下。确实要承担风险继续，需要勾选明确的确认项：

> I understand the identified risks and want to proceed anyway.

不需要填写理由。任务载荷和任务事件会记录操作人、时间、当时的就绪度快照、阻断项数量和 `forceProceed=true`，事后可以追溯。勾选前请确认变更审批已经覆盖这些风险。

## 二、选择恢复模式

| 模式 | 页面上叫 | 说明 |
| --- | --- | --- |
| 克隆到新 Namespace | Clone to new namespace | 不复用目标端已有资源，冲突少，但访问入口和外部依赖仍要单独处理 |
| 恢复回原 Namespace | Full restore to original namespace | 目标端同名 Namespace 里的现有资源可能被覆盖，需要额外确认风险 |

选择原 Namespace 时，先把目标端现有资源列清楚，评估覆盖影响，再勾选风险确认框。

![正式接管窗口中的恢复点、目标与原 Namespace 风险确认](/images/user-guide/takeover-options.png)

图中是尚未提交的配置窗口局部，当前选中 **Full restore to original namespace**，风险确认框还没勾选。这个状态不代表已经执行接管，也不是推荐配置。

## 三、核对资源范围

**Restore scope** 默认包含 Namespace 级资源，**不包含集群级资源**，不要理解成「整个集群恢复」。

范围可以在默认和自定义之间切换。自定义只列当前 Namespace 里实际有对象的类型，具体规则见[应用容灾保护](/docs/manual/application-dr#resource-scope)。

## 四、核对环境映射

源集群和目标集群环境不同时，展开 **Advanced options** 逐项核对。名称相同不代表底层能力相同，这是接管最容易出问题的地方。

![接管高级配置中的 Service NodePort 映射](/images/user-guide/takeover-nodeports.png)

| 映射项 | 要核对什么 |
| --- | --- |
| StorageClass | 备份里的存储类要映射到目标端真实可用的存储类；同名不代表同一套后端 |
| 业务镜像 | 为源镜像指定目标镜像；留空则保留备份中的值，此时目标集群必须能拉到该镜像 |
| Service NodePort | 按 Service 和端口配置，有效范围 `30000–32767`；留空由 Kubernetes 自动分配，改完还要检查防火墙、负载均衡和访问地址 |

不要照抄截图里的 `31080`，也不要假定自动分配的端口和源端一致。

## 五、执行步骤

1. 进入 **DR → Restore Points**，核对当前源集群、Namespace 和恢复点时间，选中要接管的恢复点。
2. 点击 **Takeover**，打开 **DR Takeover**。再核对一次恢复点，避免用错应用或时间点。
3. 在 **Target cluster** 选目标集群。不可用或类型不兼容的集群不能作为有效目标。
4. 选恢复模式：克隆到新 Namespace，或恢复到目标端原 Namespace。
5. 在 **Restore scope** 核对默认或自定义资源范围。
6. 环境不同时展开 **Advanced options**，检查 StorageClass、业务镜像、NodePort 映射，以及工作负载就绪和应用验证选项。
7. 对照已批准的接管方案复核目标、范围和数据时间点，然后点 **Start Takeover**。**Cancel** 或直接关闭窗口都不会提交任务。
8. 到 **DR → Backup & Recovery Tasks** 看接管任务，在应用的 **Recovery Status** 里检查恢复阶段和最终结果。
9. 检查目标端的工作负载、PVC、Service 和业务数据，按业务方案切换访问入口并完成验收。

## 六、结果确认

任务完成只代表平台侧流程结束。接管是否成功，要同时满足：

- [ ] 目标工作负载、PVC、Service 已就绪
- [ ] 应用健康检查通过，能对外提供服务
- [ ] 业务数据与恢复点时间相符
- [ ] 访问入口、外部依赖（DNS、负载均衡、第三方服务）已按方案切换
- [ ] 源端写入已停止，或已明确两端并行的边界

记录恢复点、目标集群版本、开始和结束时间，以及遗留告警。

## 七、失败之后

- **先停手，再排查。** 发现目标冲突、权限错误或数据异常时，停止后续变更并保留现场，不要在同一个目标上反复重试。
- **失败不等于什么都没建。** 接管中断时，目标端可能已经创建了部分资源。重试前先核对已恢复的内容，不要直接清空原来的业务 Namespace。
- **不要只看百分比。** 流量切换完成前，不要仅凭进度数字宣布业务已恢复。

## 相关

- [应用容灾保护](/docs/manual/application-dr)：资源范围与目标集群的选择
- [容灾演练](/docs/manual/drill)：接管前先在隔离环境验证同一恢复点
- [任务与恢复点](/docs/manual/tasks)：任务状态与恢复点的对应关系
