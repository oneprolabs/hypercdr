---
title: 蓝绿部署与回滚
description: 蓝绿切换的组件关系、回滚能恢复什么、不能恢复什么。
---

# 蓝绿部署与回滚

当前拓扑来自根目录 `docker-compose.yml`。

## 组件关系

Edge 在宿主机 12443 提供 TLS，把页面与 API/WebSocket 请求转发给**活动颜色**的前端和 API。蓝、绿各有独立应用网络，共享 PostgreSQL；API 另接入出站网络。注册执行器为独立内部服务。产品文档站独立部署，不在本编排内。

```text
                    ┌─ 蓝色：前端 + API ─┐
Edge :12443 ────────┤                    ├── PostgreSQL
                    └─ 绿色：前端 + API ─┘
                              ↑
                    切换由 deploy-blue-green.sh 控制
```

## 切换过程

`scripts/release/deploy-blue-green.sh`：

1. 启动非活动颜色；
2. 执行健康检查；
3. 切换 Nginx 流量；
4. 检查切换结果；
5. 失败时尝试恢复原流量；
6. 成功后激活对应版本的 `current-release.json`。

平台组件与集群组件属于同一版本清单，但集群升级有独立任务及验收。

## 回滚

在安装目录执行：

```bash
./deploy-blue-green.sh --rollback
```

::: danger 回滚的边界

回滚依赖**保留的上一版本镜像、清单及运行状态**。它切换回旧版本的应用流量，**不是数据库恢复**。

如果升级过程中已经执行了数据库迁移，回滚后旧版本应用面对的是新结构的数据库，可能无法正常运行。这就是升级前必须备份数据库的原因。

:::

## 升级前的检查清单

- [ ] 已备份 PostgreSQL（逻辑备份或停机一致性备份）
- [ ] 已备份 `.env`，特别是 `HCDR_SECRET_KEY`
- [ ] 已备份 TLS 与 Compose 配置
- [ ] 已确认 `releases/` 中上一版本镜像仍在
- [ ] 已确认升级执行器服务正常
- [ ] 已了解集群组件需单独升级

## 外部代理

不依赖外部 Nginx Proxy Manager。若另设代理，代理的生命周期和证书由部署者管理，平台升级不会修改它们。

## 相关

- [安装指南](/docs/deployment/install)
- [配置参考](/docs/deployment/configuration)
- [平台运维与升级](/docs/operations/platform)
- [构建与发布维护](/docs/operations/release)
