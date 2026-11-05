---
title: 运维
description: 平台启停、升级、蓝绿回滚、恢复缓存与故障排查。
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

## 日常巡检建议

| 频率 | 检查项 |
| --- | --- |
| 每日 | 集群 online 状态、失败任务、升级执行器服务状态 |
| 每周 | 存储容量增长、保留规则执行情况、恢复点数量 |
| 每月 | 执行一次演练、核对组件版本与清单 |
| 每次变更前 | 数据库与配置备份、回滚方案 |
