# 蓝绿部署

当前拓扑来自根目录 `docker-compose.yml`。Edge 在宿主机 12443 提供 TLS，将页面与 API/WebSocket 请求转发给活动颜色的前端和 API。蓝、绿各有独立应用网络，共享 PostgreSQL；API 另接入出站网络。注册执行器为独立内部服务。可选官网独立占用 12445。

`scripts/release/deploy-blue-green.sh` 启动非活动颜色、执行健康检查、切换 Nginx 流量并检查切换结果；失败时尝试恢复原流量。成功后激活对应版本的 `current-release.json`。平台组件与集群组件属于同一版本清单，但集群升级有独立任务及验收。

安装目录中可运行 `./deploy-blue-green.sh --rollback`。回滚依赖保留的上一版本镜像、清单及运行状态，不是数据库恢复；升级前需要备份数据库和运行配置。

不依赖外部 Nginx Proxy Manager；若另设代理，代理生命周期和证书由部署者管理。安装、发布、配置和运维分别参见[安装指南](deployment-guide.zh.md)、[发布指南](release-flow.zh.md)、[配置参考](configuration.zh.md)、[运维指南](../operations/platform.zh.md)。
