---
title: 安装详解
description: HyperCDR 安装参数、非交互安装、验证码模式、离线包、升级回滚、卸载与安装故障定位。
---

# 安装详解

本页是安装的参考文档，涵盖参数、验证码模式、离线包、升级回滚卸载和故障定位。在线入口是 `deploy/online/install.sh`，Release 安装包内的 `scripts/release/install-blue-green.sh` 负责实际部署。

::: tip 首次安装请先阅读快速开始

首次安装请按[快速开始](/docs/)完成三步流程，前置条件、证书和验证步骤见[安装 HyperCDR](/docs/getting-started/install)。本页适用于已安装过一次、需要查询参数或处理特定场景的读者。

:::

## 安装命令

最简在线安装命令如下：

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

不带 `--version` 时，入口脚本从 Release 的 `latest.json` 资产获取最新正式版，不依赖 GitHub API；指定版本时直接下载对应资产，生产环境和问题复现建议固定版本。

### 非交互安装

自动化环境可使用 `--yes` 跳过确认，并显式指定验证码模式：

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr \
    --auth-challenge image \
    --yes
```

### 安装参数

| 参数 | 必需 | 说明 |
| --- | :---: | --- |
| `--base-url URL` | 是 | 控制台对外 HTTPS Origin，默认入口端口 `12443` |
| `--version VERSION` | 否 | Release 版本，如 `ce-v0.0.13`；不填使用最新正式版 |
| `--install-dir PATH` | 否 | 安装目录，默认 `/var/lib/hypercdr` |
| `--registry REGISTRY` | 否 | 覆盖默认镜像仓库 |
| `--install-website` | 否 | 历史产品门户兼容参数，不建议用于新部署；产品文档站独立部署 |
| `--auth-challenge MODE` | 否 | `auto`、`image` 或 `turnstile` |
| `--yes` | 否 | 跳过安装确认 |

`--install-website` 是**无参数开关**，不接受 `true`/`false`。当前源码已停止自动部署旧门户，新部署不要依赖它；历史安装包的行为以该版本说明为准。控制台始终通过 `https://HOST:12443/login` 访问，产品文档站独立部署。

::: warning `--public-base-url` 当前不生效

入口脚本解析了这个参数，但没有传给下游安装命令。不要依赖它改变生成的注册地址，以 `--base-url` 为准。

:::

## 验证码与登录

Release 包可以附带 Turnstile Site Key 和 Secret Key。交互安装选择 `auto` 且凭据完整时，安装器会询问当前访问主机/IP 是否已加入 Turnstile 允许主机列表：

- 选择 `y`：使用 Cloudflare Turnstile；
- 选择 `n` 或直接回车：使用内置图片验证码；
- `--auth-challenge image`：强制图片验证码；
- `--auth-challenge turnstile`：强制 Turnstile，缺少完整凭据时安装失败。

运行期间 Cloudflare 不可达时，系统不会自动切换到图片验证码；需要重新部署，或修改 `.env` 后重建受影响容器。图片验证码为 4 位数字和字母，字母校验不区分大小写。初始账号 `admin`，初始密码 `admin123`，首次登录后必须修改。

![HyperCDR 149 开发环境登录页](/images/overview/login-149.png)

截图来自 149 开发环境，生产环境应使用正式地址和受信任证书。

## 离线安装

先在联网且已认证的发布环境下载对应 Release 资产及校验文件：

```bash
VERSION=ce-v0.0.13
gh release download "$VERSION" -R oneprolabs/hypercdr \
  -p "hypercdr-installer-${VERSION}.tar.gz" \
  -p "hypercdr-installer-${VERSION}.sha256"
sha256sum -c "hypercdr-installer-${VERSION}.sha256"
```

将校验通过的压缩包复制到目标主机，解压后执行包内安装器：

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

回滚平台镜像不能回滚数据库迁移，也不自动回滚集群 Agent；平台升级不等于 Agent、Velero/OADP 或 Kubernetes 组件升级。卸载控制台不会删除集群 Agent、应用 Namespace 或外部对象存储备份。

:::

## 安装产物与服务

安装器会准备 TLS 文件、随机运行密钥、Compose 配置、版本清单和生命周期脚本。典型安装目录为 `/var/lib/hypercdr`，实际文件以发布包版本为准：

| 路径 / 对象 | 作用 |
| --- | --- |
| `.env` | 地址、认证、数据库、密钥和镜像配置，必须保护权限 |
| `docker-compose.yml` | 当前发布包的服务编排 |
| PostgreSQL 数据卷 | 控制台元数据和任务状态 |
| `certs/` | TLS 证书材料 |
| `release-manifest.json` | 活动版本和组件镜像标识 |
| `restart-platform.sh`、`configure-public-url.sh` | 重启和访问地址辅助脚本 |

平台通常由 Edge、蓝/绿 API、蓝/绿前端、PostgreSQL、注册执行器和升级执行器组成。控制台使用 `12443`，产品文档站独立部署。历史安装包可能包含使用 `12445` 的旧门户，升级前应核对所选版本说明及端口占用。

## 常见问题与故障定位

| 现象 | 优先检查 |
| --- | --- |
| 入口下载停滞 | 到 `raw.githubusercontent.com` 的 DNS、IPv4/IPv6 和 HTTPS 出口；用 `--version` 可减少版本发现步骤 |
| 返回 GitHub API 403 | 新入口不应依赖 GitHub API，确认使用的是 `main` 上最新 `install.sh` |
| 镜像拉取失败 | Registry 地址、凭据、DNS、代理和防火墙 |
| 登录页验证码异常 | `/api/v1/auth/config`、`.env` 中的模式、前端/API 版本一致性 |
| Turnstile 失败 | site/secret key、允许域名、浏览器和 API 出站网络 |
| 浏览器证书警告 | 使用正式证书，或按组织安全策略导入测试证书 |
| Agent 离线 | Agent 到 `--base-url` 的 HTTPS/WSS 出站、证书、时间同步、Token 和网络策略 |
| 存储测试失败 | endpoint、DNS/TLS、Bucket、区域、凭据权限和集群网络 |
| 注册任务停滞 | kubeconfig context、RBAC、注册执行器日志和 clusterType |
| 恢复失败 | 恢复点状态、目标权限、PVC/CSI、对象存储和任务首个失败事件 |

## 后续步骤

- 安装后的完整验收流程：[首次使用](/docs/user-guide/first-use)
- 配置项与验证码模式：[配置参考](/docs/deployment/configuration)
- 更换访问地址：[更改平台访问地址](/docs/deployment/configuration#access-url)
- 安装后出现问题：[故障排查](/docs/operations/troubleshooting)
