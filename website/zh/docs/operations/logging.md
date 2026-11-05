---
title: 诊断日志
description: 在控制台检索平台与集群的诊断日志，定位失败的请求、任务和组件，并导出交给支持人员。
---

# 诊断日志

入口：**Operations → Diagnostic Logs**。

平台把诊断信息按结构化索引单独存了一份，用于在页面里安全检索。它不替代容器原始日志 —— 容器日志由 Docker 和 Kubernetes 自己保存、自己轮转；诊断索引回答的是另一个问题：哪一步失败了，最早的错误是什么。

## 两种日志源

| 日志源 | 覆盖范围 |
| --- | --- |
| Platform | 平台自身的 API 请求、任务生命周期、升级执行器 |
| Cluster | 托管集群里的 comm-agent、Velero、node-agent |

集群日志是**按需采集**的：平台通过已认证的 Agent 连接去取，浏览器不能直接指定 Namespace、Pod 或容器。所以要先选集群和组件，才能搜索。

## 筛选条件

| 条件 | 说明 |
| --- | --- |
| Log Scope | Tenant and system、Tenant logs 或 System logs；后两项只有系统管理员能选 |
| Tenant | 系统管理员可选全部租户或某个租户，普通账号固定为自己的租户 |
| Cluster | 日志源选 Cluster 时必填 |
| Level | Info、Warning、Error、Debug |
| Component | Platform 侧有 Platform API、Platform Upgrader、Task Lifecycle；Cluster 侧有 comm-agent、Velero、node-agent |
| From / To | 时间范围，查询必须有边界 |
| Search | 匹配消息内容、Task ID、Request ID |

## 检索步骤

1. 选择日志源。
2. 按需设置租户、集群、等级、组件和时间范围。
3. 在搜索框输入关键字，回车或点 **Search**。
4. 点某一行打开 **Log Details**，查看 Correlation 与 Diagnostic Context。

Correlation 区里有 Level、Operation、Status、Task ID、Command ID、Request ID、Error Code。报障时把 Task ID 和 Request ID 一起给出，对方就能定位到同一条链路。

## 保留与上限

- 结构化诊断索引默认保留 **14 天**；
- 单次查询最多返回 **5000 条**。返回不全时缩小时间范围，不要误以为没有数据；
- Docker 原始日志按服务轮转（单文件 50 MB，每个服务保留 5 个文件），Kubernetes 原始日志的保留由 kubelet 和容器运行时负责。

## 权限边界

::: warning 租户之间看不到彼此的日志

普通账号只能查自己租户和自己集群的日志，导出同样受这个限制。系统日志不带租户 ID，只有系统管理员可见。

:::

## 不会记录的内容

密码、bearer token、Agent 凭据、对象存储密钥、SMTP 密码和 Kubernetes Secret 内容都不入库，诊断字段会递归脱敏。但这不代表可以随手贴日志：地址、集群名、业务对象名仍会出现在消息里，对外分享前照旧要过一遍。

## 什么时候用它

- 任务失败，但任务详情只给了一句概括；
- 想确认某个请求是否真的到了平台 —— 按 Request ID 反查；
- 集群侧组件异常，需要看 comm-agent、Velero 或 node-agent 的原话；
- 需要把一段时间的错误导出，交给支持人员。

## 相关

- [故障排查](/docs/operations/troubleshooting)：按现象定位问题
- [任务与恢复点](/docs/manual/tasks)：任务状态与失败类型
- [平台运维与升级](/docs/operations/platform)：控制台自身的升级与维护
