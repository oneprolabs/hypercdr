# HyperCDR 文档

以下入口以当前代码和流程为依据。公开版本及安装包见 [GitHub Releases](https://github.com/oneprolabs/hypercdr/releases)。

## 安装与使用

1. [安装部署](deployment/deployment-guide.zh.md)
2. [首次使用：注册、保护、备份与恢复](user-guide/first-use.zh.md)
3. [配置参考](deployment/configuration.zh.md)
4. [平台运维、升级与卸载](operations/platform.zh.md)
5. [故障排查](operations/troubleshooting.zh.md)
6. [部署与适配范围](deployment/support-scope.zh.md)

## 系统与开发

- [当前架构](architecture/control-plane-architecture.zh.md) / [English architecture](architecture/control-plane-architecture.md)
- [数据库与迁移](database/schema.md)
- [集群注册](architecture/cluster-registration-v2.md)
- [蓝绿部署](deployment/blue-green-deployment.zh.md)
- [GitHub 发布流程](deployment/release-flow.zh.md)
- [源码构建与容器开发](deployment/build-release-install.md)
- [开发与真实数据库测试](development/platform.md)
- [实现边界](development/architecture.md)
- [源码产物规则](development/repository-artifacts.md)
- [贡献指南](../CONTRIBUTING.md)

## 专项参考

- [Agent](agent/agent-design.md)、[WebSocket](protocols/websocket.md)、[消息](protocols/platform-agent-messages.md)、[任务](protocols/platform-agent-tasks.zh.md)
- [DR readiness](architecture/dr-readiness.md)、[升级验收](architecture/upgrade-lifecycle.md)
- [CCE 兼容性](deployment/huawei-cce-compatibility.md)、[OpenShift OADP](openshift-oadp-phase1.md)、[OpenShift 验证清单](openshift-migration-poc-checklist.zh.md)
- [离线集群清理](agent-offline-uninstall.md)、[日志设计](operations/logging-design.md)、[恢复缓存](operations/velero-restore-cache.zh.md)

专项适配范围应结合文档中的限制及实际验证，不等同于所有版本、驱动或环境均已通过认证。

- [用户、组织与平台权限](user-guide/users-and-organizations.zh.md)：设置菜单、组织归属、平台管理范围与会话一致性。
