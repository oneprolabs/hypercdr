# HyperCDR

[English](README.md) | 中文

HyperCDR 是 Kubernetes 容器容灾平台，提供集群管理、保护策略、同步、恢复点、容灾演练、恢复任务及组件升级。集群 Agent 主动连接平台，协调 Velero 或 OpenShift OADP 执行任务。

社区版支持邮箱自助注册、Google 登录及组织邀请。一个账号只属于一个组织，业务资源按组织隔离。邮箱验证和邀请需要平台配置默认 SMTP；Google 凭据及回调配置参见[配置参考](docs/deployment/configuration.zh.md)。

## 安装发布版

Linux 主机需预装 Docker Engine/Compose v2、Bash、curl、tar、jq 和 OpenSSL。替换为浏览器及业务集群能访问的地址：

```bash
curl -fsSL https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- --base-url https://platform.example.com:12443 --install-dir /var/lib/hypercdr --yes
```

不指定版本使用最新 GitHub Release；增加 `--version VERSION` 可固定版本。中控使用 HTTPS 12443。官网默认不安装，增加无参数开关 `--install-website` 后使用 HTTPS 12445。不依赖 Nginx Proxy Manager 或本地 Harbor，安装器不会安装 Docker。

首次登录为 `admin` / `admin123`，请修改初始密码。验证码可配置为图片验证码或 Cloudflare Turnstile，以安装配置为准。自动生成的 TLS 证书不会自动成为浏览器信任的证书。

按[安装指南](docs/deployment/deployment-guide.zh.md) → [首次使用](docs/user-guide/first-use.zh.md)完成注册、保护和恢复验证。另见[配置参考](docs/deployment/configuration.zh.md)、[故障排查](docs/operations/troubleshooting.zh.md)、[GitHub Releases](https://github.com/oneprolabs/hypercdr/releases)。

## 架构与版本

浏览器通过 Edge 访问活动蓝/绿前端与 Go API；PostgreSQL 保存业务状态，无 MemoryStore 回退。集群 Agent 使用出站 WSS 上报资源并执行任务。Install for me 注册还需要平台注册执行器通过 kubeconfig 访问集群 API；命令方式在具备该网络访问的主机执行。

GitHub Release manifest 固定组件版本，安装和成功蓝绿升级激活本地清单，集群注册及组件升级读取该清单。宿主机升级执行器负责平台升级任务；平台升级不代表所有集群组件已经升级。详见[当前架构](docs/architecture/control-plane-architecture.zh.md)和[文档入口](docs/README.md)。

代码中的部署资产不等同于所有集群版本、存储驱动和拓扑都已通过验证；请核对专项适配范围。

## 代码与开发

`backend/` 为后端，`frontend/` 为中控界面，`agent/comm-agent/` 为集群 Agent，`deploy/online/` 为在线安装入口，`scripts/release/` 为发布及生命周期工具，`website/` 为独立官网，`third_party/velero/` 保留组件构建使用的源码，`docker/` 和 `charts/` 为部署资产。在线安装采用 Compose，不使用 Helm Chart。

运行数据、缓存、日志和构建产物放在源码同级 `hypercdr-runtime`。宿主机开发使用 Go 1.25.13、Node.js 22、Docker/Compose、PostgreSQL 和 OpenSSL，参见[开发说明](scripts/dev/README.md)。独立源码容器运行见[构建与安装](docs/deployment/build-release-install.md)。

```bash
make dev
make status
make update-dev
make stop
make verify
```

后端测试使用独立真实 PostgreSQL 数据库，不能连接业务数据库。详见[测试说明](docs/development/platform.md)及[贡献指南](CONTRIBUTING.md)。tag 发布当前会部署到 CI 配置的环境；手动工作流默认不部署，PR 检查不发布、不部署。见[发布流程](docs/deployment/release-flow.zh.md)。

不要提交密码、kubeconfig、私钥、证书、备份或运行产物。安全报告见 [SECURITY.md](SECURITY.md)，许可见 [LICENSE](LICENSE)。

## 发布版本规则

社区版标签采用 `ce-v主版本.次版本.补丁版本`，从 `ce-v0.0.0` 开始；企业版采用 `ee-v主版本.次版本.补丁版本`，从 `ee-v0.0.0` 开始，两者独立递增。日期和 Git 提交号保留为构建元数据，不加入版本号。历史版本仍支持指定安装和回滚；升级比较会将新前缀版本视为历史编号之后的新阶段，不会因为数字从零开始而误判为旧版本。

企业版通过 `community.lock` 固定社区版源码提交和已发布的组件版本，不要求两版版本号相同。依赖版本不存在时发布校验失败，不会静默替换或自动触发社区版发布。
