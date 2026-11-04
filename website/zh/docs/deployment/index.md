---
title: 部署指南
description: HyperCDR 社区版的部署路径、环境要求与安装后的下一步。
---

# 部署指南

社区版通过在线安装器部署到一台自有 Linux 主机，控制面以 Docker Compose 运行，采用蓝绿方式切换 API 和前端版本，PostgreSQL 作为共享持久化服务。

## 部署路径

```text
准备 Linux 主机 → 安装 Docker/Compose 与基础工具 → 开放 12443（可选 12445）
→ 执行在线安装器 → 检查服务与健康状态 → 首次登录并修改密码 → 注册测试集群
```

## 你需要准备

| 项目 | 说明 |
| --- | --- |
| 主机 | 一台可长期运行的 Linux 主机，root 或 sudo 权限 |
| 容器环境 | Docker Engine 与 Docker Compose v2，安装器不会代装 |
| 基础工具 | Bash、curl、tar、OpenSSL；升级执行器另需 `flock`；`jq` 只在本地发布/打包时需要 |
| 地址 | 浏览器和集群都能访问的域名或 IP，以及可用的 TLS 证书策略 |
| 网络 | 能访问 GitHub Release（`latest.json` 与安装包）和镜像仓库 |
| 集群 | 至少一个可注册的 Kubernetes、CCE 或 OpenShift 集群，用于验证 |

::: tip 不需要的东西

Nginx Proxy Manager 和本地 Harbor 不是安装前提。`charts/` 里的 Kubernetes 资产也不是当前在线安装路径的交付方式。

:::

## 本板块内容

<div class="hcdr-grid">
  <a class="hcdr-card" href="/docs/deployment/install"><b>安装社区版</b><span>前置条件、安装命令、参数说明和安装后的验收步骤。</span></a>
  <a class="hcdr-card" href="/docs/deployment/configuration"><b>配置参考</b><span>环境变量、验证码模式、注册配置和访问地址调整。</span></a>
  <a class="hcdr-card" href="/docs/reference/support-scope"><b>部署与适配范围</b><span>哪些入口和组合已经验证，哪些只是代码里存在。</span></a>
</div>

## 安装之后

装完不等于可用。建议按顺序完成：

1. [首次使用](/docs/user-guide/first-use)：注册集群、准备存储和策略、配置保护、核验备份与恢复。
2. [平台运维与升级](/docs/operations/platform)：了解启停脚本、升级链路和备份要点。
3. [故障排查](/docs/operations/troubleshooting)：出问题时先看这张对照表。

## 如果是升级现有环境

平台升级走蓝绿流程，由宿主机升级执行器执行。升级前必须备份数据库和运行配置，因为**数据库迁移不会随流量回滚自动撤销**。详见[蓝绿部署与回滚](/docs/operations/blue-green)。
