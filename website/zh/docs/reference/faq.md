---
title: 常见问题
description: 按现象快速定位：Agent 离线、存储测试失败、没有恢复点、演练与接管失败、邮件发不出去。
---

# 常见问题

这里只回答一个问题：**看到这个现象，先查哪里**。具体操作在对应的页面上。

## 先按现象找

| 现象 | 先查什么 | 详细 |
| --- | --- | --- |
| Agent offline | 集群到平台的 WSS 出站、证书、Token、Agent Pod、网络策略 | [集群注册与管理](/docs/manual/clusters) |
| 存储测试失败 | endpoint、DNS/TLS、Bucket、区域、凭据权限、集群侧网络 | [对象存储](/docs/manual/storage) |
| 没有恢复点 | 保护配置是否生效、同步任务是否成功、是否被保留规则清掉 | [任务与恢复点](/docs/manual/tasks) |
| 演练失败 | 隔离 Namespace、目标权限、PVC/CSI、容量、资源冲突 | [容灾演练](/docs/manual/drill) |
| 接管或恢复失败 | 先看第一个失败阶段，再查目标 RBAC、Velero/OADP、对象存储 | [正式接管](/docs/manual/takeover) |
| 邮件发不出去 | SMTP 配置、测试状态、发件地址是否获授权 | [邮件服务](/docs/manual/email-service) |
| 登录进不去 | 验证码模式、证书、账号状态与组织归属 | [登录与个人安全设置](/docs/user-guide/users-and-organizations#personal-security) |
| 平台起不来 | 服务状态、`/readyz`、容器日志、磁盘空间 | [安装详解](/docs/deployment/install) |
| 找不到某段时间的日志 | 时间范围、租户、日志源筛选 | [诊断日志](/docs/operations/logging) |

## 排查的通用顺序

1. **先看任务状态和第一个错误事件。** 任务详情通常已经指出问题属于存储、PVC/StorageClass 还是资源映射。
2. **再按 Task ID 或 Request ID 查诊断日志。** 这一步能确认请求到底有没有到平台、失败在哪个组件。
3. **最后才看组件原始日志。** 直接翻 comm-agent、Velero 的输出，很容易被无关信息带偏。

跳过前两步直接看日志，通常只是多花时间。

## 几个常见误判

| 看到的 | 实际含义 |
| --- | --- |
| 任务状态成功 | 平台侧流程跑完了。业务能否恢复，要用 Drill 验证 |
| 保存成功 | 只写进了数据库。配置能不能用，要靠一次真实备份和恢复确认 |
| `/readyz` 返回 200 | 控制台能服务请求。对象存储、Agent 注册、恢复任务都还没验证 |
| 恢复点数量不涨 | 可能是任务失败，也可能是保留规则按配额清理了旧点 |
| Drill 任务 | 名字叫 Drill 不代表目标隔离，取决于你选的恢复模式和目标 |
| 事件里出现 `accepted` | Agent 只是收到了任务，还没开始恢复 |
| 事件里出现 `ContainersNotReady` | 某个时刻容器没通过就绪检查。后续变成 Ready 就属正常过程 |
| Agent 显示 online | 只说明连接在。备份是否可用还要看存储和任务 |
| 接管任务完成 | 平台侧完成。访问入口切换和业务验收仍要人做 |

## 下一步

上面这些问题如果都排除了，但仍定位不到原因：

- 收集材料：任务 ID、发生时间、集群名、第一个错误事件、相关截图；
- 用[诊断日志](/docs/operations/logging)导出对应时间段，必要时附上支持包；
- 分享前删掉 Token、密码、kubeconfig、密钥和内部地址。

## 相关

- [诊断日志](/docs/operations/logging)
- [故障排查](/docs/operations/troubleshooting)
- [术语表](/docs/reference/glossary)
