# HyperCDR 项目恢复与现状梳理

> 更新时间：2026-09-02（Asia/Shanghai）
>
> 来源：已恢复的 2026-09-01 Codex 会话、当前仓库状态及本机运行状态。
>
> 说明：本文记录事实、证据与未闭环事项；“已实现”不等于已提交、已推送或已在生产环境验证。

## 1. 项目目标

HyperCDR 是面向 Kubernetes 的容灾控制平面，当前工作的两条主线是：

1. 规范化集群注册：Native Kubernetes 与 Huawei Cloud CCE 均支持平台直连注册和用户执行命令注册。
2. 统一版本治理：一个 HyperCDR 平台版本携带一份完整、不可变的组件 Manifest；平台升级成功后，该 Manifest 成为新集群安装、现有集群组件升级的唯一目标来源。

## 2. 当前仓库与运行状态

### 代码仓库

| 仓库 | 当前状态 |
|---|---|
| `/data/hypercdr-main` | `main` 相对 `github/main` ahead 6；存在大量未提交修改及新增迁移 |
| `/data/hypercdr-enterprise` | `main` 与远端一致；存在本次未提交修改；`community.lock` 指向 Community `65ea692` |

未提交修改属于当前工作成果和用户已有修改，严禁用 reset/checkout 覆盖。

### 149 开发环境

- Community：HTTPS `https://127.0.0.1:3002`，API `18080`。
- Enterprise：HTTPS `https://127.0.0.1:3102`，API `19080`。
- Bootstrap：`8080`。
- 两个 API `/readyz` 返回 200；两个 HTTPS 前端返回 200。
- 正式 `/var/lib/hypercdr` Compose 仍是旧部署形态，不能作为新 Registration Executor/整包升级链路的证据。

已关闭的 116 与 CCE 不应再次访问，除非用户明确要求并确认环境已恢复。

## 3. 已实现设计

### 3.1 集群注册

- UI 先选择 Cluster type，再选择 Registration method。
- Native 与 CCE 的 direct 流程共用上传 API，但安装逻辑、预检和命令参数保持 provider 解耦。
- 生成命令显式携带 `--cluster-type`。
- CCE 支持 kubeconfig/context 选择及 StorageClass 选择；无默认 StorageClass 时从 `/dev/tty` 交互选择，无终端则明确要求 `--storage-class`。
- kubeconfig 仅临时使用；成功、失败、取消和 session 过期均删除，API 启动及运行期 janitor 清理重启后遗留目录。
- 注册任务具备阶段门禁、有限重试、幂等和失败回滚；验收以平台持久化任务状态为准。

### 3.2 统一发布 Manifest

- 新增 `platform_releases.component_manifest` JSONB（迁移 `000037_platform_release_manifest.sql`）。
- Manifest 覆盖平台 API、前端、升级器、Registration Executor、comm-agent、Velero/Node Agent 及 AWS/Azure/GCP 插件。
- 同一版本的 Manifest 不可变；重复登记不得改写已登记清单。
- 新集群安装、Agent/Velero/plugin 升级目标只读取活动平台 Manifest，不再回退到 `component_releases` 或运行期环境变量。
- 平台升级器按 Manifest 更新完整平台；失败或回滚时保留旧活动 Manifest。
- Community 使用 API/Frontend 分离拓扑；Enterprise 使用组合平台镜像，并通过 `layout=combined` 适配共享升级器。
- Upgrade Management 页面只保留整体平台升级入口，组件清单只读展示。

## 4. 已完成验证

### 注册生命周期（116/CCE，历史验证）

- Native/command：注册、Agent Ready、注销及资源清理成功。
- CCE/direct：识别 `cce-test-001`、Kubernetes `v1.35.3`、选择 `csi-disk`，注册/注销成功，上传 session 删除。
- CCE/command：命令带 `--cluster-type huaweicloud-cce`，自动发现 kubeconfig，交互选择 `csi-disk`，注册/注销成功。
- Native/direct：预检因 116 无法路由至 `192.168.7.136:6443` 准确失败，写资源前停止；这是环境限制，不是伪成功。

### DR（历史验证）

- 实际资源名称为 `demo-cgi-pvc`（不是早期描述中的 `demo-csi-pvc`）。
- 136 → CCE 的 Sync task `ecc19ecb-425a-444e-9d41-def30d6caf72`：`succeeded`。
- Drill task `fd3450df-37d1-4213-b271-e4fea853fe93`：`succeeded`。
- Drill 目标 namespace `demo-cgi-pvc-drill`，PVC 使用 `csi-disk`，Pod `1/1 Running`，恢复文件校验成功。
- CCE 资源紧张时曾临时缩容旧 Drill Deployment，随后恢复业务 Deployment；未删除业务 namespace/PVC/对象存储数据。

### 统一版本治理（149，历史验证）

- Community 与 Enterprise 接受完整 Manifest，远端 digest 校验通过。
- `/install.sh` 输出清单指定的 Agent `.14`、Velero `.3` 和插件版本。
- 旧集群 `k8s-129` 能识别组件落后并显示可升级。
- 两套 `make verify`、前端构建、脚本语法检查和 `git diff --check` 曾通过。

## 5. 待实现或未闭环

1. 将两仓库改动拆分为可审查提交并推送；正式 CI/release 仍要求精确、干净的 Community commit 和匹配的 `community.lock`。
2. 清理残留的旧环境变量入口（如 `HCDR_AGENT_IMAGE`、`HCDR_VELERO_IMAGE`）：可为部署兼容暂留，但不得成为运行期目标来源，并应补充迁移说明。
3. 全面扫描并删除独立组件发布的旧文档、脚本和接口引用；历史 migration 可保留但必须由迁移 37 清理表。
4. 补齐 Manifest 完整性、digest、安装器单一来源、升级激活/回滚、Native/CCE 一致性和 404 兼容行为测试。
5. 修订仍使用 `vYYYYMMDD.N` 的旧文档/示例，与约定的新格式 `MAJOR.MINOR.PATCH.YYYYMMDD` 统一；旧格式仅限过渡兼容。
6. 用干净构建和真实浏览器重新验证 149 的登录、Upgrade Management、Manifest 展示、`/install.sh` 和实际 workload 镜像；不能只依据 Toast、进度 100% 或元数据。
7. 真实部署 Enterprise 整包升级并验证组合拓扑的更新、失败和回滚；页面正确不代表升级链路已闭环。
8. 若恢复访问 116/CCE，再复核 CCE 网络、136 API 稳定性和历史 DR 资源；不应未经确认重复注册、注销或删除业务资源。

## 6. 潜在隐患与证据

- **Manifest 与旧入口并存**：源码仍能搜到若干旧环境变量/历史 migration，误用会破坏“单一来源”。
- **文档漂移**：部分部署文档仍写旧版本格式和旧流程，容易导致人工发布错误。
- **Enterprise 拓扑差异**：共享升级器必须识别 `combined`，否则可能只更新不存在的 API/Frontend 服务。
- **数据库兼容**：旧数据库无平台发布记录时，`/install.sh` 应返回 `503 component_target_unavailable`，不能偷偷回退环境变量；首次升级需先播种完整 Manifest。
- **状态可信度**：失败任务曾显示 100%，UI 必须区分 succeeded 与 failed；集群状态应以持久化任务、Kubernetes workload 和实际 digest 交叉验证。
- **集群稳定性**：136 曾出现约 40 秒 kube-apiserver 重启；任务异常时先检查 API `/readyz` 和控制面日志。
- **资源容量**：CCE 单节点 CPU 接近 95%，Drill 可能因 `Insufficient cpu` 失败；应在执行前检查容量并明确提示。
- **对象存储适配**：OBS virtual-host、Kopia、BSL 字段必须由 provider 独立处理；不得用 AWS 默认行为覆盖 Huawei 配置。
- **敏感信息**：远程 `.env`、数据库密码、TLS、executor token 和 kubeconfig 不得覆盖、提交或写入文档。

## 7. 项目原则与工作要求

### 用户明确要求

- 不能长时间无反馈；耗时操作要阶段性汇报。
- 不接受仅凭 Toast、页面 100%、构建成功或内存状态宣称完成。
- 所有注册、Sync、Drill、升级验收以平台数据库持久化状态为准，并结合实际 Kubernetes workload 验证。
- Native 与 CCE 必须解耦；新增 provider 不得改变 Native 既有流程。
- 不同对象存储 provider 必须解耦，不能把一个 provider 的字段假定为通用字段。
- 页面错误应固定展示在当前页面，不依赖短暂右上角 Toast。
- 上传 kubeconfig 必须明确“临时使用、用完即删”，并实际清理成功/失败/取消/过期文件。
- 不得未经确认删除业务 namespace、PVC、Deployment 或对象存储数据；临时验证资源要可恢复并记录。
- 发现用户判断与事实不符时，以数据库、日志、Kubernetes 和代码证据纠正，不迎合猜测。
- 持续使用同一整体版本与完整 Manifest，禁止独立发布/激活 Agent、Velero 等组件。

### 仓库规则

- 共享功能只能放在 Community 公共扩展点；Enterprise 不复制 Community 源码、不导入 Community Go `internal` 包。
- 生成物、依赖、截图、日志、证书、数据库和临时文件统一放 `/data/hypercdr-runtime`，不写入源码仓库。
- 数据库存 UTC，只有显示边界转换为用户时区；排序不能使用显示字符串。
- 后端必须执行 tenant scope 和授权校验，不能只依赖 UI。
- 保持已安装旧 Agent 在滚动升级期间的协议兼容。
- 仓库级改动完成前运行 `make verify`；聚焦改动同时运行受影响测试、构建和 `git diff --check`。
- 不使用破坏性 Git 操作覆盖用户未提交修改；不提交真实环境配置和秘密。

## 8. 建议下一步顺序

1. 先建立本文件及版本治理文档为评审基线。
2. 清理旧入口和文档漂移，补齐测试。
3. 在不污染源码树的前提下运行两仓库 `make verify`。
4. 以干净、可重复的发布包在 149 做浏览器和实际 workload 验证。
5. 修正 Enterprise combined upgrade 后，再拆分提交、更新 `community.lock`、推送并生成发布记录。
6. 最后才恢复 116/CCE 的真实环境复测，并按持久化任务状态完成审计。

## 9. 会话恢复记录

原始 Codex 会话文件已保留备份：

`/root/.codex/sessions/2026/08/13/rollout-2026-08-13T16-36-38-019ffa44-25f9-7c60-aff7-1e0c77405a73.jsonl.pre-repair-20260902.bak`

修复后的会话文件包含 82,033 条可解析 JSONL 记录；唯一损坏记录已移除。原始文件未覆盖，可用于回滚或进一步取证。
