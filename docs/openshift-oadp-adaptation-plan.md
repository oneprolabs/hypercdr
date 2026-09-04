# HyperCDR OpenShift OADP 适配方案

## 1. 结论与选型

OpenShift 4.14、4.15 使用 OADP 1.3.10 社区构建链路。HyperCDR 不直接在 OpenShift
安装原生 Kubernetes 使用的 Velero Deployment，而是安装自有精简 OLM Catalog，
由 OADP Operator 创建和维护 Velero、Node Agent 与插件。

第一阶段限定：Linux AMD64、S3/S3-compatible、RWO PVC、File System Backup、Kopia、
全量 namespace 备份和完整恢复/Drill。不开启 CSI snapshot/Data Mover，不包含
OpenShift Virtualization。

此方案使用 Apache 2.0 开源代码和公开 `quay.io/konveyor` 社区镜像，不要求 Red Hat
pull secret，但不宣称获得 Red Hat 官方镜像认证或商业支持。

## 2. 需要管理的镜像

### 第一阶段运行必需

| 发布组件键 | 上游公开镜像 | 用途 |
|---|---|---|
| `oadp-operator` | `quay.io/konveyor/oadp-operator:oadp-1.3` | 解析 DPA 并部署 OADP |
| `oadp-velero` | `quay.io/konveyor/velero:oadp-1.3` | OADP 配套 Velero；Node Agent 使用同一镜像的不同命令 |
| `oadp-openshift-plugin` | `quay.io/konveyor/openshift-velero-plugin:oadp-1.3` | OpenShift 资源恢复适配 |
| `oadp-aws-plugin` | `quay.io/konveyor/velero-plugin-for-aws:oadp-1.3` | S3/S3-compatible 对象存储插件 |
| `oadp-restore-helper` | `quay.io/konveyor/velero-restore-helper:oadp-1.3` | 文件系统恢复辅助容器 |
| `oadp-comm-agent` | HyperCDR 构建 | 与中控通信、创建/观察 OADP CR |
| `oadp-bundle` | HyperCDR 从固定 OADP 1.3.10 CSV 构建 | OLM CRD、CSV 和安装元数据 |
| `oadp-catalog` | HyperCDR 从上述 Bundle 构建 | 仅包含 OADP 1.3.10 的精简 OLM Catalog |

Kopia 能力包含在 OADP Velero/Node Agent 运行路径中，第一阶段不需要独立 Kopia
镜像。Node Agent 也不是单独镜像。

### 可运维但不阻塞第一阶段

| 发布组件键 | 上游公开镜像 | 用途 |
|---|---|---|
| `oadp-must-gather` | `quay.io/konveyor/oadp-must-gather:oadp-1.3` | OADP 故障信息采集 |
| `oadp-cli` | `quay.io/konveyor/oadp-cli-binaries:oadp-1.3` | OpenShift Console CLI 下载能力 |

### 第一阶段明确不纳入

- `velero-plugin-for-csi`：当前只做 FSB + Kopia，不使用 CSI 快照。
- `kubevirt-velero-plugin`：当前不支持 OpenShift Virtualization。
- Azure/GCP 插件：当前对象存储只支持 S3/S3-compatible。

## 3. 镜像供应链

1. 固定 OADP `oadp-1.3` 分支中声明 1.3.10 的 Git commit。
2. 对所有公开上游镜像解析平台为 `linux/amd64` 的 manifest digest。
3. 按 digest 拉取，推送到 HyperCDR 阿里云仓库的版本化、不可变 tag。
4. 生成 Operator CSV，将所有 `RELATED_IMAGE_*` 改为阿里云镜像地址。
5. 构建并推送 `oadp-bundle:1.3.10-hcdr.N`。
6. 使用 `opm` 构建只包含一个 package/channel/version 的
   `oadp-catalog:1.3.10-hcdr.N`。
7. 对 Catalog 执行 `opm render`，验证所有运行镜像 host 都属于阿里云仓库。
8. 将镜像地址和远端 digest 写入 HyperCDR 不可变发布清单；缺少任何必需组件时
   禁止激活发布。

不得直接在安装清单中保留 `quay.io/konveyor/*:oadp-1.3`，也不得覆盖已经发布的
tag。

## 4. OpenShift 注册和部署编排

1. 用户选择集群类型 `openshift`，上传 cluster-admin kubeconfig。
2. 预检 OpenShift 版本只能为 4.14/4.15，架构为 Linux AMD64；检查 OLM、SCC、
   VolumeSnapshot API（仅报告，不作为 FSB 阻塞条件）、默认 StorageClass、阿里云
   Registry、S3 endpoint 和中控 WSS 连通性。
3. 在 `openshift-adp` 创建阿里云 pull secret、CatalogSource、OperatorGroup 和
   Subscription，等待 CSV `Succeeded`。
4. 创建 S3 credentials Secret 和 DPA。DPA 使用 `openshift`、`aws` 默认插件，
   `nodeAgent.enable=true`、`uploaderType=kopia`、`defaultVolumesToFSBackup=true`，不创建
   snapshot location。
5. 等待 DPA Reconciled、Velero Deployment Ready、Node Agent DaemonSet 全节点 Ready、
   BSL `Available`。
6. 安装独立 `oadp-comm-agent`，由它复用现有 HyperCDR WSS、任务和状态协议。

## 5. HyperCDR 任务适配

- Backup：继续创建 Velero `Backup` CR，第一阶段只允许全量 namespace，默认 FSB。
- Restore/Drill：创建 Velero `Restore` CR；恢复前执行 CRD/Operator 依赖预检与
  StorageClass 映射。
- Schedule：继续使用 Velero `Schedule` CR。
- 删除与保留：同时处理 Backup、PodVolumeBackup/Restore、DataUpload/DataDownload。
- 状态：观察 DPA、BSL、Backup、Restore 和 Node Agent；不以命令提交成功代替任务成功。
- OpenShift 特殊资源：Route、DeploymentConfig、BuildConfig、ImageStream 等 API 对象可
  被 Velero 发现；恢复可用性必须进入兼容矩阵。内部 Registry 的镜像层不随 ImageStream
  对象自动迁移。

## 6. Upgrade 页面和升级策略

页面分组展示：

- Kubernetes：Comm-agent、Kubernetes Velero-agent、对应对象存储插件。
- OpenShift：OADP Comm-agent、OADP Operator、OADP Velero-agent、OpenShift Plugin、
  OADP AWS Plugin、Restore Helper、Bundle、Catalog。
- 可选诊断：Must Gather、CLI。

页面显示产品版本（例如 OADP `1.3.10`、Velero实际版本），完整镜像 tag/digest 在详情中
显示。`Not included` 只表示活动发布清单确实缺少该组件；正式发布的必需组件不得出现
该状态。

OADP 升级必须以新 Bundle/Catalog 版本完成，通过 Subscription/InstallPlan 升级；不能
像原生 Kubernetes Velero 一样直接 patch OADP 管理的 Deployment。

## 7. 验收标准

- 所有必需镜像仅从阿里云仓库拉取，断开 Quay/Red Hat Registry 后仍可完成注册。
- Catalog render 后不包含非阿里云运行镜像引用。
- OpenShift 4.14、4.15 各完成一次：注册、S3 BSL、RWO/Kopia 全备份、跨集群恢复、Drill、
  保留清理、卸载、重装、OADP 升级。
- Route、DeploymentConfig、BuildConfig、ImageStream、CR/CRD、SCC/ClusterRole 等资源形成
  明确兼容矩阵。
- Upgrade 页面中的所有第一阶段必需组件均有版本、镜像和 digest，不出现
  `Not included`。

## 8. 已核实资料

- Red Hat OpenShift 4.14/4.15 Backup and Restore 文档均包含 OADP 1.3、S3-compatible、
  Kopia/FSB、跨集群恢复说明。
- OADP 开源仓库 `oadp-1.3` 分支 CSV 声明版本 `1.3.10`，并列出全部
  `RELATED_IMAGE_*`。
- 当前环境已匿名验证上述 `quay.io/konveyor` 第一阶段镜像 manifest 可访问。

