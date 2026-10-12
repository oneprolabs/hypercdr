# 当前中控架构

中控由 Go API、React 前端、PostgreSQL、HTTPS Edge、注册执行器及宿主机升级执行器组成。根目录 `docker-compose.yml` 是正式运行拓扑的依据。

Edge 在 12443 终止 TLS，转发到活动蓝/绿前端和 API。PostgreSQL 共享持久化业务状态，API 接入出站网络。可选官网独立使用 12445，不与中控前端混用。

集群 Agent 日常通过出站 WSS 接入平台，采集资源并执行持久化任务。Install for me 是独立注册过程，注册执行器需要使用 kubeconfig 访问集群 API；Run command 则在具备集群网络访问的主机执行安装。

平台以 GitHub Release manifest 交付组件版本，安装和成功升级激活本地清单。升级执行器读取平台任务后调用蓝绿脚本；集群组件升级仍需独立执行和确认。数据库迁移不能通过流量回滚自动撤销。

PostgreSQL 是持久化实现，没有 MemoryStore 回退。用户主题偏好、任务、资源和版本信息均来自持久化数据。原生 Kubernetes/CCE 与 OpenShift 分别使用 Velero 和 OADP 安装路径；项目保留构建所需的 Velero 源码。

详细组件关系见[架构说明](control-plane-architecture.md)，安装见[安装指南](../deployment/deployment-guide.zh.md)，存储结构见[数据库说明](../database/schema.md)。
