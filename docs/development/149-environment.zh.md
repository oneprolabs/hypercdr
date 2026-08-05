# 149 中控平台部署现状（历史快照）

> 本文只记录目录重构前的 149 环境，不是当前安装说明。当前开发运行数据统一位于 `/data/hypercdr-runtime`，请使用 `scripts/dev/`。

更新时间：2026-07-14

本文记录 `192.168.8.149` 当前 HyperCDR 中控平台的实际部署状态。该环境是原 149 主机损坏后，基于 `/data/hypercdr` 源码目录重新恢复的全新部署；旧 Harbor 数据和 PostgreSQL 数据未恢复。

## 当前运行模式

2026-07-14 已从标准 Compose 中控平台切换到源码开发模式：

- 标准 Compose 平台容器已卸载，`/data/hypercdr/deploy` 数据保留。
- 开发 PostgreSQL 容器为 `hypercdr-dev-postgres`，数据在 `/data/hypercdr/.dev/data/postgres`。
- 后端源码编译到 `/data/hypercdr/.dev/bin`，以 transient systemd service `hypercdr-dev-api` 运行。
- 前端 Vite 从 `/data/hypercdr/.dev/frontend` 运行，源码通过软链接热更新，service 为 `hypercdr-dev-frontend`。
- 开发入口为 `https://192.168.8.149:3002`，Vite 终止 TLS 后代理到内部 HTTP 后端 `127.0.0.1:18080`，Agent 使用 `wss://192.168.8.149:3002/ws/agent`。该拓扑与标准 nginx 终止 TLS 后代理 API 一致。
- 标准镜像构建、Harbor 推送和 Bootstrap 安装流程仍然保留，和开发模式互不覆盖。

开发模式操作：

```bash
cd /data/hypercdr/hypercdr-platform/deployments/dev
./start-dev.sh
./status-dev.sh
./stop-dev.sh
```

下文“标准部署”部分保留最近一次标准 Compose 配置，供切回发布验收模式时参考，并不表示对应平台容器当前正在运行。

## 总体部署方式

当前标准部署方式是：

- Harbor 使用 Docker Compose 部署。
- PostgreSQL 使用中控平台 Docker Compose 部署。
- HyperCDR 后端 API/WebSocket 使用中控平台 Docker Compose 部署。
- HyperCDR 前端使用独立 `platform-frontend` 镜像，由 nginx 托管静态文件并代理后端 API/WS。
- 平台所需业务镜像构建后推送到本机 Harbor。

也就是说，中控平台当前已经切换为标准镜像化部署；旧的 systemd 直跑后端二进制和 Vite dev server 部署方式已停止、禁用并移除 systemd unit。

## 目录位置

| 项目 | 路径 |
| --- | --- |
| 总目录 | `/data/hypercdr` |
| 平台源码 | `/data/hypercdr/hypercdr-platform` |
| Velero 源码 | `/data/hypercdr/hypercdr-velero/velero-1.17.1` |
| Harbor 安装目录 | `/data/harbor` |
| 平台证书目录 | `/data/hypercdr/certs` |
| 平台运行日志 | `/data/hypercdr/logs` |
| 平台 secret 文件 | `/data/hypercdr/platform_secret_key` |
| 标准部署目录 | `/data/hypercdr/deploy` |
| Compose 文件 | `/data/hypercdr/deploy/docker-compose.yaml` |
| Compose 环境文件 | `/data/hypercdr/deploy/.env` |

## Harbor

Harbor 部署在 `/data/harbor`，通过 Docker Compose 运行。

- 访问地址：`https://192.168.8.149:5001`
- 端口：`80`、`5001`
- 管理员用户：`admin`
- 管理员密码文件：`/data/harbor/harbor_admin_password`

当前 Harbor 项目：

- `hypercdr`
- `baseimage`

`hypercdr` 项目用于存放 HyperCDR 平台、agent、Velero 等运行镜像。`baseimage` 项目用于存放从外网同步或下载的通用基础镜像。

## PostgreSQL

PostgreSQL 由 `/data/hypercdr/deploy/docker-compose.yaml` 管理。

| 项目 | 值 |
| --- | --- |
| 容器名 | `hypercdr-postgres` |
| 镜像 | `192.168.8.149:5001/hypercdr/postgres:16` |
| 端口 | `15432:5432` |
| 数据库 | `hypercdr` |
| 用户 | `hypercdr` |
| 密码 | `hypercdr` |

后端连接串：

```text
postgres://hypercdr:hypercdr@127.0.0.1:15432/hypercdr?sslmode=disable
```

## 后端 API 和 WebSocket

后端由 `/data/hypercdr/deploy/docker-compose.yaml` 管理，运行 `platform-api` 镜像。

| 项目 | 值 |
| --- | --- |
| 容器名 | `hypercdr-platform-api` |
| 镜像 | `192.168.8.149:5001/hypercdr/platform-api:v20260714.5` |
| 监听地址 | `0.0.0.0:18080` |
| 内部 API 地址 | `http://hypercdr-platform-api:18080` |
| 对外 API 入口 | `https://192.168.8.149:3002`，由前端 nginx 终止 TLS 并代理 |
| 对外 WebSocket 地址 | `wss://192.168.8.149:3002/ws/agent` |

关键环境变量在 `/data/hypercdr/deploy/.env` 中维护：

```bash
HCDR_PUBLIC_BASE_URL=https://192.168.8.149:3002
HCDR_AGENT_WS_ENDPOINT=wss://192.168.8.149:3002/ws/agent
HCDR_IMAGE_REGISTRY=192.168.8.149:5001/hypercdr
HCDR_AGENT_IMAGE=192.168.8.149:5001/hypercdr/comm-agent:v20260714.5
HCDR_VELERO_IMAGE=192.168.8.149:5001/hypercdr/velero:v1.18.2-hcdr.2
HCDR_VELERO_AWS_PLUGIN_IMAGE=192.168.8.149:5001/hypercdr/velero-plugin-for-aws:v1.13.0
HCDR_REGISTRY_CA_PATH=/etc/hypercdr/registry-ca.crt
```

## 前端

前端由 `/data/hypercdr/deploy/docker-compose.yaml` 管理，运行 `platform-frontend` 镜像，使用 nginx 托管静态资源并代理后端。

| 项目 | 值 |
| --- | --- |
| 容器名 | `hypercdr-platform-frontend` |
| 镜像 | `192.168.8.149:5001/hypercdr/platform-frontend:v20260714.5` |
| 监听地址 | `0.0.0.0:3002` |
| 访问地址 | `https://192.168.8.149:3002` |

前端 nginx 代理路径：

```text
/api/
/install.sh
/prepare-node.sh
/assets/
/healthz
/readyz
/ws/
```

## Harbor 镜像

当前已经推送到本机 Harbor 的镜像：

```text
192.168.8.149:5001/hypercdr/platform-api:dev
192.168.8.149:5001/hypercdr/platform-api:v20260714.5
192.168.8.149:5001/hypercdr/platform-frontend:v20260714.5
192.168.8.149:5001/hypercdr/comm-agent:dev
192.168.8.149:5001/hypercdr/comm-agent:v20260714.5
192.168.8.149:5001/hypercdr/postgres:16
192.168.8.149:5001/hypercdr/velero:v1.18.2-hcdr.2
192.168.8.149:5001/hypercdr/velero-plugin-for-aws:v1.13.0
```

其中：

- `platform-api:dev` 来自平台后端源码编译。
- `comm-agent:dev` 来自 agent 源码编译。
- `velero:v1.18.2-hcdr.2` 从 `/data/hypercdr-main/third_party/velero` 的固定源码构建，不是直接拉取官方 Velero 镜像。
- `velero-plugin-for-aws:v1.13.0` 从外部镜像源同步后推送到本机 Harbor。

## Go 版本

当前统一使用 Go `1.24.9` 编译：

- platform-api
- platform-migrate
- comm-agent
- Velero

Go 路径：

```text
/usr/local/go/bin/go
```

版本：

```text
go version go1.24.9 linux/amd64
```

## 证书

Harbor 使用 20 年证书。

| 文件 | 路径 |
| --- | --- |
| Harbor CA 证书 | `/data/harbor/cert/hypercdr-ca.crt` |
| Harbor CA 私钥 | `/data/harbor/cert/hypercdr-ca.key` |
| Harbor 服务证书 | `/data/harbor/cert/harbor.crt` |
| Harbor 服务私钥 | `/data/harbor/cert/harbor.key` |

平台证书：

| 文件 | 路径 |
| --- | --- |
| 平台证书 | `/data/hypercdr/certs/platform.crt` |
| 平台私钥 | `/data/hypercdr/certs/platform.key` |

当前平台由前端 nginx 统一终止 TLS，容器内部 nginx 到 API 使用 HTTP：

- API：`https://192.168.8.149:3002`
- WebSocket：`wss://192.168.8.149:3002/ws/agent`

平台使用包含 `192.168.8.149` IP SAN 的 20 年自签名证书，有效期至 2046-07-09。

## 常用运维命令

查看平台服务：

```bash
cd /data/hypercdr/deploy
docker compose ps
```

重启平台服务：

```bash
cd /data/hypercdr/deploy
docker compose up -d
```

查看容器：

```bash
docker ps
```

查看平台日志：

```bash
cd /data/hypercdr/deploy
docker compose logs -f hypercdr-platform-api hypercdr-platform-frontend
```

查看 Harbor 密码：

```bash
cat /data/harbor/harbor_admin_password
```

## 当前访问入口

中控平台前端：

```text
https://192.168.8.149:3002
```

后端安装脚本：

```text
http://192.168.8.149:18080/install.sh
```

节点准备脚本：

```text
http://192.168.8.149:18080/prepare-node.sh
```

当前 `/install.sh` 默认指向：

```text
wss://192.168.8.149:3002/ws/agent
192.168.8.149:5001/hypercdr/comm-agent:v20260714.5
192.168.8.149:5001/hypercdr/velero:v1.18.2-hcdr.2
192.168.8.149:5001/hypercdr/velero-plugin-for-aws:v1.13.0
```

当前 `/prepare-node.sh` 默认指向：

```text
REGISTRY_HOST="192.168.8.149"
REGISTRY_CA_URL="https://192.168.8.149:3002/assets/registry/ca.crt"
```
