---
title: 运维
description: 平台启停、升级、蓝绿回滚、恢复缓存、诊断日志与故障排查。
---

# 运维

本板块面向长期负责平台运行的管理员，说明安装目录的内容、升级方式和故障排查入口。

## 平台构成

- 安装目录默认 `/var/lib/hypercdr`，保存 `.env`、TLS、Compose、持久化目录和版本清单。
- 平台与升级执行器都是 systemd 服务：`hypercdr.service`、`hypercdr-upgrade-runner.service`。
- 升级采用蓝绿方式切换 API 和前端，PostgreSQL 共享。
- 版本清单为 `current-release.json`，历史版本保存在 `releases/`。

## 本板块内容

<div class="hcdr-grid">
  <a class="hcdr-card" href="/docs/operations/platform"><b>平台运维与升级</b><span>启停脚本、服务状态、备份要点、升级链路和卸载。</span></a>
  <a class="hcdr-card" href="/docs/operations/blue-green"><b>蓝绿部署与回滚</b><span>切换机制，以及回滚可恢复与不可恢复的范围。</span></a>
  <a class="hcdr-card" href="/docs/operations/restore-cache"><b>Velero 恢复缓存</b><span>恢复阶段的缓存策略、阈值和预检失败的处理。</span></a>
  <a class="hcdr-card" href="/docs/operations/troubleshooting"><b>故障排查</b><span>按现象定位原因，附日志采集命令。</span></a>
  <a class="hcdr-card" href="/docs/operations/logging"><b>诊断日志</b><span>在控制台检索平台与集群日志，按 Task ID、Request ID 定位失败链路。</span></a>
  <a class="hcdr-card" href="/docs/operations/release"><b>构建与发布维护</b><span>发布工作流、版本清单与本地发布工具。</span></a>
</div>

## 运维约束

::: danger 升级前必须备份

升级会执行数据库迁移，而**流量回滚不会撤销已应用的 SQL 迁移**。升级前备份 PostgreSQL、`.env`（尤其 `HCDR_SECRET_KEY`）、TLS、Compose 配置和版本清单。

:::

::: warning 任务入队后仍需检查结果

平台升级任务入队后，由宿主机 upgrade runner 执行。检查执行器状态和最终结果，任务创建成功只表示任务已进入队列。

:::

::: warning 集群组件需单独升级

平台升级完成后，集群侧组件还需单独执行升级任务，并核对 Agent 上报的版本。

:::

## 日常巡检

巡检的目的不是「确认没报警」，而是及时发现有保护中断、恢复点过旧、组件离线、任务反复失败 —— 别等到真出事，才发现没有可用的恢复点。

### 六步巡检

1. **看整体。** 先在 Dashboard 看保护活动和告警，再到 **Configuration → Clusters** 看 Agent 是否 online、组件状态和最后上报时间。
2. **看存储。** 检查对象存储的连通性、容量和凭据有效期。别只测控制台到存储的网络 —— 备份和恢复组件同样要能访问仓库。
3. **看保护与恢复点。** 在 **DR → Application DR → Run DR** 看保护配置、同步状态和下一次调度；到恢复点列表核对最新可用时间。
4. **看失败任务。** 挑出失败和长时间没更新的任务，记下任务 ID、时间、集群和第一个错误事件，再按[诊断日志](/docs/operations/logging)往下查。
5. **做演练。** 按批准的计划在隔离环境执行 Drill，记录应用、数据和访问检查结果；演练结束后清理遗留资源。
6. **准备备份。** 平台维护前备份数据库、`.env`、秘密密钥、TLS 和版本清单，并验证备份确实可用 —— 没验证过的备份不算备份。

### 建议频率

| 频率 | 检查项 |
| --- | --- |
| 每日 | 集群 online 状态、失败任务、升级执行器服务状态 |
| 每周 | 存储容量增长、保留规则执行情况、恢复点数量 |
| 每月 | 执行一次演练、核对组件版本与清单 |
| 每次变更前 | 数据库与配置备份、回滚方案 |

### 记录什么

| 检查项 | 应记录的内容 |
| --- | --- |
| 集群与组件 | 在线状态、最后上报时间、组件版本及异常 |
| 应用保护 | 配置状态、策略、最近同步结果及下一次调度 |
| 恢复点 | 最新可用时间、源应用、仓库、是否满足业务要求 |
| 演练 | 所用恢复点、隔离目标、任务结果、业务验收及清理情况 |
| 异常处置 | 任务 ID、首个错误、影响范围、负责人和处理结果 |

### 怎样算巡检完成

各业务都有满足时间要求的可用恢复点；异常有明确的处置人和结论；演练结果与遗留风险可追踪。**没有告警不能代替恢复验证。**

::: warning 不要用发起备份或接管来「测试页面是否刷新」

误点会真的产生备份任务，接管更会改写目标环境。想看页面状态刷新就刷新页面。

:::
