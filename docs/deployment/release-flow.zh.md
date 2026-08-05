# HyperCDR 标准打包与部署流程

更新时间：2026-07-14

本文定义 HyperCDR 后续标准化打包、镜像发布、平台部署和 agent 升级流程。该流程用于替代 149 当前“源码目录直接运行后端二进制 + Vite dev server”的临时恢复部署方式。

## 目标形态

HyperCDR 后续按镜像化方式发布和部署：

- 中控后端：`platform-api` 镜像。
- 中控前端：`platform-frontend` 镜像。
- agent：`comm-agent` 镜像。
- Velero：`velero` 镜像。
- Velero AWS plugin：同步外部镜像到 Harbor，除非后续改源码或换版本。
- PostgreSQL：使用官方 `postgres:16` 或同步到 Harbor 的 `postgres:16`。
- Harbor：继续独立部署在 `/data/harbor`，不放进中控平台 Compose。

中控平台标准部署方式改为 Docker Compose：

- `hypercdr-postgres`
- `hypercdr-platform-api`
- `hypercdr-platform-frontend`

不再使用 systemd 直接运行 `platform-api` 二进制和 Vite dev server 作为标准发布形态。

## 镜像仓库和命名

统一推送到 Harbor 的 `hypercdr` 项目。镜像仓库地址必须作为部署参数传入，不能写死在脚本或 Compose 模板中。

149 当前环境默认使用：

```text
192.168.8.149:5001/hypercdr
```

如果 Harbor 改为其他端口，例如 5001，则 bootstrap 页面里的镜像仓库地址填写：

```text
192.168.8.149:5001/hypercdr
```

后续安装脚本、镜像检查、Docker Compose `.env`、平台后端 `HCDR_IMAGE_REGISTRY`、agent 镜像、Velero 镜像都必须从该输入值派生。

标准镜像：

```text
192.168.8.149:5001/hypercdr/platform-api:<version>
192.168.8.149:5001/hypercdr/platform-frontend:<version>
192.168.8.149:5001/hypercdr/comm-agent:<version>
192.168.8.149:5001/hypercdr/velero:<version>
192.168.8.149:5001/hypercdr/velero-plugin-for-aws:<version>
192.168.8.149:5001/hypercdr/postgres:16
```

版本号采用：

```text
vYYYYMMDD.N
```

示例：

```text
v20260714.5
v20260714.5
```

`dev` tag 只用于临时调试，不作为正式发布版本。

## 标准打包流程

### 1. 确认版本号

```bash
export RELEASE_VERSION=v20260714.5
export REGISTRY=192.168.8.149:5001/hypercdr
```

### 2. 后端测试与编译

```bash
cd /data/hypercdr/backend

PATH=/usr/local/go/bin:$PATH \
GOTOOLCHAIN=local \
GOPROXY=https://goproxy.cn,direct \
go test ./...

PATH=/usr/local/go/bin:$PATH \
GOTOOLCHAIN=local \
GOPROXY=https://goproxy.cn,direct \
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build -trimpath -ldflags='-s -w' -o bin/platform-api ./cmd/platform-api

PATH=/usr/local/go/bin:$PATH \
GOTOOLCHAIN=local \
GOPROXY=https://goproxy.cn,direct \
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build -trimpath -ldflags='-s -w' -o bin/platform-migrate ./cmd/platform-migrate
```

### 3. 前端依赖与构建

```bash
cd /data/hypercdr/frontend

npm ci --registry=https://registry.npmmirror.com
npm run build
```

### 4. agent 测试与编译

```bash
cd /data/hypercdr/agent/comm-agent

PATH=/usr/local/go/bin:$PATH \
GOTOOLCHAIN=local \
GOPROXY=https://goproxy.cn,direct \
go test ./...

PATH=/usr/local/go/bin:$PATH \
GOTOOLCHAIN=local \
GOPROXY=https://goproxy.cn,direct \
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build -trimpath -ldflags='-s -w' -o /data/hypercdr-runtime/build/comm-agent ./cmd/comm-agent
```

### 5. Velero 编译

Velero 不随每次平台代码更新自动重打。只有以下情况才重新编译和发布 Velero 镜像：

- Velero 源码有修改。
- `velero-helper` 或 `velero-restore-helper` 有修改。
- 更换 Velero 版本。
- 基础镜像或安全修复要求重打。

需要重打时，从本地源码编译：

```bash
cd /data/hypercdr/third_party/velero

PATH=/usr/local/go/bin:$PATH \
GOTOOLCHAIN=local \
GOPROXY=https://goproxy.cn,direct \
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build -trimpath -ldflags='-s -w' -o /tmp/hypercdr-image-build/velero/rootfs/velero ./cmd/velero

PATH=/usr/local/go/bin:$PATH \
GOTOOLCHAIN=local \
GOPROXY=https://goproxy.cn,direct \
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build -trimpath -ldflags='-s -w' -o /tmp/hypercdr-image-build/velero/rootfs/velero-helper ./cmd/velero-helper

PATH=/usr/local/go/bin:$PATH \
GOTOOLCHAIN=local \
GOPROXY=https://goproxy.cn,direct \
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
go build -trimpath -ldflags='-s -w' -o /tmp/hypercdr-image-build/velero/rootfs/velero-restore-helper ./cmd/velero-restore-helper
```

## 标准镜像构建

### platform-api 镜像

镜像内容：

- `/usr/local/bin/platform-api`
- `/usr/local/bin/platform-migrate`
- 必要 CA 证书

镜像：

```text
192.168.8.149:5001/hypercdr/platform-api:<version>
```

### platform-frontend 镜像

前端单独做镜像，使用 nginx 托管静态文件。

构建方式：

- build 阶段使用 Node 镜像执行 `npm ci` 和 `npm run build`。
- runtime 阶段使用 nginx 镜像托管 `dist`。

镜像：

```text
192.168.8.149:5001/hypercdr/platform-frontend:<version>
```

前端 nginx 推荐代理以下路径到 `platform-api:18080`：

```text
/api/
/install.sh
/prepare-node.sh
/assets/
/ws/
```

这样浏览器只访问前端入口：

```text
http://192.168.8.149:3002
```

前端不需要因为后端地址变化而重新构建。

### comm-agent 镜像

镜像内容：

- `/comm-agent`

镜像：

```text
192.168.8.149:5001/hypercdr/comm-agent:<version>
```

### Velero 镜像

镜像内容：

- `/velero`
- `/velero-helper`
- `/velero-restore-helper`
- `/usr/bin/velero-helper`
- `/usr/bin/velero-restore-helper`

镜像：

```text
192.168.8.149:5001/hypercdr/velero:<version>
```

## 标准部署目录

建议新增标准部署目录：

```text
/data/hypercdr/deploy
```

目录内容：

```text
/data/hypercdr/deploy/docker-compose.yaml
/data/hypercdr/deploy/.env
/data/hypercdr/deploy/certs/
```

Compose 管理以下服务：

```text
hypercdr-postgres
hypercdr-platform-api
hypercdr-platform-frontend
```

Harbor 仍由 `/data/harbor` 独立管理。

## Docker Compose 部署流程

### 1. 更新 `.env`

`.env` 中至少包含：

```bash
RELEASE_VERSION=v20260714.5
REGISTRY=192.168.8.149:5001/hypercdr

PLATFORM_API_IMAGE=192.168.8.149:5001/hypercdr/platform-api:v20260714.5
PLATFORM_FRONTEND_IMAGE=192.168.8.149:5001/hypercdr/platform-frontend:v20260714.5
COMM_AGENT_IMAGE=192.168.8.149:5001/hypercdr/comm-agent:v20260714.5
VELERO_IMAGE=192.168.8.149:5001/hypercdr/velero:v1.18.2-hcdr.2
VELERO_AWS_PLUGIN_IMAGE=192.168.8.149:5001/hypercdr/velero-plugin-for-aws:v1.13.0
```

后端环境变量：

```bash
HCDR_PUBLIC_BASE_URL=http://192.168.8.149:3002
HCDR_AGENT_WS_ENDPOINT=ws://192.168.8.149:3002/ws/agent
HCDR_IMAGE_REGISTRY=192.168.8.149:5001/hypercdr
HCDR_AGENT_IMAGE=192.168.8.149:5001/hypercdr/comm-agent:v20260714.5
HCDR_VELERO_IMAGE=192.168.8.149:5001/hypercdr/velero:v1.18.2-hcdr.2
HCDR_VELERO_AWS_PLUGIN_IMAGE=192.168.8.149:5001/hypercdr/velero-plugin-for-aws:v1.13.0
```

通过 bootstrap 页面安装时，页面中的镜像仓库输入框是该值的唯一来源。安装脚本会把它写入 `HCDR_IMAGE_REGISTRY`，并生成 `HCDR_AGENT_IMAGE`、`HCDR_VELERO_IMAGE`、`HCDR_VELERO_AWS_PLUGIN_IMAGE`。因此 Harbor 地址或端口变化时，只需要改页面输入值，不需要修改 Compose 模板。

如果前端 nginx 代理 `/ws/agent` 到后端，则 agent 对外连接 `ws://192.168.8.149:3002/ws/agent`。

### 2. 拉取镜像

```bash
cd /data/hypercdr/deploy
docker compose pull
```

### 3. 启动或更新平台

```bash
docker compose up -d
```

### 4. 验证

```bash
docker ps
curl -sS http://192.168.8.149:3002/ >/dev/null
curl -sS http://192.168.8.149:3002/install.sh | grep 192.168.8.149
curl -sS http://192.168.8.149:3002/prepare-node.sh | grep 192.168.8.149
```

检查 `/install.sh` 里的镜像版本：

```text
comm-agent:<version>
velero:<version>
velero-plugin-for-aws:<version>
```

## agent 升级流程

agent 升级不做自动强制更新，由用户在中控平台手动控制。

### 目标交互

中控平台的集群卡片中展示：

- 集群名称。
- agent 在线状态。
- agent 当前版本。
- 平台推荐的最新 agent 版本。
- 是否需要升级。
- 手动升级按钮。

示例：

```text
Agent 当前版本：v20260714.5
Agent 最新版本：v20260720.1
状态：可升级
操作：升级 Agent
```

### 版本来源

agent 当前版本：

- 由集群内 `comm-agent` 启动后通过 WebSocket 注册或心跳上报。
- 平台保存到集群状态中。

agent 最新版本：

- 来自平台后端配置，例如：

```bash
HCDR_AGENT_IMAGE=192.168.8.149:5001/hypercdr/comm-agent:v20260720.1
HCDR_AGENT_VERSION=v20260720.1
```

或者来自后续发布记录表。

### 手动升级动作

用户在集群卡片点击“升级 Agent”后，平台下发升级任务给目标集群当前 agent。

升级任务目标：

- 更新集群内 `hypercdr-comm-agent` Deployment 的镜像。
- 镜像更新为平台配置的最新 `comm-agent:<version>`。
- 触发 rollout。
- agent 重连平台后上报新版本。
- 平台更新集群卡片状态。

### 升级成功判定

升级任务成功需要满足：

- Kubernetes Deployment rollout 成功。
- 新 agent 成功连接 WebSocket。
- 新 agent 上报版本等于平台最新版本。

### 升级失败处理

失败时集群卡片展示：

- 失败状态。
- 失败原因。
- 当前仍运行的 agent 版本。
- 可重试升级。

## 建议脚本

当前脚本已经固化在平台源码的 `scripts/release` 目录：

```text
/data/hypercdr/scripts/release/release-all.sh <version>
/data/hypercdr/scripts/release/build-release.sh <version>
/data/hypercdr/scripts/release/push-release.sh <version>
/data/hypercdr/scripts/release/deploy-platform.sh <version>
/data/hypercdr/scripts/release/verify-platform.sh
```

日常发布优先使用一键脚本：

```bash
cd /data/hypercdr/scripts/release

# 只构建并推送镜像，不更新当前中控平台
./release-all.sh v20260714.5 --config ./release.conf
```

该脚本只发布镜像，不安装或升级中控平台。

职责划分：

- `release-all.sh`：读取发布配置，登录 Harbor，编排构建、推送、拉取验证。
- `build-release.sh`：测试、编译、构建镜像。
- `push-release.sh`：推送镜像到 Harbor。
- `deploy-platform.sh`：本机维护/应急脚本，更新 `.env` 并执行 `docker compose up -d`，不作为标准发布入口。
- `verify-platform.sh`：本机维护/验证脚本，验证容器、端口、脚本、镜像版本。

发布配置文件：

```text
/data/hypercdr/scripts/release/release.conf
```

该文件保存 `HCDR_IMAGE_REGISTRY`、Harbor 登录用户、密码文件路径等环境差异。源码只需要提交 `release.conf.example`，真实 `release.conf` 不应提交。

Velero 构建脚本放在 Velero 源码自己的 `deployments` 目录：

```text
/data/hypercdr/third_party/velero/deployments/build-velero-image.sh
```

## 当前确认结论

- 中控平台标准部署改为 Docker Compose。
- 前端单独做 `platform-frontend` 镜像，使用 nginx 托管。
- bootstrap 页面输入的镜像仓库地址是安装部署唯一 registry 参数来源，支持 `host/hypercdr` 和 `host:port/hypercdr`。
- 源码构建和推送通过 `release-all.sh` 统一入口执行，镜像仓库地址来自 `release.conf` 或 `--registry`，不使用硬编码默认值。
- 中控平台全新安装仍通过 bootstrap 页面完成；中控平台升级后续通过平台 UI 手动触发，不由 release 脚本直接修改运行态。
- 镜像 tag 采用 `vYYYYMMDD.N`。
- Velero 只在源码变化、版本变化或安全需求时重新编译打包。
- agent 升级由中控平台集群卡片展示当前版本和最新版本，用户手动触发升级。
