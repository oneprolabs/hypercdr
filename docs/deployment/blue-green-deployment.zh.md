# HyperCDR 蓝绿部署运行手册

本部署使用阿里云 ACR、Docker Compose、Nginx Proxy Manager（NPM）和 blue/green
两个 API/前端颜色。公网 HTTPS 由 NPM 终止，HyperCDR 容器之间使用 HTTP；生产
平台版本只通过 GitHub Actions 发布和部署，开发环境仍使用 `docker-compose.dev.yml`。

ACR 和 Docker Hub 使用单仓库、多组件 Tag，例如
`registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr:platform-api-1.0.39.20260916`。
前端使用 `platform-frontend-版本`，PostgreSQL 使用 `postgres-16`；所有组件
都在同一仓库，不再追加 `/platform-api` 等子路径。Harbor 保留项目下多仓库命名。

## GitHub 配置

Repository Variables：

```text
HCDR_ACR_SERVER=registry.cn-beijing.aliyuncs.com
HCDR_IMAGE_REGISTRY=registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr
HCDR_SSH_HOST=47.236.253.138
HCDR_SSH_USER=root
HCDR_SSH_PORT=22
HCDR_DEPLOY_PATH=/root/hypercdr-deploy
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

安全组开放 80、443 给 NPM；22 只允许管理来源。证书配置在 NPM 中。
确认 NPM 和 HyperCDR 使用同一个 Docker 网络。服务器当前网络名是
`nginx-proxy-manager_default`，可用以下命令核对：

```bash
docker network inspect nginx-proxy-manager_default
```

NPM 的 Proxy Host 需设置为 `http://hypercdr-edge:80`，启用 WebSocket 支持。
HyperCDR Compose 不发布宿主机端口。旧部署迁移时先改好 NPM 转发，再在部署
目录 `.env` 设置：

```dotenv
HCDR_PROXY_NETWORK=nginx-proxy-manager_default
HCDR_NPM_UPSTREAM_READY=true
```

生产 PostgreSQL 使用部署目录下 `data/postgres`，且不发布宿主机端口。

## 首次部署

Actions 在 Tag 发布后将 Compose 和部署脚本下载到 `HCDR_DEPLOY_PATH`，然后执行
蓝绿部署。首次切换前先确认 NPM Proxy Host 已指向 `hypercdr-edge:80` 并设置
`HCDR_NPM_UPSTREAM_READY=true`；部署脚本会在重建入口前检查这两个前置条件。

检查：

```bash
docker compose --project-name hypercdr \
  --env-file /root/hypercdr-deploy/.env \
  -f /root/hypercdr-deploy/docker-compose.yaml \
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
/root/hypercdr-deploy/deploy-blue-green.sh 1.0.33.20260916
```

脚本会拉取非活动颜色、运行迁移、检查 `/readyz`、reload Nginx、观察公网健康
状态，然后保留旧颜色一段时间再停止它。

## 回滚

```bash
/root/hypercdr-deploy/deploy-blue-green.sh --rollback
```

回滚要求旧颜色的镜像和数据库结构仍然兼容。破坏性数据库迁移不能依靠蓝绿
切换自动恢复。

## 重启和停止

```bash
systemctl status hypercdr --no-pager
systemctl restart hypercdr
/root/hypercdr-deploy/stop-platform.sh
/root/hypercdr-deploy/start-platform.sh
```

这些命令不会删除 PostgreSQL 数据卷。

## 注意事项

- `/ws/agent` 是长连接；HTTP 切换无停机，但 Agent 可能在旧颜色停止时重连。
- API 迁移必须遵循 expand/contract，不能在旧版本仍运行时删除字段。
- 第一阶段不运行 `platform-upgrader`，也不提供旧的平台 Upgrade 创建入口；平台版本统一通过 GitHub Actions 发布 Tag 进行蓝绿部署。集群组件升级仍按现有流程执行。
- 不要把 `.env`、数据库密码、证书私钥或 ACR 密码提交到 Git。
