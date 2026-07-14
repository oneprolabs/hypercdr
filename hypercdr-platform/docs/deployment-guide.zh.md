# HyperCDR 安装部署文档

本文档描述 HyperCDR 当前推荐的完整部署流程。推荐使用一台独立主机部署 Harbor、bootstrap 下载页面和中控平台，不建议部署在 Kubernetes worker 节点上。

HyperCDR 安装流程不负责安装和托管 Harbor。Harbor 作为前置依赖，由用户自行安装和维护；本文档只说明 Harbor 要求、验证方式，以及 HyperCDR 镜像导入 Harbor 的操作。

## 1. 部署规划

### 1.1 主机规划

准备一台独立部署主机，建议配置：

- CPU：4 核及以上
- 内存：8 GB 及以上，推荐 16 GB
- 磁盘：200 GB 及以上
- 网络：源集群、目标集群的所有节点都能访问该主机
- 外网：安装 Docker、Harbor、拉取基础镜像时可能需要访问外网

本文档使用 `<DEPLOY_HOST_IP>` 表示你最终选择的独立部署主机 IP。实际执行前先替换为真实 IP，例如 `192.168.x.x`。

后续命令分为两个执行位置：

- 开发环境：当前保存 HyperCDR 源码的机器。
- 部署主机：新准备的独立服务器，用于安装 Harbor、bootstrap 下载页面和中控平台。

部署后的访问地址格式：

```text
Harbor:           https://<DEPLOY_HOST_IP>:5000
Harbor project:   <DEPLOY_HOST_IP>:5000/hypercdr
Bootstrap portal: http://<DEPLOY_HOST_IP>:8080
Control plane:    http://<DEPLOY_HOST_IP>:8088
```

### 1.2 端口规划

默认使用：

| 服务 | 端口 | 说明 |
| --- | --- | --- |
| Harbor HTTP | 5080 | Harbor HTTP 入口 |
| Harbor HTTPS | 5000 | Harbor HTTPS 入口，镜像推拉使用 |
| Bootstrap portal | 8080 | 中控平台安装下载页面 |
| HyperCDR control plane | 8088 | 单机版中控平台访问端口 |

部署前检查端口占用：

```bash
ss -ltnp
```

## 2. 准备 HyperCDR 部署工具

本节在开发环境执行。

进入项目根目录：

```bash
cd /data/hypercdr/hypercdr-platform
```

生成 Harbor 辅助工具包：

```bash
./scripts/package-harbor-tools.sh
```

生成结果：

```text
dist/harbor-tools/hypercdr-harbor-tools.tar.gz
```

该工具包不安装 Harbor 本体，只包含：

- Harbor 项目初始化脚本
- HyperCDR 镜像推送脚本
- HyperCDR 镜像导入脚本

把工具包复制到新的部署主机。下面以复制到 `/opt/hypercdr-install` 为例：

```bash
scp dist/harbor-tools/hypercdr-harbor-tools.tar.gz root@<DEPLOY_HOST_IP>:/opt/
```

登录部署主机并解压：

```bash
ssh root@<DEPLOY_HOST_IP>
mkdir -p /opt/hypercdr-install
tar -xzf /opt/hypercdr-harbor-tools.tar.gz -C /opt/hypercdr-install
cd /opt/hypercdr-install
```

后续 Harbor 项目初始化和镜像导入命令都在部署主机的 `/opt/hypercdr-install` 目录下执行。

在部署主机设置变量：

```bash
export HCDR_HOST=<DEPLOY_HOST_IP>
export HCDR_HARBOR_HTTP_PORT=5080
export HCDR_HARBOR_HTTPS_PORT=5000
export HCDR_REGISTRY=${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT}/hypercdr
```

## 3. 环境依赖

部署主机需要安装：

- Docker
- Docker Compose v2
- curl
- openssl
- tar

### 3.1 Ubuntu 依赖安装

以下命令适用于 Ubuntu，默认使用国内 Docker CE 镜像源。

1. 安装基础工具：

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl gnupg openssl tar
```

2. 添加 Docker GPG key：

```bash
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://mirrors.aliyun.com/docker-ce/linux/ubuntu/gpg -o /tmp/docker.gpg
sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg /tmp/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg
```

3. 添加 Docker CE 软件源：

```bash
. /etc/os-release
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://mirrors.aliyun.com/docker-ce/linux/ubuntu ${VERSION_CODENAME} stable" | sudo tee /etc/apt/sources.list.d/docker.list
```

4. 安装 Docker Engine 和 Docker Compose v2：

```bash
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
```

5. 启动 Docker：

```bash
sudo systemctl enable --now docker
```

### 3.2 企业内网环境

如果部署主机不能访问外网，需要提前准备：

- Docker Engine 安装源或安装包
- Docker Compose v2 插件
- 用户自行安装 Harbor 所需的软件源、安装包和组件镜像

企业环境如果有统一的软件源或镜像源，应优先使用企业内网源。

### 3.3 验证依赖

检查命令：

```bash
docker version
docker compose version
curl --version
openssl version
tar --version
```

## 4. 准备 Harbor

Harbor 是 HyperCDR 的前置依赖，由用户自行安装和维护。本文档不提供 Harbor 安装步骤，也不使用 HyperCDR 项目脚本安装 Harbor。

请先完成 Harbor 安装，并确认：

- Harbor 可以从部署主机访问。
- Harbor 可以从所有 Kubernetes 节点访问。
- Harbor 已启用 HTTPS。
- Harbor 镜像仓库地址已经确定。
- Harbor 管理员或具备项目创建权限的账号已经准备好。

推荐使用以下地址格式：

```text
https://<DEPLOY_HOST_IP>:5000
```

镜像仓库项目前缀推荐为：

```text
<DEPLOY_HOST_IP>:5000/hypercdr
```

如果 Harbor 使用其他端口，需要同步修改变量：

```bash
export HCDR_HARBOR_HTTPS_PORT=<HARBOR_HTTPS_PORT>
export HCDR_REGISTRY=${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT}/hypercdr
```

验证 Harbor API：

```bash
curl -k https://${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT}/api/v2.0/systeminfo
```

## 5. 初始化 Harbor 项目

推荐在 Harbor 中创建两个项目：

- `hypercdr`：存放 HyperCDR 运行镜像，例如 platform、agent、velero、postgres。
- `base-images`：存放构建基础镜像，例如 node、golang、debian、distroless，避免后续构建访问 Docker Hub 或 GCR。

创建 `hypercdr` 项目：

```bash
cd /opt/hypercdr-install/deployments/harbor

./init-project.sh \
  --harbor-url https://${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT} \
  --project hypercdr \
  --username admin \
  --password Harbor12345 \
  --execute
```

创建 `base-images` 项目：

```bash
./init-project.sh \
  --harbor-url https://${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT} \
  --project base-images \
  --username admin \
  --password Harbor12345 \
  --execute
```

验证 Harbor：

```bash
docker ps | grep harbor
curl -k https://${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT}/api/v2.0/projects
```

## 6. 推送 HyperCDR 镜像

进入 Harbor 辅助工具目录：

```bash
cd /opt/hypercdr-install/deployments/harbor
```

### 6.1 准备构建基础镜像

为了避免后续构建访问 Docker Hub 或 GCR，先把常见基础镜像同步到 `base-images` 项目。

目标前缀：

```text
${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT}/base-images
```

同步基础镜像：

```bash
./sync-base-images.sh \
  --registry ${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT}/base-images \
  --username admin \
  --password Harbor12345 \
  --execute
```

当前基础镜像清单：

```text
debian:bookworm-slim
node:22-bookworm-slim
golang:1.24-bookworm
golang:1.24
static-debian12:nonroot
```

### 6.2 首次部署：从旧 Harbor 同步必需运行镜像

新 Harbor 第一次使用时，只需要从旧 Harbor 同步 HyperCDR 部署必需镜像，不建议把旧 Harbor 中的 demo、Longhorn、历史 agent tag 全部迁移过来。

示例：旧 Harbor 是 `<SOURCE_HARBOR>/hypercdr`，新 Harbor 是 `${HCDR_REGISTRY}`：

```bash
./sync-required-images.sh \
  --source-registry <SOURCE_HARBOR>/hypercdr \
  --registry ${HCDR_REGISTRY} \
  --username admin \
  --password Harbor12345 \
  --execute
```

如果源 Harbor 也需要认证，请先手动登录源 Harbor：

```bash
docker login <SOURCE_HARBOR>
```

同步前建议先确认旧 Harbor 服务可访问：

```bash
curl -k https://<SOURCE_HARBOR>/api/v2.0/systeminfo
```

常见错误区分：

- 如果报 `connect: connection refused`，表示旧 Harbor 地址或端口没有服务监听，通常是 Harbor 未启动、端口不对或网络不通。
- 如果报 `unauthorized`、`authentication required`，表示旧 Harbor 需要认证，需要先执行 `docker login <SOURCE_HARBOR>`。
- 如果报 `x509: certificate signed by unknown authority`，表示当前机器 Docker 不信任 Harbor 证书，需要把 Harbor CA 放到 `/etc/docker/certs.d/<SOURCE_HARBOR>/ca.crt` 后重启 Docker。

必需镜像清单：

```text
hypercdr/platform-api:dev
hypercdr/comm-agent:dev
hypercdr/velero:v1.17.1-helperfix
hypercdr/postgres:16
```

### 6.3 后续更新：构建并推送最新镜像

当 platform 或 agent 源码更新后，在保存完整 HyperCDR 源码的开发环境执行：

```bash
cd /data/hypercdr/hypercdr-platform

./deployments/harbor/update-built-images.sh \
  --registry ${HCDR_REGISTRY} \
  --execute
```

该脚本会构建并推送：

- `${HCDR_REGISTRY}/platform-api:dev`
- `${HCDR_REGISTRY}/comm-agent:dev`

Velero 和 PostgreSQL 镜像通常不随业务代码更新，仍由首次同步得到。

脚本默认会把 `${HCDR_REGISTRY}` 同一 Harbor 主机下的 `base-images` 项目作为基础镜像来源。例如 `${HCDR_REGISTRY}` 是 `${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT}/hypercdr` 时，基础镜像来源会自动设置为 `${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT}/base-images`。

脚本默认使用国内 Go module proxy 和 npm registry。如果企业环境有内网 Go module proxy 或 npm registry，可以显式覆盖：

```bash
./deployments/harbor/update-built-images.sh \
  --registry ${HCDR_REGISTRY} \
  --base-registry ${HCDR_HOST}:${HCDR_HARBOR_HTTPS_PORT}/base-images \
  --goproxy <INTERNAL_GO_PROXY> \
  --gosumdb off \
  --npm-registry <INTERNAL_NPM_REGISTRY> \
  --execute
```

## 7. 生成 Bootstrap 下载页面

本节可以在开发环境执行，也可以在已经复制完整 HyperCDR 源码的部署主机上执行。推荐先在开发环境生成 bootstrap 页面，再复制到部署主机。

在开发环境进入项目根目录：

```bash
cd /data/hypercdr/hypercdr-platform
```

生成 bootstrap 页面和安装包：

```bash
HCDR_IMAGE_REGISTRY=<DEPLOY_HOST_IP>:5000/hypercdr \
./scripts/package-bootstrap-portal.sh
```

启动 bootstrap 页面：

如果是在部署主机上启动，需要先把 `dist/bootstrap-portal` 复制到部署主机。例如：

```bash
scp -r dist/bootstrap-portal root@<DEPLOY_HOST_IP>:/opt/hypercdr-bootstrap-portal
```

在部署主机启动页面：

```bash
cd /opt/hypercdr-bootstrap-portal
python3 -m http.server 8080 --bind 0.0.0.0
```

访问：

```text
http://<DEPLOY_HOST_IP>:8080
```

页面中的 `Image registry` 应显示或填写：

```text
<DEPLOY_HOST_IP>:5000/hypercdr
```

## 8. 安装中控平台

打开 bootstrap 页面，选择 Docker Compose 单机部署。

建议填写：

```text
Public base URL: http://<DEPLOY_HOST_IP>:8088
Image registry:  <DEPLOY_HOST_IP>:5000/hypercdr
HTTP port:       8088
```

复制页面生成的命令，在部署主机执行。

安装完成后访问：

```text
http://<DEPLOY_HOST_IP>:8088
```

验证中控平台：

```bash
curl http://${HCDR_HOST}:8088/healthz
curl http://${HCDR_HOST}:8088/readyz
```

## 9. 注册 Kubernetes 集群

进入中控平台页面：

```text
Clusters -> Register Cluster
```

按页面步骤操作：

1. 先在 Kubernetes 节点上安装 Harbor 证书信任。
2. 再执行 agent 安装命令。
3. 等待 agent 连接成功。

agent 安装命令会默认使用中控平台安装时填写的 Harbor 镜像库：

```text
<DEPLOY_HOST_IP>:5000/hypercdr/comm-agent:dev
<DEPLOY_HOST_IP>:5000/hypercdr/velero:v1.17.1-helperfix
```

在每个源集群、目标集群都执行一次注册。

验证 agent：

```bash
kubectl -n hypercdr-agent get pods
kubectl -n hypercdr-agent logs deploy/hypercdr-comm-agent
```

验证 Velero：

```bash
kubectl -n hypercdr-agent get pods | grep velero
kubectl -n hypercdr-agent get bsl
```

## 10. 容灾功能验证

建议按以下顺序验证：

1. 在 `Clusters` 页面确认源集群和目标集群均为 online。
2. 进入 DR 第一阶段，等待 namespace inventory 上报。
3. 选择 namespace，进入第二阶段。
4. 配置 DR plan。
5. 在第三阶段执行手动同步。
6. 查看恢复点。
7. 执行 drill，验证恢复到目标集群。

## 11. 常用运维命令

### 11.1 重新生成 Bootstrap 页面

```bash
cd /data/hypercdr/hypercdr-platform

HCDR_IMAGE_REGISTRY=${HCDR_REGISTRY} \
./scripts/package-bootstrap-portal.sh
```

## 12. 注意事项

- 不建议将 Harbor 和中控平台部署在 Kubernetes worker 节点上。
- Harbor 使用自签证书时，所有 Kubernetes 节点都需要信任该证书。
- `Image registry` 必须填写 Harbor 项目前缀，例如 `<DEPLOY_HOST_IP>:5000/hypercdr`，不要只填写 Harbor 主机地址。
- 中控平台安装时使用的镜像库会作为后续 agent 安装的默认镜像库。
- Harbor 由用户自行安装和维护，HyperCDR 只依赖可访问的 Harbor 项目前缀。
- 生产环境应修改 Harbor 默认管理员密码。

## 附录 A. Harbor 安装参考

本附录只作为用户自行安装 Harbor 时的参考，不属于 HyperCDR 安装脚本的一部分。实际生产环境应按企业标准安装和运维 Harbor。

### A.1 下载 Harbor online installer

以下示例使用 Harbor `v2.10.0`：

```bash
cd /opt
export HARBOR_VERSION=v2.10.0
export HARBOR_INSTALLER=harbor-online-installer-${HARBOR_VERSION}.tgz
export HARBOR_DOWNLOAD_URL=https://ghproxy.net/https://github.com/goharbor/harbor/releases/download/${HARBOR_VERSION}/${HARBOR_INSTALLER}
curl -fL --connect-timeout 20 --retry 5 "${HARBOR_DOWNLOAD_URL}" -o "${HARBOR_INSTALLER}"
tar -xzf "${HARBOR_INSTALLER}"
cd /opt/harbor
```

如果该国内加速地址不可用，可以替换为其他企业允许的 GitHub 加速地址，或在可访问 GitHub 的机器下载后上传到部署主机。官方原始地址为：

```text
https://github.com/goharbor/harbor/releases/download/v2.10.0/harbor-online-installer-v2.10.0.tgz
```

### A.2 生成自签名证书

以下示例使用 `<DEPLOY_HOST_IP>` 作为 Harbor 对外访问地址。执行前替换为真实 IP。

```bash
export HARBOR_HOST=<DEPLOY_HOST_IP>
mkdir -p /data/harbor/cert
openssl req -x509 -nodes -newkey rsa:4096 -sha256 -days 36500 -subj "/CN=HyperCDR Harbor CA" -keyout /data/harbor/cert/private.key -out /data/harbor/cert/public.crt
openssl req -newkey rsa:4096 -nodes -subj "/CN=${HARBOR_HOST}" -keyout /data/harbor/cert/server.key -out /data/harbor/cert/server.csr
```

生成证书扩展文件：

```bash
cat >/data/harbor/cert/server.ext <<EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = ${HARBOR_HOST}
IP.1 = ${HARBOR_HOST}
EOF
```

签发 Harbor 服务端证书：

```bash
openssl x509 -req -sha256 -days 36500 -in /data/harbor/cert/server.csr -CA /data/harbor/cert/public.crt -CAkey /data/harbor/cert/private.key -CAcreateserial -out /data/harbor/cert/server.crt -extfile /data/harbor/cert/server.ext
```

### A.3 配置 harbor.yml

复制模板：

```bash
cp harbor.yml.tmpl harbor.yml
```

编辑 `harbor.yml`，关键配置如下：

```yaml
hostname: <DEPLOY_HOST_IP>

http:
  port: 5080

https:
  port: 5000
  certificate: /data/harbor/cert/server.crt
  private_key: /data/harbor/cert/server.key

harbor_admin_password: Harbor12345

data_volume: /data/harbor
```

说明：

- `hostname` 必须是其他主机可以访问到的 IP 或域名。
- `https.port` 推荐使用 `5000`。
- `data_volume` 是 Harbor 数据目录，需保证磁盘空间充足。
- 生产环境必须修改 `harbor_admin_password`。

### A.4 安装并启动 Harbor

```bash
cd /opt/harbor
sudo ./install.sh
```

安装完成后检查容器：

```bash
docker ps
```

验证 Harbor API：

```bash
curl -k https://<DEPLOY_HOST_IP>:5000/api/v2.0/systeminfo
```

### A.5 Docker 信任 Harbor 证书

在需要推送镜像的部署主机上配置 Docker 信任证书：

```bash
sudo mkdir -p /etc/docker/certs.d/<DEPLOY_HOST_IP>:5000
sudo cp /data/harbor/cert/public.crt /etc/docker/certs.d/<DEPLOY_HOST_IP>:5000/ca.crt
sudo systemctl restart docker
```

验证登录：

```bash
docker login <DEPLOY_HOST_IP>:5000
```
