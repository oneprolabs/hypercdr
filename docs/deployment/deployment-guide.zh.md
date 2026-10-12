# 安装 HyperCDR

本文对应 `deploy/online/install.sh`、`scripts/release/install-blue-green.sh` 和根目录 `docker-compose.yml`。

## 安装前准备

使用 Linux 主机，以 root 或 sudo 执行。预先安装 Docker Engine、Docker Compose v2、Bash、curl、tar、jq 和 OpenSSL；安装器不会替你安装 Docker。宿主机升级执行器还使用 flock（通常来自 util-linux）。容量按数据库、日志和保留版本规划；应用备份数据写入配置的对象存储。

主机需要访问 GitHub API、Release 下载地址及镜像仓库；集群节点也必须能拉取组件镜像。浏览器及集群 Agent 需要访问中控 HTTPS/WSS 入口。默认中控端口为 12443，安装官网时额外开放 12445；数据库、API、前端和注册执行器不直接发布宿主机端口。Nginx Proxy Manager 和本地 Harbor 不是安装前提。

Install for me 还要求中控注册执行器能访问 kubeconfig 中的集群 API。Run command 要求执行命令的主机具备相应网络和 Kubernetes 管理权限。

## 安装最新发布版

将示例域名替换为浏览器和集群都能访问的实际 IP 或域名；域名解析需自行配置。

```bash
curl -fsSL https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr \
    --yes
```

不带 `--version` 时查询 GitHub 最新 Release。固定版本时增加 `--version VERSION`，VERSION 使用 Release 的版本号，可带 v 前缀。安装器下载该版本的安装包，再运行包内安装脚本。

官网默认不安装。需要时增加 **`--install-website`**，这是无参数开关，不接受 true/false。官网通过 `https://HOST:12445/` 访问，中控通过 `https://HOST:12443/login` 访问。`--registry` 可覆盖镜像仓库；默认是 `registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr`。

当前入口解析了 `--public-base-url`，但没有传给下游安装命令，不应依赖它改变生成的注册地址。以 `--base-url` 为准。

## 首次访问和验收

安装器准备 TLS 文件、随机运行密钥、Compose 配置、版本清单和生命周期脚本。自动生成的证书不等同于浏览器信任的公开证书；正式环境应按自己的证书管理流程配置可信证书。

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
curl --cacert tls/tls.crt https://platform.example.com:12443/healthz
sudo systemctl status hypercdr.service hypercdr-upgrade-runner.service
```

证书域名须与 URL 一致。无 systemd 的主机需自行管理平台和升级执行器，不能仅凭容器启动就认定 UI 升级可执行。`/readyz` 的配置检查不能替代数据库、登录及注册验收。

首次账号为 `admin`，初始密码为 `admin123`（见后端默认账号常量）。首次使用应修改密码。登录需要通过已配置的验证码。检查登录、集群注册和真实备份任务后再认为环境可用。

## 配置、升级和卸载

配置文件在安装目录 `.env`；不要直接复制源码中的占位 `env.example` 代替安装。验证码支持 `captcha` 和 `turnstile`，参见[配置参考](configuration.zh.md)。

平台通过蓝绿 API/前端切换版本，PostgreSQL 为共享持久化服务。当前清单为 `current-release.json`，历史版本保存在 `releases/`。集群组件版本依据活动清单，平台升级不等于所有集群组件已经完成升级。

参见[运维指南](../operations/platform.zh.md)、[首次使用](../user-guide/first-use.zh.md)及[故障排查](../operations/troubleshooting.zh.md)。
