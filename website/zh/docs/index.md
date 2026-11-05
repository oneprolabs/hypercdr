---
title: 快速开始
description: 在自有 Linux 主机上完成 HyperCDR 社区版安装：执行在线安装器、检查服务状态、登录控制台。
---

# 快速开始

HyperCDR 社区版部署在一台自有 Linux 主机上：在线安装器从 GitHub Release 下载已发布版本，控制台以 Docker Compose 运行，浏览器通过 `12443` 端口访问。

开始之前请确认以下四项：

- 一台可长期运行的 Linux 主机，具备 root 或 `sudo` 权限；
- 已安装 Docker Engine 与 Docker Compose v2（`docker compose`），安装器不会代为安装；
- 一个浏览器和所有受管集群都能访问的 HTTPS 地址；
- 安装主机能够访问 GitHub Release 与镜像仓库。

磁盘容量、基础工具、证书要求和全部安装参数见[安装 HyperCDR](/docs/getting-started/install)。

## 1. 执行安装

将示例域名替换为实际的 IP 或域名。该地址必须同时能被浏览器和集群 Agent 访问。

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

安装器会先输出解析到的实际版本、访问地址和安装目录，确认后才开始下载。出现 `Continue installation? [y/N]` 时，需要输入 `y` 并回车才会继续。

## 2. 检查服务状态

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
curl -kfsS -o /dev/null -w 'ready=%{http_code}\n' https://platform.example.com:12443/readyz
```

Edge、API、前端、PostgreSQL、注册执行器和升级执行器都应处于健康状态，`/readyz` 应返回 `200`。`/healthz` 通过只说明进程已启动；`/readyz` 通过也不代表对象存储和真实恢复任务已验证。

## 3. 登录并修改密码

在浏览器中打开 `https://platform.example.com:12443`，初始账号为 `admin`，初始密码为 `admin123`。**首次登录后请立即修改密码。**

登录验证方式可能是图片验证码或 Cloudflare Turnstile，取决于安装时的选择。实际生效的模式以安装目录下的 `.env` 为准。

## 接下来

<div class="hcdr-grid hcdr-grid-3">
  <a class="hcdr-card" href="/docs/getting-started/install"><b>安装 HyperCDR</b><span>前置条件、证书、访问地址、安装参数与验证的完整步骤。</span></a>
  <a class="hcdr-card" href="/docs/user-guide/first-use"><b>首次使用</b><span>注册集群、准备存储与策略、配置保护，直到完成一次隔离演练。</span></a>
  <a class="hcdr-card" href="/docs/operations/troubleshooting"><b>安装失败排查</b><span>按现象检查端口、证书、镜像拉取、Agent 连接和任务失败。</span></a>
</div>

安装成功只说明控制台能够启动。集群 Agent、对象存储权限和恢复链路都需要另行验证，安装平台本身也不会自动保护任何业务应用。

::: info 文档约定

- 平台命令默认在安装目录执行，默认路径为 `/var/lib/hypercdr`。
- 文档以仓库中的代码、脚本和 `env.example` 为准，示例值需替换为实际环境的值。
- 某个能力在代码里可用，不代表它在所有版本、驱动和拓扑上都验证过，正式环境请核对[支持与适配范围](/docs/reference/support-scope)。
- 请勿将密码、kubeconfig、私钥、证书或数据库备份写入文档、仓库或公开 issue。

:::
