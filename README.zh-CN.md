# HyperCDR

[English](README.md) | 中文

HyperCDR 是面向 Kubernetes 的容器容灾平台。中控平台统一管理保护策略、数据同步、
恢复点、容灾演练、恢复操作、组件升级和诊断日志；集群侧 `comm-agent` 通过出站连接
接入平台，并协调 Velero 在各业务集群中执行备份和恢复。

## 系统架构

```text
浏览器
  -> HyperCDR 前端
  -> HyperCDR API + PostgreSQL
  -> 安全 WebSocket
  -> comm-agent
  -> Velero + node-agent
  -> 对象存储
```

## 代码目录

```text
backend/                  Go 中控后端、调度器、迁移和升级器
frontend/                 React + TypeScript 中控前端
agent/comm-agent/         Go 集群通信与任务执行 Agent
charts/                   中控平台和业务集群 Helm Chart
docker/                   容器镜像定义和运行配置
scripts/                  开发、构建、Harbor、发布和验证脚本
third_party/velero/        HyperCDR 固定版本的 Velero 源码
docs/                     架构、协议、部署和运维资料
```

本地运行数据、构建产物、日志、证书、数据库文件和 kubeconfig 不属于源码。开发脚本
默认将这些内容保存到源码同级的 `hypercdr-runtime` 目录。

## 本地开发

依赖 Go 1.25.13、Node.js 22、Docker Compose v2、PostgreSQL 16 和 OpenSSL。

```bash
cp scripts/dev/dev.conf.example ../hypercdr-runtime/environments/community/dev.conf
./scripts/dev/start-dev.sh
./scripts/dev/status-dev.sh
./scripts/dev/stop-dev.sh
```

分别验证各组件：

```bash
(cd backend && go test ./...)
(cd agent/comm-agent && go test ./...)
(cd frontend && npm ci && npm run build)
```

## 发布和独立容器开发

生产继续使用 GitHub Release manifest 和现有蓝绿发布流程。PR 镜像检查只构建，
不推送、不部署。首次安装使用 `deploy/online/install.sh`，不指定版本时选择最新版本。

```bash
curl -fsSL https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- --base-url https://platform.example.com:12443 --install-dir /var/lib/hypercdr --yes
```

官网默认不安装，需要时添加 `--install-website`，官网端口为 12445。
`env.example` 是配置参考，包含占位值，不能替代安装脚本。

独立源码容器开发入口（需将 GitHub Release 的 manifest 下载到源码目录外）：

```bash
./scripts/dev/source-compose.sh up --manifest ../hypercdr-runtime/release-manifest.json
./scripts/dev/source-compose.sh ps
./scripts/dev/source-compose.sh down
```

默认通过 `http://localhost:13002/login` 访问，采用图片验证码；数据与现有环境隔离。
详细说明参考[构建与安装文档](docs/deployment/build-release-install.md)。

## 部署方式

- 独立主机：使用 `docker-compose.yml` 和 GitHub Release 安装器。
- Kubernetes 中控平台：使用 `charts/hypercdr-platform`。
- 业务集群组件：以平台生成的注册安装器为准；`charts/hypercdr-agent` 暂为未来受支持 Chart 的保留位置。

独立主机正式安装默认将持久化数据保存到 `/var/lib/hypercdr`。

## 文档与安全

文档入口为 [docs/README.md](docs/README.md)。禁止提交密码、kubeconfig、私钥、
证书、数据库备份、运行日志或客户数据。安全问题报告方式见 [SECURITY.md](SECURITY.md)。
