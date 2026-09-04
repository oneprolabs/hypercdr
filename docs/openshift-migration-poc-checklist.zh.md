# HyperCDR OpenShift 应用迁移 POC 清单

适用范围：OpenShift 4.14/4.15、Linux AMD64、多节点集群；使用
HyperCDR、独立 `oadp-comm-agent`、OADP 1.3、Node Agent 和 Kopia；第一阶段
仅验证全量备份、RWO PVC恢复及完整 Drill。

## 1. 源集群信息采集

- [ ] OpenShift版本为4.14或4.15，并记录完整补丁版本。
- [ ] 所有 Master和 Worker节点为 Linux AMD64，操作系统为 RHCOS或 RHEL。
- [ ] 提供可访问集群 API的 kubeconfig，账号具备 `cluster-admin`。
- [ ] 记录 API地址、集群名称、节点数量及节点 Ready状态。
- [ ] 确认集群未安装 OADP；HyperCDR第一阶段不复用已有 OADP。
- [ ] 记录待迁移 Namespace及其 PVC、容量、AccessMode和 StorageClass。
- [ ] 确认待迁移 PVC全部为 `ReadWriteOnce`；RWX、ROX暂不纳入本次 POC。
- [ ] 记录应用引用的镜像及拉取 Secret，确认目标集群能够获得这些镜像。
- [ ] 记录集群级依赖，例如 CRD、ClusterRole、SCC、Operator和外部服务；用于判断
  全量恢复后是否仍需人工安装或配置，不作为第一阶段资源过滤条件。

## 2. 目标集群条件

- [ ] OpenShift版本为兼容矩阵中的4.14或4.15。
- [ ] 所有 Master和 Worker节点为 Linux AMD64，操作系统为 RHCOS或 RHEL。
- [ ] 提供可访问集群 API的 kubeconfig，账号具备 `cluster-admin`。
- [ ] 未安装 OADP；注册时由 HyperCDR自动安装。
- [ ] 目标 Namespace可创建，或确认与现有 Namespace不存在名称和资源冲突。
- [ ] 目标集群具备恢复工作负载所需的 CRD、Operator、SCC及外部依赖。
- [ ] 目标集群可以拉取应用镜像以及阿里云中的 HyperCDR/OADP镜像。

## 3. 对象存储条件

- [ ] 第一阶段使用 S3或 S3-compatible对象存储，仅作为备份中转仓库。
- [ ] 提供 Endpoint、Bucket、Region、Prefix、Access Key、Secret Key及 TLS要求。
- [ ] 源集群和目标集群均可解析并访问对象存储 Endpoint。
- [ ] 凭证对指定 Bucket/Prefix具备读、写、列举和删除权限。
- [ ] Bucket容量满足应用资源和 PVC全量数据，并预留重试及 Drill空间。
- [ ] 源、目标集群使用同一仓库时，确认 Prefix规划不会与其他 POC或生产任务冲突。
- [ ] 对象存储使用私有 CA时，提供完整 CA证书链。

## 4. 网络条件

- [ ] 源、目标集群到中控平台：TCP 3002（HTTPS、API和 WSS共用）。
- [ ] 使用外部网关时，集群访问 TCP 443，由网关转发至中控3002，并支持
  WebSocket Upgrade和长连接。
- [ ] 中控平台到源、目标集群 API：通常 TCP 6443；若中控出站默认允许，无需
  单独配置出站规则。
- [ ] 源、目标集群到阿里云镜像仓库：TCP 443。
- [ ] 源、目标集群到对象存储：通常 TCP 443；使用 HTTP时按实际端口放通。
- [ ] DNS能够解析中控平台、集群 API、阿里云镜像仓库和对象存储域名。
- [ ] 中控平台入站默认开放 TCP 3002；SSH运维按需开放 TCP 22并限制来源网段。

## 5. 目标存储条件

- [ ] 目标集群存在可动态创建 RWO PVC的 CSI StorageClass。
- [ ] 记录源 StorageClass到目标 StorageClass的一一映射。
- [ ] 目标 StorageClass支持应用要求的容量、VolumeMode和文件系统。
- [ ] 目标存储可用容量满足所有待恢复 PVC，并预留 Drill副本空间。
- [ ] CSI Provisioner、节点插件和挂载能力在目标 Worker节点均为健康状态。

## 6. 部署条件

- [ ] HyperCDR中控平台可用，默认入口为 `https://<host>:3002`。
- [ ] 发布清单包含 `oadp-comm-agent`和经过校验的 OADP Catalog镜像及摘要。
- [ ] OADP Operator、Velero、Node Agent、Kopia和 AWS/S3插件相关镜像已全部
  同步至指定阿里云镜像命名空间，Catalog不得回退到 Red Hat公共仓库。
- [ ] 私有阿里云仓库的拉取凭证已配置，并能被 Catalog、Operator和 OADP工作负载使用。
- [ ] 若阿里云仓库为私有仓库，已按 OpenShift集群安全规范配置全局 Pull Secret或等效
  镜像访问策略；`openshift-adp` 和 `openshift-marketplace` 中的命名空间 Secret仅是
  安装流程的补充，不能替代集群级凭证。
- [ ] 中控平台具备注册执行器，且能访问源、目标集群 API。
- [ ] 集群与中控平台时间同步，证书有效且系统时钟偏差可接受。

## 7. POC验收记录

- [ ] 源、目标 OpenShift集群均成功注册并保持 Agent在线。
- [ ] OADP Operator、DPA、Velero和所有目标 Worker上的 Node Agent均 Ready。
- [ ] 源、目标 BackupStorageLocation均为 Available。
- [ ] 对一个包含 RWO PVC的应用完成全量备份，Kopia数据成功写入对象存储。
- [ ] 使用 StorageClass映射在目标集群完成完整恢复。
- [ ] Pod、Service、配置、Secret和 PVC数据检查通过，应用健康检查通过。
- [ ] 完成一次完整 Drill，任务状态、事件和恢复点记录与现有流程一致。
- [ ] 记录 Catalog、CSV、DPA、Velero、Node Agent及 Agent实际就绪耗时，用于校准
  当前暂定安装超时值。

## 8. 推荐执行顺序

1. 分别采集源、目标集群信息，确认版本、节点 OS/架构、权限、StorageClass和 PVC
   AccessMode均满足本清单。
2. 准备阿里云镜像仓库、OADP Catalog和 S3对象存储，并从源、目标集群分别验证拉取
   镜像及访问对象存储。
3. 先注册目标集群，再注册源集群；两端均选择 OpenShift，使用中控生成的注册流程。
4. 在中控存储配置中创建 S3/S3-compatible仓库，确认源、目标两端 BSL均为 Available。
5. 为源应用创建全量保护任务，检查 Backup CR、Kopia BackupRepository、对象存储
   制品和任务事件。
6. 选择目标集群执行完整恢复，检查 Namespace、工作负载、Service、Secret、PVC及
   应用数据。
7. 执行完整 Drill并保存任务详情、事件、恢复点和耗时，最后填写兼容矩阵和问题记录。

## 暂不纳入第一阶段

- 资源类型、标签或对象级过滤备份。
- 部分资源或部分制品恢复。
- RWX、ROX PVC。
- Azure、GCP等非 S3对象存储插件。
- 自动复用或自动卸载集群中已有的 OADP。
- 对 Operator/BuildConfig等 OpenShift特有资源作超出 OADP实际恢复能力的额外编排。
