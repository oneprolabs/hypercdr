# HyperCDR 蓝绿部署运行手册

本部署使用阿里云 ACR、Docker Compose、稳定 Nginx 边缘代理和 blue/green
两个 API/前端颜色。生产平台版本只通过 GitHub Actions 发布和部署；开发环境
仍使用 `docker-compose.dev.yml`。

## GitHub 配置

Repository Variables：

```text
HCDR_ACR_SERVER=crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com
HCDR_IMAGE_REGISTRY=crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr
HCDR_SSH_HOST=47.236.253.138
HCDR_SSH_USER=root
HCDR_SSH_PORT=22
HCDR_DEPLOY_PATH=/var/lib/hypercdr
HCDR_DOMAIN=hypercdr.com
HCDR_AUTO_DEPLOY=false
```

Repository Secrets：

```text
REGISTRY_USERNAME
REGISTRY_PASSWORD
SSH_PRIVATE_KEY
```

先保持 `HCDR_AUTO_DEPLOY=false`。首次人工部署和一次蓝绿切换、回滚都验证
通过后，才改为 `true`。

## 服务器首次准备

确认 DNS：

```text
hypercdr.com A 47.236.253.138
```

安全组开放 80、443；22 只允许管理来源。使用已有有效证书，或在部署前为
首次部署会在部署目录自动生成自签名证书；如已有正式证书，也可以通过参数显式指定。

服务器当前的 `hypercdr-dev-postgres` 不需要先删除；生产 PostgreSQL 使用
`/var/lib/hypercdr/data/postgres`，且不发布宿主机端口。

## 手动首次部署

在本地构建并推送版本后，把仓库中的发布脚本和 Compose 文件放到服务器，执行：

```bash
cd /root/hypercdr
./scripts/release/install-blue-green.sh 1.0.32.20260915 \
  --base-url https://hypercdr.com \
  --domain hypercdr.com \
  --registry crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr \
  --install-dir /var/lib/hypercdr \
  --execute
```

检查：

```bash
docker compose --project-name hypercdr \
  --env-file /var/lib/hypercdr/.env \
  -f /var/lib/hypercdr/docker-compose.yaml \
  --profile blue --profile green ps
curl -fsS https://hypercdr.com/readyz
```

首次安装默认使用 blue。只有 edge、PostgreSQL、blue API、blue 前端和单实例
registration executor 应处于运行状态；green 此时是待部署颜色。

## 发布和切换

创建版本 Tag：

```text
v1.0.33.20260916
```

Actions 会构建并推送去掉 `v` 后的 ACR 镜像标签
`1.0.33.20260916`。启用自动部署后，Actions 会在服务器执行：

```bash
/var/lib/hypercdr/deploy-blue-green.sh 1.0.33.20260916
```

脚本会拉取非活动颜色、运行迁移、检查 `/readyz`、reload Nginx、观察公网健康
状态，然后保留旧颜色一段时间再停止它。

## 回滚

```bash
/var/lib/hypercdr/deploy-blue-green.sh --rollback
```

回滚要求旧颜色的镜像和数据库结构仍然兼容。破坏性数据库迁移不能依靠蓝绿
切换自动恢复。

## 重启和停止

```bash
systemctl status hypercdr --no-pager
systemctl restart hypercdr
/var/lib/hypercdr/stop-platform.sh
/var/lib/hypercdr/start-platform.sh
```

这些命令不会删除 PostgreSQL 数据卷。

## 注意事项

- `/ws/agent` 是长连接；HTTP 切换无停机，但 Agent 可能在旧颜色停止时重连。
- API 迁移必须遵循 expand/contract，不能在旧版本仍运行时删除字段。
- 第一阶段不运行 `platform-upgrader`，也不提供旧的平台 Upgrade 创建入口；平台版本统一通过 GitHub Actions 发布 Tag 进行蓝绿部署。集群组件升级仍按现有流程执行。
- 不要把 `.env`、数据库密码、证书私钥或 ACR 密码提交到 Git。
