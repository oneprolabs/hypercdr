---
title: 安装 HyperCDR
description: HyperCDR 社区版安装的前置条件、访问地址与证书、在线安装命令、结果验证和首次登录。
---

# 安装 HyperCDR

社区版部署在自有 Linux 主机上。在线安装器下载并部署选定的 Release 版本，控制台以 Docker Compose 运行，蓝绿方式切换 API 与前端，PostgreSQL 作为共享持久化服务。

如果只需要先完成安装，[快速开始](/docs/) 提供了三步最短路径。本页说明安装前需要确认的条件；安装参数、非交互安装、离线包、升级回滚与卸载见[安装详解](/docs/deployment/install)。

## 安装前准备

| 项目 | 要求 |
| --- | --- |
| 主机 | 一台可长期运行的 Linux 主机，具备 root 或 `sudo` |
| 容器环境 | Docker Engine 与 Docker Compose v2（`docker compose`），daemon 正常运行 |
| 基础工具 | Bash、curl、tar、OpenSSL；部署锁需要 `flock`（通常来自 util-linux），版本清单校验需要 `jq` |
| 访问地址 | 浏览器与所有受管集群都能访问的 HTTPS Origin，默认入口端口 `12443` |
| 磁盘与容量 | 按数据库、日志和保留版本规划；应用备份数据写入你配置的对象存储，不占安装目录 |
| 网络出站 | 安装主机 → GitHub Release 与镜像仓库；集群 Agent → 控制台 `12443/tcp`；Agent/集群组件 → 对象存储 |
| 端口 | `12443/tcp`（控制台与 API） |

::: warning 安装器不负责的内容

安装器不会安装 Docker，也不会安装 Harbor、Nginx Proxy Manager 或外部对象存储。命令存在不代表服务可用，Docker daemon、镜像拉取和端口连通都需要实际验证一次。

:::

### 关于 Docker

- **复用现有环境**：主机已有符合要求的 Docker Engine 与 Compose v2 时直接复用，安装器不做替换。
- **不做修复**：安装器不升级、不修复也不卸载已有的 Docker；不符合要求时请先手动处理。
- **卸载范围**：卸载 HyperCDR 不会移除 Docker、Compose、containerd，也不会删除业务集群里的 Agent 和对象存储中的备份数据。

### 地址与证书

`--base-url` 必须是浏览器和集群 Agent 都能访问的 HTTPS Origin：

```text
https://platform.example.com:12443
```

可以使用 IP，但正式环境建议用 DNS 名称加受信任证书。安装器生成的 TLS 证书**不会**被浏览器自动信任，证书名称、访问地址和反向代理配置必须一致。

::: info 两种注册路径的额外要求

**Install for me** 还要求控制台注册执行器能访问 kubeconfig 中登记的集群 API。**Run command** 要求执行命令的主机具备相应的网络与 Kubernetes 管理权限。

:::

## 执行安装

把示例域名替换成实际地址，然后执行：

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

安装器先解析版本：不带 `--version` 时从 Release 的 `latest.json` 取最新正式版，不依赖 GitHub API；指定版本时直接下载对应资产。

```bash
# 固定版本，生产环境和问题复现建议这样装
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --version ce-v0.0.13 \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

脚本启动后会打印 **Version**、**Base URL**、**Install directory**，出现 `Continue installation? [y/N]` 时输入 `y` 回车才继续，回车或输入其他内容都会取消。管道执行时确认问题从 `/dev/tty` 读取。

`--install-website`、`--registry`、`--auth-challenge`、`--yes` 等参数的完整说明见[安装详解](/docs/deployment/install)。

## 检查安装结果

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
sudo systemctl status hypercdr.service hypercdr-upgrade-runner.service --no-pager
curl -kfsS -o /dev/null -w 'ready=%{http_code}\n' https://platform.example.com:12443/readyz
```

Edge、API、前端、PostgreSQL、注册执行器和升级执行器都应处于健康状态，`/readyz` 返回 `200`。没有 systemd 的主机需要自行管理平台与升级执行器，容器能启动不代表页面里的升级功能可执行。

安装完成后只有一个访问入口：`https://<host>:12443`，集群、存储、策略、容灾、任务与平台设置都在这里。产品文档站是独立部署的服务，不由安装器部署。

::: warning 健康检查的边界

`/healthz` 成功只说明服务已启动；`/readyz` 通过也不代表对象存储、Agent 注册或真实恢复任务已经验证。

:::

验收分三段，逐项清单见[安装后验收](/docs/deployment/install#post-install-checks)：

| 阶段 | 通过标准 |
| --- | --- |
| 主机侧 | Edge、API、前端、PostgreSQL、注册执行器、升级执行器均健康，`/readyz` 返回 `200` |
| 浏览器侧 | 登录页可打开、验证码与安装时的模式一致、能登录并改密 |
| 业务侧 | 测试集群 online、存储连接测试通过、产出恢复点、隔离 Drill 成功 |

前两段只证明控制台活着，第三段才证明它真能保护业务。

## 首次登录

浏览器打开 `https://<host>:12443`，初始账号 `admin`，初始密码 `admin123`，**首次登录后必须修改密码**。登录挑战可能是图片验证码或 Cloudflare Turnstile，取决于安装时的 `--auth-challenge` 选择，实际模式以安装目录 `.env` 为准。

能打开登录页只说明入口可以访问。完成验收还需要注册一个测试集群、配置对象存储并生成第一个恢复点，这三步在[首次使用](/docs/user-guide/first-use)中依次说明。

## 下一步

<div class="hcdr-grid hcdr-grid-3">
  <a class="hcdr-card" href="/docs/user-guide/first-use"><b>首次使用</b><span>注册集群、配置存储与策略，用 Drill 在隔离目标里验证恢复。</span></a>
  <a class="hcdr-card" href="/docs/deployment/install"><b>安装详解</b><span>全部参数、非交互安装、离线包、升级回滚、卸载与故障定位。</span></a>
  <a class="hcdr-card" href="/docs/deployment/configuration"><b>配置参考</b><span>环境变量、验证码模式、注册配置与访问地址调整。</span></a>
</div>
