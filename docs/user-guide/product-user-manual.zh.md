# HyperCDR 使用手册

本文按“首次使用—资源准备—保护—验证—恢复—运维”的顺序说明控制台操作。页面名称以当前版本界面为准；不同发行版或后续版本可能增加字段，但不应改变组织隔离和任务确认原则。

## 1. 首次登录

1. 浏览器访问 `https://平台地址:12443/login`。
2. 使用安装时显示的认证方式完成图片验证码或 Turnstile。
3. 使用初始账号 `admin` / `admin123` 登录后立即修改密码。
4. 在 **Settings → Sign-in & Security** 检查会话、密码和绑定邮箱；在 **Personal Information** 设置显示名称和时区。
5. 检查 Dashboard 的平台状态、集群状态和最近任务；首次验证不要直接操作生产 Namespace。

图片验证码为 4 位数字和字母，字母大小写不敏感；验证码一次性使用且过期后应刷新。Turnstile 需要浏览器访问 Cloudflare 控件及 API 出站访问校验服务。

## 2. 用户、组织和权限

HyperCDR 使用组织作为业务隔离边界。一个账号只属于一个组织；集群、存储、策略、应用、任务和恢复点只属于当前组织。

- **平台管理员**：在 Settings 中管理 Users、Organizations、Access & Sign-in、Email Service 和 Platform Upgrade；不因平台身份自动获得其他组织的业务资源访问权。
- **组织管理员**：管理本组织资料、成员和邀请。
- **普通成员**：使用本组织被授权的业务资源。

邀请成员前配置 Email Service。邀请链接和密码重置链接具有效期且只能使用一次；不需要使用时应撤销邀请。临时禁止访问优先使用 Disable，删除账号会撤销会话但不会删除组织业务资源。

## 3. 注册集群 Agent

进入 **Configuration → Clusters**，选择集群类型和注册方式。

### 3.1 Install for me

适用于中控注册执行器能够访问 Kubernetes API 的环境：

1. 上传或选择具备所需权限的 kubeconfig。
2. 按向导检查集群地址、Namespace 和凭据。
3. 执行注册并等待 Agent 部署。
4. 回到集群详情，确认 Agent online、节点和 Namespace 清单已上报。

kubeconfig 是敏感凭据，不应提交到代码仓库、聊天记录或公开工单。注册执行器的网络和权限不足时，使用 Run command。

### 3.2 Run command

在能访问集群 API 的主机执行页面生成的命令。执行完成后检查：

- Agent Pod 已运行；
- Agent 能访问中控 HTTPS/WSS 地址；
- 集群状态从注册中变为 online；
- 节点、Namespace 和工作负载清单已刷新。

注销集群前确认没有仍在运行的保护、恢复或升级任务。注销平台记录不等于删除对象存储中的备份。

## 4. 配置存储和策略

### 4.1 存储

在 **Configuration → Storage** 创建对象存储配置，填写服务地址、Bucket、区域（如适用）、访问凭据和集群侧访问参数。保存后仍需通过真实保护任务验证：

- Agent/集群能解析并访问存储地址；
- 凭据具备所需读写权限；
- TLS、代理和证书链正确；
- Bucket 生命周期和容量符合保留策略。

不要仅以“配置保存成功”判断存储已可用。

### 4.2 保护策略

在 **Configuration → Policies** 创建策略，设置调度、备份频率、保留数量/期限及任务相关选项。策略保存后确认目标集群和存储配置的引用关系，再用测试 Namespace 执行一次任务。

## 5. 创建应用保护

进入 **Disaster Recovery → Application DR**：

1. 选择源集群和需要保护的 Namespace/工作负载。
2. 在保护向导中选择目标集群、存储和策略。
3. 核对资源范围、PVC、排除项、Namespace 映射和恢复目标。
4. 提交保护配置并等待 Agent 接收。
5. 在应用详情确认保护状态、最近任务和恢复点。

多个 Namespace 可以按业务边界独立保护或按恢复一致性要求组合；不要把平台清单中的 Namespace 数量误认为单一应用边界。

## 6. 同步与恢复点

同步任务用于将保护数据和状态推进到目标端/对象存储。任务开始后进入任务列表查看：

- 当前阶段和百分比；
- 已处理资源、对象和数据量；
- 开始时间、更新时间和预计状态；
- Agent、组件或目标端返回的错误；
- 可展开的详细日志。

恢复点应至少核对创建时间、源集群、应用范围、数据完整性和任务最终状态。不要使用仍在创建、已过期或来源不匹配的恢复点执行恢复。

## 7. Drill 容灾演练

Drill 是不影响生产业务的恢复验证流程。执行前：

1. 选择隔离的目标集群/Namespace。
2. 规划资源名称、网络、Ingress、Service 和 PVC 冲突处理。
3. 选择已验证的恢复点。
4. 提交演练后持续观察任务进度和详细日志。

演练完成后验证 Pod/Deployment/StatefulSet、PVC、Service、配置、应用健康检查和实际数据。演练成功只证明本次隔离恢复验证通过，不应直接替代正式恢复或回切审批。演练资源应按组织变更流程清理。

## 8. Restore 正式恢复

正式恢复前执行变更评审：确认目标集群、Namespace、恢复点、资源映射、覆盖策略、凭据和回退方案。提交后：

- 不要重复点击创建相同任务；
- 在任务详情确认排队、执行、验证和完成阶段；
- 发现目标冲突、权限错误或数据异常时先暂停后续变更；
- 以目标工作负载和数据检查作为最终验收，而不是只看 API 返回成功。

恢复完成后记录恢复点、开始/结束时间、目标版本、实际应用验证和遗留告警。

## 9. 任务进度和详细日志

在 **Backup & Recovery Tasks** 或对应容灾/恢复任务页面打开详情。列表状态用于快速筛选，详情用于判断真实结果。重点查看：

- 任务类型、来源和目标；
- 当前阶段、进度和重试次数；
- Agent、Kubernetes、Velero/OADP 和对象存储错误；
- 资源级成功/失败明细；
- 时间线、请求关联标识和详细日志。

日志中的凭据、Token、kubeconfig 和敏感 URL 不得复制到公开工单。排障时保留任务 ID、时间范围、集群版本、Agent 版本和关键错误行。

## 10. 平台升级与日常运维

平台管理员可在 **Settings → Platform Upgrade** 查看发布版本并创建升级任务。任务入队不等于升级完成，必须确认：

1. 候选 API/前端健康；
2. Edge 已切换到新颜色；
3. 当前版本清单已更新；
4. 登录、组织、集群和任务查询正常；
5. Agent 与集群组件版本符合发布要求。

宿主机也可使用：

```bash
sudo systemctl status hypercdr.service hypercdr-upgrade-runner.service --no-pager
cd /var/lib/hypercdr
sudo ./restart-platform.sh
```

升级前备份 PostgreSQL、`.env`、`HCDR_SECRET_KEY`、TLS 文件和版本清单。应用备份在对象存储中独立管理；只有数据库而没有秘密数据保护密钥，不能视为完整平台备份。

## 11. 常见排障顺序

1. 先记录任务 ID、集群、Namespace、恢复点和时间。
2. 检查平台 `/readyz`、Edge、API、前端和 PostgreSQL 容器。
3. 检查集群 Agent online 状态、Pod 日志和到中控的 WSS 连接。
4. 检查 Kubernetes RBAC、组件 Pod、CSI/存储驱动和对象存储连通性。
5. 打开任务详细日志，按第一个失败阶段定位，不要只看最后一条汇总错误。
6. 必要时先用隔离 Namespace 重试；生产恢复前保留原现场和审计信息。

## 12. 集群接入详细流程

### 12.1 注册前检查

准备 kubeconfig、集群 API 可达性、等效 RBAC、对象存储路径、镜像拉取凭据和中控 HTTPS 地址，并选择 `native-kubernetes`、`huaweicloud-cce` 或 `openshift`。kubeconfig 必须内嵌凭据，不能依赖外部 exec 插件或凭据文件，上传大小不超过 1 MiB。

### 12.2 Install for me 与 Run command

**Install for me** 由中控注册执行器访问 kubeconfig 中的集群并安装组件，适合中控能访问集群 API 的拓扑。**Run command** 生成一次性命令，由能访问集群的跳板机或管理员主机执行，适合中控无法直连集群的拓扑。两者最终都建立集群身份和 Agent 出站 WSS；命令中的 Token、kubeconfig 和地址不得公开。

注册后在集群详情确认连接状态、Kubernetes 版本、节点、Namespace、资源清单、Agent 和 Velero/OADP 状态。集群记录存在但 Agent offline，只表示中控记录已创建，不表示安装成功。

## 13. 对象存储、策略和应用范围

在 **Configuration → Storage** 核对名称、类型、endpoint、Bucket、区域、路径前缀、访问密钥和秘密密钥，并分别从中控和 Agent 网络执行测试。保存配置不代表 Agent 能访问；使用明确的集群前缀隔离多个源集群。更换正在使用的仓库应使用 **Reconfigure DR Storage**，不要手工改数据库。

策略应明确名称、调度/频率、保留数量或期限和执行选项。保护配置是策略与具体集群、应用、存储的绑定；编辑策略前检查引用关系。应用范围可按 Namespace、资源类型、标签和资源清单选择，并配置排除项、目标 Namespace、PVC 和存储类映射。应用标签用于分类检索，不改变备份范围。保护配置创建后还需激活或启动，存在配置不等于已经产生恢复点。

## 14. 恢复点内容、保留和删除

恢复点详情包含源集群、保护配置、Velero 备份名、Namespace、创建时间、状态和内容清单。核验 Deployment/StatefulSet、Service、ConfigMap/Secret、PVC、VolumeSnapshot 或数据上传对象。保留设置影响平台保留决策；对象存储生命周期、备份仓库和云端版本控制可能独立保留数据。删除前确认没有任务引用恢复点，并保留审计记录；删除平台记录不等于立即擦除对象存储全部数据。

## 15. 任务状态、事件与重试

任务详情由汇总状态、阶段进度、事件和详细日志组成。排障时记录任务 ID、类型、源/目标集群、保护配置、恢复点和时间；查看任务停在 storage sync、backup、restore、readiness、cleanup 还是组件升级；优先处理第一个失败事件，区分平台调度、Agent、Kubernetes/RBAC、Velero/OADP 和对象存储错误。修复后使用 **Retry**，不要重复创建同一业务任务；取消运行中任务前确认组件支持安全取消，并检查残留资源。

主要任务类型包括 storage-sync、backup、restore、drill、takeover、retention-cleanup、组件升级和 unregister。列表成功只表示任务链路完成，仍要检查资源级结果。

## 16. Drill、Restore 与 Takeover 边界

| 操作 | 目的 | 验收重点 |
|---|---|---|
| Drill | 隔离验证恢复点和链路 | 隔离 Namespace、工作负载、PVC、Service、应用数据 |
| Restore | 正式恢复业务 | 变更审批、覆盖/映射、容量、应用可用性和数据 |
| Takeover | 按容灾流程切换控制权 | 前置校验、源端状态、网络/DNS、回切方案和审计 |

Drill 使用隔离 Namespace 和资源前缀，完成后执行 cleanup。Drill 成功不能直接推断生产 Restore 一定成功。Takeover 属于高风险操作，只在前置校验通过并完成审批后执行。

## 17. 组件升级、注销和平台运维

集群详情提供 Comm Agent 和 Velero 组件升级入口。升级前确认没有备份、恢复、Drill 或注销任务，检查目标版本、镜像仓库、权限和回滚方案；平台升级与组件升级分别验收。注销前执行 unregister precheck，处理运行任务、保护配置和恢复点引用。注销清理中控集群身份，不删除应用 Namespace、Longhorn、外部对象存储或其他命名空间的 Velero 资源。

平台管理员可在 **Users/Organizations** 管理身份和组织，在 **Access & Login** 修改公开 Origin，在 **Email Service** 配置 SMTP，在 **Platform Upgrade** 执行平台升级，并在 **Audit/Diagnostics** 查询真实操作者、目标组织、任务 ID 和时间范围。删除账号会撤销会话，但不删除业务资源；每个组织应保留有效组织管理员。

## 18. 149 实机页面截图

登录入口已在 149 开发环境实际访问核验；集群、策略和任务页面需要有效业务账号及测试数据，因此不使用虚构业务截图。

![HyperCDR 149 开发环境登录页](../assets/images/overview/login-149.png)

