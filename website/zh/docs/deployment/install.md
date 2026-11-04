---
title: 安装社区版
description: 在自有 Linux 主机上安装 HyperCDR 社区版，覆盖在线与离线安装、参数、验收、升级与卸载。
---

# 安装社区版

本文面向平台管理员，覆盖在线安装、离线安装、安装参数、验收、升级和卸载。在线入口是 `deploy/online/install.sh`，Release 安装包内的 `scripts/release/install-blue-green.sh` 负责实际部署。

## 安装前准备 {#prepare}

### 主机要求

使用 Linux 主机，以 root 或 sudo 执行。以下组件需要预先准备，安装器**不会**替你安装：

| 项目 | 要求 |
| --- | --- |
| 容器运行环境 | Docker Engine、Docker Compose v2（`docker compose`） |
| 基础工具 | Bash、curl、tar、OpenSSL |
| 升级执行器 | `flock`（通常由 util-linux 提供） |
| 可选工具 | `jq` 不是在线入口的必需项，但本地发布/打包工具可能需要 |

安装器不会安装 Docker，也不会安装 Harbor、Nginx Proxy Manager 或外部对象存储。

### 网络要求

| 方向 | 用途 |
| --- | --- |
| 安装主机 → GitHub | 获取入口脚本、Release 安装包和校验文件 |
| 安装主机 → 镜像仓库 | 拉取 API、前端、注册执行器及依赖镜像 |
| 浏览器 → 中控主机 `12443/tcp` | 访问登录页和 API |
| 集群 Agent → 中控 `12443/tcp` | 建立 HTTPS/WSS 出站连接 |
| Agent / 集群组件 → 对象存储 | 写入和读取备份数据 |
| 注册执行器 → Kubernetes API | 使用 Install for me 注册集群 |

数据库、API、前端和注册执行器不直接发布宿主机端口；安装可选产品门户时另用 `12445/tcp`。

### 地址与证书

`--base-url` 必须是浏览器和集群 Agent 都能访问的 HTTPS Origin：

```text
https://platform.example.com:12443
```

可以使用 IP，但正式环境建议使用 DNS 名称和受信任证书。安装器生成的 TLS 证书**不会自动被浏览器信任**，证书名称、访问地址和反向代理配置必须一致。

::: info 注册路径的额外要求

**Install for me** 还要求中控注册执行器能访问 kubeconfig 中的集群 API。**Run command** 要求执行命令的主机具备相应网络和 Kubernetes 管理权限。

:::

## 执行安装 {#run}

### 安装最新正式版

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

安装器会显示解析到的实际版本、地址和安装目录。交互安装会询问是否继续；管道方式也会从 `/dev/tty` 读取回答。

### 固定版本安装

生产环境和问题复现建议固定版本：

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --version ce-v0.0.13 \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

不带版本时，入口从 GitHub Release 的固定 `latest.json` 资产获取版本，不依赖 GitHub API；指定版本时直接下载对应 Release 资产。

### 非交互安装

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr \
    --auth-challenge image \
    --yes
```

`--yes` 会跳过确认，并默认选择图片验证码；自动化环境建议显式指定 `--auth-challenge image` 或 `turnstile`。

### 安装参数

| 参数 | 必需 | 说明 |
| --- | :---: | --- |
| `--base-url URL` | 是 | 中控对外 HTTPS Origin，默认入口端口 `12443` |
| `--version VERSION` | 否 | Release 版本，如 `ce-v0.0.13`；不填使用最新正式版 |
| `--install-dir PATH` | 否 | 安装目录，默认 `/var/lib/hypercdr` |
| `--registry REGISTRY` | 否 | 覆盖默认镜像仓库 |
| `--install-website` | 否 | 同时安装可选产品门户，使用 `12445` |
| `--auth-challenge MODE` | 否 | `auto`、`image` 或 `turnstile` |
| `--yes` | 否 | 跳过安装确认 |

`--install-website` 是**无参数开关**，不接受 `true`/`false`。门户通过 `https://HOST:12445/` 访问，中控通过 `https://HOST:12443/login` 访问。

::: warning `--public-base-url` 当前不生效

入口脚本解析了这个参数，但没有传给下游安装命令。不要依赖它改变生成的注册地址，以 `--base-url` 为准。

:::

## 验证码与登录

Release 包可携带 Turnstile Site Key 和 Secret Key。交互安装选择 `auto` 时，如果凭据完整，会询问当前访问主机/IP 是否已加入 Turnstile 允许主机列表：

- 选择 `y`：使用 Cloudflare Turnstile；
- 选择 `n` 或直接回车：使用内置图片验证码；
- `--auth-challenge image`：强制图片验证码；
- `--auth-challenge turnstile`：强制 Turnstile，缺少完整凭据时安装失败。

Cloudflare 运行时不可达不会自动切换图片验证码；需要重新部署或修改 `.env` 后重建受影响容器。图片验证码为 4 位数字和字母，字母校验不区分大小写。

## 检查安装结果 {#verify}

安装器会准备 TLS 文件、随机运行密钥、Compose 配置、版本清单和生命周期脚本。

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
sudo systemctl status hypercdr.service hypercdr-upgrade-runner.service --no-pager
curl -kfsS -o /dev/null -w 'ready=%{http_code}\n' https://platform.example.com:12443/readyz
```

Edge、API、前端、PostgreSQL、注册执行器和升级执行器都应处于健康状态。无 systemd 的主机需要自行管理平台和升级执行器，容器能启动不代表 UI 升级可执行。

::: warning 健康检查的边界

`/healthz` 成功只说明服务起来了；`/readyz` 通过也不代表对象存储、Agent 注册或真实恢复任务已验证。

:::

## 首次登录与验收 {#first-login}

初始账号通常为 `admin`，初始密码为 `admin123`，**首次登录后必须修改密码**。登录需要通过安装时选择的验证方式（图片验证码或 Cloudflare Turnstile）。

![HyperCDR 149 开发环境登录页](/images/overview/login-149.png)

这张截图来自 149 开发环境，生产环境应替换为正式地址和受信任证书。集群注册、存储和任务页面需要登录后的业务数据，本版不使用未经验证的示意图。

在认为环境可用之前，按顺序完成这些验收：

1. 访问 `https://<host>:12443`，确认登录页、验证码和认证模式一致；
2. 用初始管理员登录后立即改密，检查 **Settings → Access & Login** 的公开地址；
3. 检查 Edge、API、前端、PostgreSQL、注册执行器、升级执行器和 `/readyz`；
4. 注册测试集群，确认 Agent online、节点/Namespace 清单和 inventory 刷新；
5. 创建对象存储并执行 **Test Storage Connection**；
6. 创建最小测试策略，执行一次同步，检查恢复点和详细日志；
7. 在隔离 Namespace 执行一次 Drill，并验证工作负载、PVC 和应用数据。

## 离线安装

先在联网且已认证的发布环境下载对应 Release 资产及校验文件：

```bash
VERSION=ce-v0.0.13
gh release download "$VERSION" -R oneprolabs/hypercdr \
  -p "hypercdr-installer-${VERSION}.tar.gz" \
  -p "hypercdr-installer-${VERSION}.sha256"
sha256sum -c "hypercdr-installer-${VERSION}.sha256"
```

把校验通过的压缩包复制到目标主机，解压后执行包内安装器：

```bash
tar -xzf hypercdr-installer-${VERSION}.tar.gz
cd hypercdr-installer-${VERSION}
sudo ./install-blue-green.sh "$VERSION" \
  --base-url https://platform.example.com:12443 \
  --domain platform.example.com \
  --registry registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr \
  --install-dir /var/lib/hypercdr \
  --execute
```

离线安装仍需要目标主机能访问配置的镜像仓库，除非镜像已提前导入并改用可访问的本地仓库。安装包不含 Docker Engine，也不自动准备 Kubernetes 集群或对象存储。

## 升级、回滚与卸载

升级前备份 PostgreSQL、`.env`、平台密钥、TLS 文件和版本清单。平台升级采用蓝绿部署，成功切换后检查活动版本、任务和 Agent 状态。

```bash
# 回滚应用流量与版本清单（不撤销数据库迁移）
sudo /var/lib/hypercdr/deploy-blue-green.sh --rollback

# 默认卸载，保留数据
sudo /var/lib/hypercdr/uninstall.sh --execute

# 彻底删除安装目录、数据库卷和平台镜像（不可恢复）
sudo /var/lib/hypercdr/uninstall.sh --purge-data --remove-images --execute
```

::: warning 回滚与卸载的边界

回滚平台镜像不能回滚数据库迁移，也不自动回滚集群 Agent；平台升级不等于 Agent、Velero/OADP 或 Kubernetes 组件升级。卸载中控不会删除集群 Agent、应用 namespace 或外部对象存储备份。

:::

## 安装产物与服务

典型安装目录为 `/var/lib/hypercdr`，实际文件以发布包版本为准：

| 路径 / 对象 | 作用 |
| --- | --- |
| `.env` | 地址、认证、数据库、密钥和镜像配置，必须保护权限 |
| `docker-compose.yml` | 当前发布包的服务编排 |
| PostgreSQL 数据卷 | 中控元数据和任务状态 |
| `certs/` | TLS 证书材料 |
| `release-manifest.json` | 活动版本和组件镜像身份 |
| `restart-platform.sh`、`configure-public-url.sh` | 重启和访问地址辅助脚本 |

平台通常由 Edge、蓝/绿 API、蓝/绿前端、PostgreSQL、注册执行器和升级执行器组成；可选产品门户独占 `12445`，不安装门户不影响中控 `12443`。

## 常见问题与故障定位

| 现象 | 优先检查 |
| --- | --- |
| 入口下载卡住 | 到 `raw.githubusercontent.com` 的 DNS、IPv4/IPv6 和 HTTPS 出口；用 `--version` 可减少版本发现步骤 |
| 返回 GitHub API 403 | 新入口不应依赖 GitHub API，确认使用的是 `main` 上最新 `install.sh` |
| 镜像拉取失败 | Registry 地址、凭据、DNS、代理和防火墙 |
| 登录页验证码异常 | `/api/v1/auth/config`、`.env` 中的模式、前端/API 版本一致性 |
| Turnstile 失败 | site/secret key、允许域名、浏览器和 API 出站网络 |
| 浏览器证书警告 | 使用正式证书，或按组织安全策略导入测试证书 |
| Agent 离线 | Agent 到 `--base-url` 的 HTTPS/WSS 出站、证书、时间同步、Token 和网络策略 |
| 存储测试失败 | endpoint、DNS/TLS、Bucket、区域、凭据权限和集群网络 |
| 注册任务卡住 | kubeconfig context、RBAC、注册执行器日志和 clusterType |
| 恢复失败 | 恢复点状态、目标权限、PVC/CSI、对象存储和任务首个失败事件 |

## 接下来

- 配置项与验证码模式：[配置参考](/docs/deployment/configuration)
- 更换访问地址：[更改平台访问地址](/docs/deployment/configuration#access-url)
- 走完第一次完整流程：[首次使用](/docs/user-guide/first-use)
- 装机后出问题：[故障排查](/docs/operations/troubleshooting)
