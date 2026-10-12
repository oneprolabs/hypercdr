# 部署与适配范围

本页区分代码入口与环境验收。存在实现不意味着所有版本组合已经验证。

| 项目 | 当前入口与限制 |
|---|---|
| 独立主机中控 | GitHub Release 在线安装器，Docker Compose 蓝绿 API/前端，共享 PostgreSQL |
| 官网 | 可选独立网站容器，默认 12445；中控默认 12443 |
| Kubernetes 中控 | 仓库有 Chart 资产；不是在线安装器或当前蓝绿生命周期的交付路径 |
| Native Kubernetes | 平台注册页面及生成的安装脚本，使用社区 Velero 路径 |
| Huawei CCE | 独立 provider 检查，见 CCE 专项范围；不能据此推断 CCE Autopilot 或所有节点架构可用 |
| OpenShift | OADP 专用组件与安装路径，见专项范围和逐项验证清单 |
| 对象存储 | UI/后端有多种存储配置；具体可用性取决于集群侧插件、网络、凭据与真实备份恢复验证 |
| 社区/企业扩展 | 当前仓库提供公开扩展边界；企业扩展设计不等同于社区版自带所有企业实现 |

不要把开发环境测试、专项 POC 清单或历史镜像地址当成公开支持承诺。正式环境记录实际平台版本、组件 manifest、集群版本、节点架构、存储驱动和完整备份恢复结果。

参见 [CCE](huawei-cce-compatibility.md)、[OpenShift](../openshift-oadp-phase1.md)、[OpenShift 验证清单](../openshift-migration-poc-checklist.zh.md)、[当前架构](../architecture/control-plane-architecture.md)。
