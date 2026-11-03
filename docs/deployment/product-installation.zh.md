# HyperCDR 产品安装

本文面向平台管理员，覆盖发行版在线安装、离线安装、参数、验收、升级和卸载。在线入口为 `deploy/online/install.sh`；Release 安装包内的 `scripts/release/install-blue-green.sh` 负责实际部署。

## 1. 安装前准备

### 1.1 主机要求

Linux 主机需要预先安装：

- Docker Engine；
- Docker Compose v2（`docker compose`）；
- Bash、curl、tar、OpenSSL；
- `flock`（通常由 util-linux 提供）；
- `jq` 不是当前在线入口的必需项，但本地发布/打包工具可能需要。

安装器不会自动安装 Docker，也不会安装 Harbor、Nginx Proxy Manager 或外部对象存储。

### 1.2 网络要求

| 方向 | 用途 |
|---|---|
| 安装主机 → GitHub | 获取入口脚本、Release 安装包和校验文件。 |
| 安装主机 → 镜像仓库 | 拉取 API、前端、注册执行器及依赖镜像。 |
| 浏览器 → 中控主机 `12443/tcp` | 访问登录页和 API。 |
| 集群 Agent → 中控 `12443/tcp` | 建立 HTTPS/WSS 出站连接。 |
| Agent/集群组件 → 对象存储 | 写入和读取备份数据。 |
| 注册执行器 → Kubernetes API | 使用 Install for me 注册集群。 |

数据库、API、前端和注册执行器不直接发布宿主机端口。安装产品门户时另使用 `12445/tcp`。

### 1.3 地址和证书

`--base-url` 必须是浏览器和集群 Agent 都能访问的 HTTPS Origin，例如：

```text
https://platform.example.com:12443
```

可以使用 IP，但正式环境建议使用 DNS 名称和受信任证书。安装器生成的 TLS 证书不会自动受到浏览器信任；证书名称、访问地址和反向代理配置必须一致。

## 2. 在线安装

### 2.1 安装最新正式版

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

安装器会显示解析到的实际版本、地址和安装目录。交互安装会询问是否继续；管道方式也会从 `/dev/tty` 读取回答。

### 2.2 固定版本安装

生产环境和问题复现建议固定版本：

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --version ce-v0.0.13 \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

不带版本时，入口从 GitHub Release 的固定 `latest.json` Asset 获取版本，不依赖 GitHub API；指定版本时直接下载对应 Release Asset。

### 2.3 非交互安装

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr \
    --auth-challenge image \
    --yes
```

`--yes` 会跳过确认，并默认选择图片验证码；建议在自动化环境显式指定 `--auth-challenge image` 或 `turnstile`。

## 3. 安装参数

| 参数 | 必需 | 说明 |
|---|---:|---|
| `--base-url URL` | 是 | 中控对外 HTTPS Origin，默认入口端口为 `12443`。 |
| `--version VERSION` | 否 | Release 版本，如 `ce-v0.0.13`；不填使用最新正式版。 |
| `--install-dir PATH` | 否 | 安装目录，默认 `/var/lib/hypercdr`。 |
| `--registry REGISTRY` | 否 | 覆盖默认镜像仓库。 |
| `--install-website` | 否 | 同时安装可选产品门户，使用 `12445`。 |
| `--public-base-url URL` | 否 | 兼容参数；当前安装流程以 `--base-url` 为主要地址，不应依赖此参数改变平台设置。 |
| `--auth-challenge MODE` | 否 | `auto`、`image` 或 `turnstile`。 |
| `--yes` | 否 | 跳过安装确认。 |

## 4. 验证码和登录

Release 包可携带 Turnstile Site Key 和 Secret Key。交互安装选择 `auto` 时，如果凭据完整，会询问当前访问主机/IP 是否已加入 Turnstile 允许主机列表：

- 选择 `y`：使用 Cloudflare Turnstile；
- 选择 `n` 或直接回车：使用内置图片验证码；
- `--auth-challenge image`：强制图片验证码；
- `--auth-challenge turnstile`：强制 Turnstile，缺少完整凭据时安装失败。

Cloudflare 运行时不可达不会自动切换图片验证码；需要重新部署或修改 `.env` 后重建受影响容器。图片验证码为 4 位数字和字母，字母校验不区分大小写。

## 5. 安装后验收

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
sudo systemctl status hypercdr.service hypercdr-upgrade-runner.service --no-pager
curl -kfsS -o /dev/null -w 'ready=%{http_code}\n' https://platform.example.com:12443/readyz
```

然后从浏览器访问 `/login`，完成初始登录、修改密码、验证码验证，并检查 API、前端、PostgreSQL 和 Edge 容器均健康。`readyz` 通过不代表对象存储、Agent 注册或真实恢复任务已验证。

初始账号通常为 `admin`，初始密码为 `admin123`；首次登录后必须修改密码。

## 6. 离线/本地安装

在联网且已认证的发布环境下载对应 Release 资产及校验文件：

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

离线安装仍需要目标主机能够访问配置的镜像仓库，除非镜像已提前导入并改用可访问的本地仓库。离线包不包含 Docker Engine，也不自动准备 Kubernetes 集群或对象存储。

## 7. 升级、回滚和卸载

升级前备份 PostgreSQL、`.env`、平台密钥、TLS 文件和版本清单。平台升级采用蓝绿部署，成功切换后检查活动版本、任务和 Agent 状态。回滚：

```bash
sudo /var/lib/hypercdr/deploy-blue-green.sh --rollback
```

回滚不撤销数据库迁移，也不自动回滚集群 Agent。

默认卸载保留数据：

```bash
sudo /var/lib/hypercdr/uninstall.sh --execute
```

彻底删除平台安装目录、数据库卷和平台镜像（不可恢复）：

```bash
sudo /var/lib/hypercdr/uninstall.sh --purge-data --remove-images --execute
```

这些命令不删除外部对象存储、业务集群、Harbor、MinIO 或其他无关服务。

## 8. 常见问题

- **入口下载卡住**：检查到 `raw.githubusercontent.com` 的 DNS、IPv4/IPv6 和 HTTPS 出口；用 `--version` 可减少版本发现步骤，但仍需下载 Release 资产。
- **返回 API 403**：新入口不应依赖 GitHub API；确认使用的是 `main` 上最新 `install.sh`。
- **镜像拉取失败**：检查 Registry 地址、凭据、DNS、代理和防火墙。
- **浏览器证书警告**：使用正式证书或将测试证书按组织安全策略导入信任库。
- **Agent 离线**：检查 Agent 到 `--base-url` 的 WSS 出站连通性、时间同步、Token 和集群网络策略。
- **任务失败**：先查看任务详情和详细日志，再核对 Agent、Kubernetes 权限、Velero/OADP、CSI 和对象存储访问。

