---
title: 文档总览
description: HyperCDR 文档入口，按安装部署、用户指南、使用手册、运维和参考组织。
---

# HyperCDR 文档

HyperCDR 是面向 Kubernetes 与 OpenShift 的容器容灾平台。控制面统一管理集群、保护策略、恢复点、容灾演练、恢复任务和组件升级；集群 Agent 主动连接平台，协调 Velero 或 OpenShift OADP 执行实际动作。

本站在当前社区版 `ce-v0.0.8` 的代码和流程基础上编写。安装包与版本记录见 [GitHub Releases](https://github.com/oneprolabs/hypercdr/releases)。

## 按任务找入口

<div class="hcdr-grid">
  <a class="hcdr-card" href="/docs/deployment/"><b>我要部署平台</b><span>从环境准备、执行安装到配置调整，产出可登录的控制台。</span></a>
  <a class="hcdr-card" href="/docs/user-guide/"><b>我第一次使用</b><span>注册集群、准备存储、配置保护，直到完成一次恢复验证。</span></a>
  <a class="hcdr-card" href="/docs/manual/"><b>我要查某个功能</b><span>按模块拆开的操作说明：集群、存储、策略、容灾、任务、演练。</span></a>
  <a class="hcdr-card" href="/docs/operations/"><b>我要维护平台</b><span>启停、升级、蓝绿回滚、恢复缓存和故障排查。</span></a>
</div>

## 文档地图

| 板块 | 面向 | 主要内容 |
| --- | --- | --- |
| [产品概览](/docs/product/) | 技术评估 | 平台定位、中控架构、核心概念之间的关系 |
| [安装部署](/docs/deployment/) | 部署实施 | 环境前置条件、在线安装、配置项、访问地址调整 |
| [用户指南](/docs/user-guide/) | 首次使用 | 从集群注册到恢复验证的完整路径，账号与组织权限 |
| [使用手册](/docs/manual/) | 日常操作 | 仪表盘、集群、存储、策略、应用容灾、任务、演练、Agent 卸载 |
| [运维](/docs/operations/) | 平台运维 | 平台运维与升级、蓝绿部署、恢复缓存、故障排查、发布维护 |
| [参考](/docs/reference/) | 所有人 | 术语表、支持与适配范围、限制与安全建议、CCE 与 OpenShift 专项 |

## 阅读约定

- 平台相关命令默认在安装目录执行，默认路径为 `/var/lib/hypercdr`。
- 文档以仓库中的代码、脚本和 `env.example` 为准；示例值需要替换为实际环境。
- 代码中存在的能力不等于所有版本、驱动和拓扑都已通过验证，正式环境请核对[支持与适配范围](/docs/reference/support-scope)。
- 不要把密码、kubeconfig、私钥、证书或数据库备份写入文档、仓库或公开 issue。

::: tip 中文与英文

中文文档目前完整覆盖安装、使用、运维和参考。英文正文正在按板块补齐，可先通过语言切换查看英文入口说明。
:::
