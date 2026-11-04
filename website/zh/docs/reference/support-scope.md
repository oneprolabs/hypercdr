---
title: 支持与适配范围
description: 区分代码入口与经过环境验收的能力，避免把实现当成支持承诺。
---

# 支持与适配范围

本页区分**代码入口**与**环境验收**。存在实现不意味着所有版本组合已经验证。

| 项目 | 当前入口与限制 |
| --- | --- |
| 独立主机中控 | GitHub Release 在线安装器，Docker Compose 蓝绿 API/前端，共享 PostgreSQL |
| 产品文档站 | 独立部署的静态文档站，与中控安装和发布流程不共用容器；中控默认 12443 |
| Kubernetes 中控 | 仓库有 Chart 资产；**不是**在线安装器或当前蓝绿生命周期的交付路径 |
| Native Kubernetes | 平台注册页面及生成的安装脚本，使用社区 Velero 路径 |
| Huawei CCE | 独立 provider 检查，见 [CCE 专项](/docs/reference/cce)；不能据此推断 CCE Autopilot 或所有节点架构可用 |
| OpenShift | OADP 专用组件与安装路径，见 [OpenShift 专项](/docs/reference/openshift)和逐项验证清单 |
| 对象存储 | UI/后端有多种存储配置；具体可用性取决于集群侧插件、网络、凭据与真实备份恢复验证 |
| 社区/企业扩展 | 当前仓库提供公开扩展边界；企业扩展设计不等同于社区版自带所有企业实现 |

::: danger 不要把这三样当成支持承诺

- 开发环境测试结果
- 专项 POC 清单
- 历史镜像地址

正式环境必须记录实际平台版本、组件 manifest、集群版本、节点架构、存储驱动和完整备份恢复结果。

:::

## 相关

- [Huawei CCE](/docs/reference/cce)
- [OpenShift 与 OADP](/docs/reference/openshift)
- [OpenShift POC 验证清单](/docs/reference/openshift-checklist)
- [中控架构](/docs/product/architecture)
