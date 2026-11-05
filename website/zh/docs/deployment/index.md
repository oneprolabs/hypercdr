---
title: 部署规划
description: HyperCDR 社区版的部署形态、端口、容量与证书要求，以及部署前如何判断适配范围。
---

# 部署规划

社区版为单机部署：一台 Linux 主机运行 Docker Compose，控制台由 Edge、蓝绿 API 与前端、PostgreSQL、注册执行器和升级执行器组成，版本按蓝绿方式切换。Kubernetes 集群通过 Agent 出站接入，备份数据写入配置的对象存储。

安装步骤不在本页：[快速开始](/docs/) 提供三步最短路径，[安装 HyperCDR](/docs/getting-started/install) 列出完整前置条件和验证步骤。本页说明部署前的准备事项。

## 部署形态

```text
浏览器 ──HTTPS 12443──► Edge ──► 活动颜色的前端
                          └────► 活动颜色的 API ──► PostgreSQL
集群 Agent ──出站 WSS────► Edge
Agent / 集群组件 ────────► 对象存储
```

- API 与前端各有蓝、绿两套，升级时启动非活动颜色，健康检查通过后切换流量；
- PostgreSQL 是共享持久化服务，不随颜色切换；
- 注册执行器是内部服务，不发布宿主机端口；
- 产品文档站独立部署，不在本编排内。

## 端口与网络

| 端口 | 用途 | 谁来访问 |
| --- | --- | --- |
| `12443/tcp` | 控制台页面、API 与 WebSocket | 浏览器、集群 Agent |
| `12445/tcp` | 历史产品门户的兼容端口 | 仅历史安装包会用到；文档站不使用 |

数据库、API、前端、注册执行器和升级执行器都不直接发布宿主机端口。

| 方向 | 用途 |
| --- | --- |
| 安装主机 → GitHub Release | 获取入口脚本、安装包与校验文件 |
| 安装主机 → 镜像仓库 | 拉取 API、前端、注册执行器及依赖镜像 |
| 集群 Agent → 控制台 `12443/tcp` | 建立 HTTPS/WSS 出站连接 |
| Agent / 集群组件 → 对象存储 | 写入和读取备份数据 |
| 注册执行器 → Kubernetes API | 使用 Install for me 注册集群 |

## 部署前需确定的事项

| 事项 | 判断依据 |
| --- | --- |
| 访问地址 | 浏览器和所有受管集群都能解析并访问的 HTTPS Origin；IP 可用，正式环境建议域名 |
| 证书 | 安装器生成的证书不受浏览器信任；正式环境自备可信证书，且域名与访问地址一致 |
| 容量 | 按 PostgreSQL、日志和保留版本规划；备份数据落在对象存储，不占安装目录 |
| 集群类型 | Native Kubernetes、Huawei CCE、OpenShift 三条路径的组件镜像、注册检查与限制不同 |
| 对象存储 | S3 兼容；Bucket、凭据和集群侧网络需实测可达 |

::: tip 非必需组件

Nginx Proxy Manager 和本地 Harbor 不是安装前提。`charts/` 内的 Kubernetes 资产也不是当前在线安装路径的交付方式。

:::

## 适用性判断

| 事项 | 参考页面 |
| --- | --- |
| 发行版、存储驱动和拓扑支持情况 | [支持与适配范围](/docs/reference/support-scope) |
| 华为云 CCE 的差异 | [Huawei CCE](/docs/reference/cce) |
| OpenShift 与 OADP | [OpenShift 与 OADP](/docs/reference/openshift) |
| 已知限制与安全建议 | [限制与安全建议](/docs/reference/limitations) |

## 本板块内容

<div class="hcdr-grid">
  <a class="hcdr-card" href="/docs/getting-started/install"><b>安装 HyperCDR</b><span>前置条件、证书、安装命令、结果验证与首次登录。</span></a>
  <a class="hcdr-card" href="/docs/deployment/install"><b>安装详解</b><span>全部参数、非交互安装、离线包、升级回滚、卸载与故障定位。</span></a>
  <a class="hcdr-card" href="/docs/deployment/configuration"><b>配置参考</b><span>环境变量、验证码模式、注册配置与访问地址调整。</span></a>
</div>

## 安装完成后

安装完成不代表环境可用，建议按以下顺序完成：

1. [首次使用](/docs/user-guide/first-use)：注册集群、准备存储和策略、配置保护、核验备份与恢复。
2. [平台运维与升级](/docs/operations/platform)：启停脚本、升级链路和备份要点。
3. [故障排查](/docs/operations/troubleshooting)：常见故障的排查入口。

升级现有环境按蓝绿流程执行，由宿主机上的升级执行器完成。升级前必须备份数据库和运行配置，因为**数据库迁移不会随流量回滚自动撤销**。详见[蓝绿部署与回滚](/docs/operations/blue-green)。
