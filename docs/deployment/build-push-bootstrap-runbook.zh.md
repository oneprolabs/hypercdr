# HyperCDR 构建、推送、Bootstrap 部署操作手册

本文记录当前标准流程：发布脚本只负责构建和推送镜像；中控平台安装部署通过 bootstrap 页面完成；后续升级由中控平台 UI 手动触发。

## 当前环境

| 项目 | 值 |
| --- | --- |
| 源码目录 | `/data/hypercdr` |
| Harbor | `https://192.168.8.149:5001` |
| 镜像项目前缀 | `192.168.8.149:5001/hypercdr` |
| bootstrap 页面 | `http://192.168.8.149:8080` |
| 中控平台部署目录 | `/data/hypercdr/deploy` |
| 中控平台访问地址 | `https://192.168.8.149:3002` |

## 1. 构建并推送镜像

编辑发布配置：

```bash
cd /data/hypercdr/scripts/release
vi release.conf
```

当前 149 配置：

```bash
HCDR_IMAGE_REGISTRY=192.168.8.149:5001/hypercdr
HCDR_HARBOR_SERVER=192.168.8.149:5001
HCDR_HARBOR_USERNAME=admin
HCDR_HARBOR_PASSWORD_FILE=/data/harbor/harbor_admin_password
HCDR_BUILD_GOPROXY=https://goproxy.cn,direct
HCDR_BUILD_NPM_REGISTRY=https://registry.npmmirror.com
HCDR_RELEASE_SKIP_TESTS=false
```

执行一键构建和推送：

```bash
./release-all.sh v20260714.5 --config ./release.conf
```

该脚本会执行：

```text
docker login
build-release.sh
push-release.sh
docker pull 验证
```

该脚本不会安装或升级中控平台。

## 2. 刷新 Bootstrap 页面安装包

发布新版本后，使用 Bootstrap 发布脚本指定镜像 tag。页面模板的初始 tag 在：

```text
/data/hypercdr/bootstrap/site/assets/app.js
```

`release-bootstrap.sh` 只在外部发布副本中替换该 tag，不修改模板源码。

重新生成页面下载包：

```bash
cd /data/hypercdr/bootstrap
./release-bootstrap.sh v20260714.5
```

该脚本不修改 Bootstrap 源码。临时文件写入 `/data/hypercdr/.build/bootstrap`，
可发布页面写入 `/data/hypercdr/bootstrap-publish`。

刷新 bootstrap portal：

```bash
/data/hypercdr/bootstrap/portal/install-bootstrap-portal.sh \
  --source-dir /data/hypercdr/bootstrap-publish \
  --data-dir /data/hypercdr/bootstrap-portal \
  --port 8080 \
  --execute
```

## 3. 卸载当前中控平台

保留数据卸载：

```bash
/data/hypercdr/bootstrap/uninstall-platform.sh \
  --data-dir /data/hypercdr/deploy \
  --execute
```

全新安装测试时清理数据：

```bash
/data/hypercdr/bootstrap/uninstall-platform.sh \
  --data-dir /data/hypercdr/deploy \
  --purge-data \
  --execute
```

该脚本只移除：

```text
hypercdr-platform-frontend
hypercdr-platform-api
hypercdr-postgres
```

它不会卸载 Harbor，也不会停止 bootstrap portal。

## 4. 通过 Bootstrap 页面部署

打开：

```text
http://192.168.8.149:8080
```

选择 `Standalone Host / Docker Compose deployment`，确认：

```text
Public base URL: https://192.168.8.149:3002
Image registry:  192.168.8.149:5001/hypercdr
Image tag:       v20260714.5
```

按页面生成命令执行三步。

也可以在命令行按页面流程执行：

```bash
rm -rf /tmp/hypercdr-bootstrap-flow
mkdir -p /tmp/hypercdr-bootstrap-flow
cd /tmp/hypercdr-bootstrap-flow

curl -fsSL http://192.168.8.149:8080/releases/dev/hypercdr-bootstrap.tar.gz \
  -o hypercdr-bootstrap.tar.gz
mkdir -p hypercdr-bootstrap
tar -xzf hypercdr-bootstrap.tar.gz -C hypercdr-bootstrap --strip-components=1
chmod +x hypercdr-bootstrap/*.sh
cd hypercdr-bootstrap

./prepare-docker-registry.sh --registry 192.168.8.149:5001/hypercdr
./check-harbor.sh --registry 192.168.8.149:5001/hypercdr --image-tag v20260714.5
./install-platform.sh docker \
  --public-base-url https://192.168.8.149:3002 \
  --data-dir /data/hypercdr/deploy \
  --registry 192.168.8.149:5001/hypercdr \
  --image-tag v20260714.5 \
  --tls-cert-file /data/hypercdr/certs/platform.crt \
  --tls-key-file /data/hypercdr/certs/platform.key \
  --execute
```

安装后标准目录：

```text
/data/hypercdr/deploy/.env
/data/hypercdr/deploy/docker-compose.yaml
/data/hypercdr/deploy/certs/registry-ca.crt
/data/hypercdr/deploy/data/postgres
```

## 5. 验证

验证平台：

```bash
/data/hypercdr/scripts/release/verify-platform.sh \
  --host 192.168.8.149 \
  --registry 192.168.8.149:5001/hypercdr \
  --deploy-dir /data/hypercdr/deploy
```

检查容器：

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}' \
  | grep hypercdr
```

检查 agent 安装脚本：

```bash
curl -kfsS https://192.168.8.149:3002/install.sh | head -20
```

应看到：

```text
AGENT_IMAGE="192.168.8.149:5001/hypercdr/comm-agent:v20260714.5"
VELERO_IMAGE="192.168.8.149:5001/hypercdr/velero:v1.17.1-helperfix"
```

## 6. 本次已验证结果

2026-07-14 已验证完整流程：

```text
release-all.sh v20260714.5: 成功
Harbor push/pull: 成功
bootstrap 页面命令生成: 成功
uninstall-platform.sh --purge-data: 成功
bootstrap Docker Compose 安装: 成功
verify-platform.sh: 成功
HTTPS/WSS 和 Playwright 页面验证: 成功
```
