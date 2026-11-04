---
title: 快速开始
description: 在自有 Linux 主机上部署 HyperCDR 社区版：执行在线安装器、检查服务状态、登录控制台，然后进入第一次恢复验证。
---

# 快速开始

HyperCDR 社区版装在一台自有 Linux 主机上：在线安装器从 GitHub Release 下载已发布版本，控制面以 Docker Compose 运行，浏览器通过 `12443` 访问。

当前版本 `ce-v0.0.13`。本文只覆盖从零到能登录控制台的最短路径；参数、离线安装、升级和卸载见[安装详解](/docs/getting-started/install)。

## 开始之前

| 项目 | 要求 |
| --- | --- |
| 主机 | 一台可长期运行的 Linux 主机，具备 root 或 `sudo` 权限 |
| 容器环境 | Docker Engine 与 Docker Compose v2（`docker compose`）；**安装器不会替你安装** |
| 基础工具 | Bash、curl、tar、OpenSSL；升级执行器另需 `flock`，版本清单校验需要 `jq` |
| 访问地址 | 浏览器和所有受管集群都能访问的 HTTPS Origin，例如 `https://platform.example.com:12443` |
| 网络 | 安装主机能访问 GitHub Release 与镜像仓库；集群 Agent 能出站访问中控 `12443/tcp` |
| 集群 | 至少一个可注册的 Kubernetes、CCE 或 OpenShift 集群，用来验证保护链路 |

::: tip 安装器不管的事

Docker、DNS、防火墙、公开证书、外部反向代理、对象存储和集群网络策略都需要你提前准备。安装器生成的 TLS 证书只用于把 HTTPS 跑起来，不等同于浏览器信任的公开证书。

:::

## 三步装起来

### 1. 执行安装

把示例域名换成实际的 IP 或域名。该地址必须同时被浏览器和集群 Agent 访问到。

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

安装器会先打印解析到的实际版本、访问地址和安装目录，再次确认后才开始下载。交互安装出现 `Continue installation? [y/N]` 时，要输入 `y` 回车才继续。

### 2. 检查服务状态

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
curl -kfsS -o /dev/null -w 'ready=%{http_code}\n' https://platform.example.com:12443/readyz
```

Edge、API、前端、PostgreSQL、注册执行器和升级执行器都应处于健康状态，`/readyz` 应返回 `200`。`/healthz` 通过只说明进程起来了，`/readyz` 通过也不代表对象存储和真实恢复任务已经验证过。

### 3. 登录并改密

浏览器打开 `https://platform.example.com:12443`，初始账号 `admin`，初始密码 `admin123`。**首次登录后立即修改密码。**

登录挑战可能是图片验证码或 Cloudflare Turnstile，取决于安装时的选择，实际模式以安装目录 `.env` 为准。

## 接下来

<div class="hcdr-grid hcdr-grid-3">
  <a class="hcdr-card" href="/docs/getting-started/install"><b>安装详解</b><span>安装参数、固定版本、非交互安装、离线包与安装产物清单。</span></a>
  <a class="hcdr-card" href="/docs/getting-started/first-recovery"><b>第一次恢复验证</b><span>注册集群、准备存储与策略、配置保护，直到完成一次隔离演练。</span></a>
  <a class="hcdr-card" href="/docs/operations/troubleshooting"><b>装不上怎么办</b><span>按现象对照检查端口、证书、镜像拉取、Agent 连接和任务失败。</span></a>
</div>

装完不等于可用。安装成功只代表控制面能启动，集群 Agent、对象存储权限和恢复链路要另外验证；平台安装也不会自动保护任何业务应用。

::: info 阅读约定

- 平台命令默认在安装目录执行，默认路径 `/var/lib/hypercdr`。
- 文档以仓库中的代码、脚本和 `env.example` 为准，示例值需要替换为实际环境。
- 代码里存在的能力不等于所有版本、驱动和拓扑都已验证，正式环境请核对[支持与适配范围](/docs/reference/support-scope)。
- 不要把密码、kubeconfig、私钥、证书或数据库备份写入文档、仓库或公开 issue。

:::
