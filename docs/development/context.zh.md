# HyperCDR Platform — Project Context（历史快照）

> 本文保留目录重构前的项目记录，旧路径仅用于追溯。当前结构与命令请以根目录 `README.md`、`scripts/` 和 `docs/deployment/` 为准。

> 这是与用户长期对齐的"项目记忆"。每次新会话开始，先读本文件再回复用户。
> 更新规则：本文件只追加"状态变化"和"已确认的决策"。讨论中的想法放 Working Notes 末尾。

---

## 1. 项目定位

- 项目名：**HyperCDR**（容器容灾平台，Container Disaster Recovery）
- 形态：中控平台（control plane）+ 集群侧 agent（comm-agent + velero）
- 第一阶段目标：注册两个集群、配置一个 S3-compatible 对象存储、创建同步策略、按 namespace 选择应用、做容灾配置、同步、容灾演练
- 用户语言：中文

## 2. 代码位置

| 模块 | 路径 | 默认端口 |
|---|---|---|
| 中控后端 | `/data/hypercdr/hypercdr-platform/platform/backend` | 18080 |
| 中控前端 | `/data/hypercdr/hypercdr-platform/platform/frontend` | 3002 |
| 集群侧 agent | `/data/hypercdr/hypercdr-platform/agent/comm-agent` | — |
| Velero（hypercdr-installer 部署） | `/data/hypercdr/hypercdr-velero`（源码）/ platform CRDs + deployments（运行依赖） | — |
| Kubeconfig | `/data/hypercdr/kubeconfigs/config-136`、`/data/hypercdr/kubeconfigs/config-158` | — |
| Harbor 镜像仓库 | `192.168.8.149`（项目路径 `hypercdr/...`） | 443 |
| Harbor CA 证书 | `/data/harbor/cert/hypercdr-ca.crt`（`HCDR_REGISTRY_CA_PATH`） | — |

## 3. 启动命令（已批准的前缀）

### 后端
```
setsid /data/hypercdr/start-platform-api.sh \
  </dev/null >/data/hypercdr/platform-api.log 2>&1 & disown
```
`/data/hypercdr/start-platform-api.sh` 固化了 `HCDR_PUBLIC_BASE_URL`、`HCDR_AGENT_WS_ENDPOINT`、`HCDR_IMAGE_REGISTRY`、`HCDR_AGENT_IMAGE`、`HCDR_VELERO_IMAGE`、`HCDR_VELERO_AWS_PLUGIN_IMAGE` 和 `HCDR_REGISTRY_CA_PATH`。重启 API 必须使用这个脚本，避免注册命令回退到 `127.0.0.1` 或 `registry.local`。
注意：仓库本身没有 git，Go build 必须加 `-buildvcs=false`。

### 前端
```
cd /data/hypercdr/hypercdr-platform/platform/frontend && npm run dev -- --host 0.0.0.0 --port 3002
```

### PostgreSQL
本机 127.0.0.1:5432，用户 `hypercdr` / 密码 `hypercdr` / 库 `hypercdr`。

## 4. 已确认的产品 / 技术决策

### 4.1 架构
- **中控** <-> **agent** 通信：WebSocket（`/ws/agent`）。中控既主动下发任务（注册、注销、备份、恢复），agent 也主动上传（心跳、inventory、Velero 状态、任务事件、注销完成）。
- **agent** 由两部分组成：
  - 业务模块 `comm-agent`（中控 ↔ agent 通道 + 业务编排）
  - velero（hypercdr-installer 自动安装，复用开源 velero + CRD）
- 中控**不直接调用 k8s API**。所有集群侧操作由 agent 通过 K8s 客户端写 CRD，velero watch CRD 实际执行。
- 第一阶段只做 **S3-compatible**（不做 VolumeSnapshotLocation）。
- 第一阶段只做 **namespace 级别**应用备份 / 恢复，**不做跨 namespace**（同 namespace 内资源）。
- 恢复时**支持恢复到目标集群的新 namespace 或同名 namespace**。
- 恢复时如目标 namespace 存在冲突，**默认策略**（"失败 → 改名 + 后缀"，"并发恢复允许 → 跳过"），前端在 wizard 中提供选择。

### 4.2 平台 / 数据
- **生产数据走 PostgreSQL**，无演示 / 内存数据 fallback。`USE_PROTOTYPE_VISUAL_DATA` 必须保持 false。
- 平台访问入口：`http://192.168.8.149:18080`（中控），前端 `http://192.168.8.149:3002`。
- 默认租户 `00000000-0000-0000-0000-000000000001`（单租户，后续再扩展）。
- 数据库关键表（schema 见 `platform/backend/internal/migrations/sql/000001_init.sql`）：
  - `clusters`（含 role/is_default/last_seen_at 等）
  - `applications`（**`(cluster_id, namespace)` 唯一**，含 `protection_status`、`protection_score`）
  - `storage_repositories`、`policies`、`protection_plans`、`restore_points`、`tasks`、`task_events`、`agent_tokens`、`agent_credentials`

### 4.3 集群 / agent
- 第一次注册：用户复制平台生成的 `install.sh | bash` URL，在每个集群节点执行。
- 安装流程（**两阶段**）：
  - **第一阶段 — 准备节点**：每节点执行 `curl -sSL http://192.168.8.149:18080/prepare-node.sh | bash`，安装 Harbor CA 到 `/etc/containerd/certs.d` 和 `/etc/docker/certs.d`。
  - **第二阶段 — 安装 agent 与 velero**：在 master 节点执行 `curl -sSL http://192.168.8.149:18080/install.sh | bash -s -- --token ...`。脚本会：
    - 创建 namespace `hypercdr-agent`（**namespace 固定**，后续如需改可改）
    - 安装 velero CRD、velero、node-agent、comm-agent
    - 检查镜像可拉取（image preflight）
    - 通过 `distribute-registry-ca` 把 CA 分发到所有 worker（需要 `--node-ssh-user` + `--node-ssh-key`）
- 中控下发的 `install.sh` 包含：
  - `--token hcdr_xxx`（一次性的 install token，注册后失效）
  - `--endpoint ws://192.168.8.149:18080/ws/agent`
  - `--namespace hypercdr-agent`
  - `--executor-mode kubernetes`
- **agent 在本地保存 `cluster_id` 与 `agent_token`（在 `secret/hypercdr-agent-bootstrap` 里）**，用于断线重连和反注册。
- **注销流程（更稳妥的顺序）**：
  1. 中控 → agent：下发 "uninstall" 任务。
  2. agent 收到后：删除 `hypercdr-agent` namespace、相关 CRD/RBAC/secret，**主动断开 WebSocket**。
  3. agent → 中控：上报 "uninstalled" 事件（含卸载结果），中控标记 cluster = `deregistering`。
  4. 中控收到 "uninstalled" 后：删除 `agent_credentials`、`agent_tokens`、cluster 记录、关联 applications、tasks 事件；清掉默认集群标记。
  5. 任务面板显示 final state。
- **断线重连**：agent 启动 / 网络恢复时自动用本地保存的 `cluster_id + token` 重新建立 WS；中控收到时若 cluster 已被删除则返回"删除/已注销"指令让 agent 自卸载。

### 4.4 消息标准（task 模板）
- 所有 agent ↔ 中控 消息都是 JSON，**统一 schema**：
  ```json
  {
    "type": "<message-type>",
    "id": "<uuid>",
    "clusterId": "<uuid>",
    "ts": "<rfc3339>",
    "payload": { ... }
  }
  ```
- `type` 命名空间：
  - 中控 → agent：`task.dispatched`、`task.cancel`、`agent.command.*`、`agent.deregister`
  - agent → 中控：`heartbeat`、`inventory.report`、`velero.status`、`task.event`、`agent.deregister.complete`、`agent.deregister.failed`
- 任务事件状态机：`queued → running → (succeeded | failed | cancelled)`，每次状态变化在 `task_events` 追加一行并通过 WS 推送。

### 4.5 集群 UUID / 默认集群
- **第一次注册时**，中控生成 `cluster_id`（UUID）下发给 agent；agent 持久化到本地 `secret/hypercdr-agent-bootstrap`。
- 后续所有通信用 `cluster_id` 作为唯一标识。
- **页面展示**：集群卡片显示 UUID（短串或全串，下方一行）。
- **默认集群**：用户可在卡片上点击设置/取消。注册**第一个集群**时自动成为默认。
- 集群命名：用户可在卡片上直接 inline 编辑（无需弹窗）。

### 4.6 内网镜像
- 镜像仓库：`192.168.8.149/hypercdr`。
- 平台会自己起一个 Harbor 容器，CA 证书自签 10 年：`/data/harbor/cert/hypercdr-ca.crt`。
- 客户端在每个节点把 CA 放到 `/etc/containerd/certs.d/192.168.8.149/ca.crt` 和 `/etc/docker/certs.d/192.168.8.149/ca.crt`。
- 因为 CA 是自签的，`docker pull`、`ctr image pull`、`kubelet image pull` 都需要这步；agent 安装脚本会分发。
- 平台安装时不会因为浏览器告警而失败。

### 4.7 存储仓库（备份目标）
- 用户在页面上"new storage"创建，**类型 S3-compatible**。第一阶段只做这一种。
- bucket 策略：用户创建时指定 bucket；备份时选择已配置好的对象存储仓库。
- 凭据：AK/SK 不在 UI 列表中明文显示（用 `secret_ref` + 后端 secret）。
- 列表字段：name、type、bucket、region、endpoint、**TLS**（独立列）、url style、connection status、last validated at。SK 不在列表展示。

### 4.8 加密 / 凭据
- 数据库敏感字段加密 key：平台启动时如果 `HCDR_DB_ENCRYPTION_KEY` 未设置，使用**一次性随机生成的 key**写入 `data/secret.key`，启动时自动加载；用 AES-GCM 加密 AK/SK、`agent_credentials.credential` 等。
- 第一阶段不引入 KMS。

### 4.9 Velero
- 当前固定版本：**v1.18.2**，HyperCDR 镜像版本为 `v1.18.2-hcdr.2`。完整固定源码位于 `/data/hypercdr-main/third_party/velero`，平台内嵌同版本 CRD；构建过程产生的文件放在 `/data/hypercdr-runtime` 或 `/tmp`。
- 安装方式：hypercdr installer 一体化安装，namespace `velero`（与 `hypercdr-agent` 同 namespace）。
- 第一阶段 **PVC 备份**：**remote snapshot**（上传 tar 到对象存储），**不做** local snapshot。
- 第一阶段 **不**做跨 namespace 恢复。

### 4.10 失败处理 / 验收
- 任务失败要明确分类：参数错误、对象存储不可达、Velero 失败、agent 断线超时、网络超时。
- 验收：单元 + 集成 + E2E（用 Playwright 在本机 headless 跑，chromium 路径 `/data/software/ms-playwright/chromium-1223/chrome-linux64/chrome`）。
- 已存在的测试：`platform/backend/internal/httpserver/router_test.go`。
- 工具目录：`platform/tools/...`，按需新增小工具（dbseed、playwright 截图工具等）。

## 5. UI / 交互决策（截至 2026-06-12）

- 风格：紧凑、专业、vSphere-like。所有弹窗**不再虚化**父窗口（`bg-slate-900/30`）。
- 集群管理页：
  - 顶部 CTA：单一"+ Register Cluster"按钮（带 + 图标）。
  - 卡片：UUID 在 name 下方一行小字；agent 框显示**整体状态**（Ready / Not Ready），不写模块名。
  - Online / Ready 不重复：只用"Ready"作 agent 整体状态指示。
  - 卡片下放 **Recent Tasks 列表**（任务面板），每条任务一行：开始时间、类型、状态、结果。注册 / 注销会实时更新该记录。
  - 没有注册集群时：路由到 `/clusters`，顶部其他菜单置灰。
- DR 页面（Application DR）：
  - 阶段 1（Select Applications）：展示 `namespace / workloads / services / PVC / capacity / scope`，PVC 无数据时显示 `None`（或 `N/A`），居中。
  - 阶段 2（Setup DR）：配置 wizard 选 policy / storage / target cluster。
  - 阶段 3（Sync / Recovery）：展示同步状态、演练、切换。
  - "Add application" 按钮文案改为"Next →"，点击后 namespace 进入阶段 2，刷新后仍在阶段 2。
- Storage 页：单页 wizard（无 Step1/Step2），5 种类型 tab 卡（Amazon S3 / S3-Compatible / Azure Blob / GCS / NFS）。
  - 表单顺序：**Name → Type → Endpoint / Region / Bucket / AK / SK → SSL toggle → URL style**。
  - SSL 切换控件：iOS 风格开关，简洁，无说明文字。
  - 列表字段：name、type、bucket、region、endpoint、TLS、url style、status、last validated at。SK 不展示。
  - "Test Connection" 在表单底部；成功提示在表单内部，不显示在父页面。
  - 列宽可拖拽。

## 6. 已知问题 / 已修复

### 6.1 已修复（2026-06-12）
- **`ApplyInventory` 覆盖 `protection_status`**：原逻辑是 `delete + insert`，每次 agent 上报都会把所有 application 重置成默认 `unprotected`，导致用户把 namespace 移到阶段 2 之后，下次心跳又回到阶段 1。修复为 `INSERT ... ON CONFLICT (cluster_id, namespace) DO UPDATE SET ...`（只刷新 inventory 字段；不动 `protection_status`、`protection_score`）。surviving 行保留阶段。
  - 备份：`platform/backend/internal/store/postgres.go.bak-20260612-083000`
  - 临时驱动已删：`cmd/invtest/`（目录已 rmdir）。

### 6.2 仍需跟进
- **永久 Go 测试**：在 `platform/backend/internal/store/postgres_apply_inventory_test.go` 加一个回归测试，需要在 CI 上跑通。
- **DR 页面 `resourceProfiles` 静态 demo**：截至本次 `kasten-io` 案例里只走真实 API；`resourceProfiles` 里 `frontend-service`、`auth-db-primary` 是早期 demo，需要清掉 / 改成真实数据。
- **3002 + 18080 进程**：用户每次退出 codex 上下文后服务可能停止，需要每次重新拉起（运行命令见第 3 节）。

## 7. 不需要做的事（避免反复确认）
- 不引入 VolumeSnapshotLocation / CSI snapshot。
- 不做跨 namespace 恢复。
- 不做 local snapshot。
- 不做应用发现器的高级打 tag（用 K8s labels 即可）。
- 不引入外部 KMS。
- 不在 UI 中展示 AK / SK 明文（SK 列表里也不显示）。

## 8. Working Notes（最新在底部）

### 2026-06-12（续）
- 用户反馈"kasten-io 移到第二步后下次又回第一步"。根因 6.1 已修。再现路径：用户操作（点 Next）→ 后端 `UpdateApplication` 把 `protection_status=pending_protection` 写入；下次 agent 30~60s 上报一次 inventory → 旧 `ApplyInventory` DELETE + INSERT 把所有行重置成 `unprotected`。新代码不会再覆盖。
- 验证：在 3002 重新进入 DR 页面看 kasten-io 是否在阶段 2 稳定。
- 后续：要把"前端刷新会先看到旧 state 再被新 state 覆盖"的中间态体验优化掉（用 React Query / SWR 替换自写的 `refreshPlatformData` polling）。


## 9. 网络 / 进程边界（重要！2026-06-12 新增）

- Codex 沙箱的进程跑在 `bwrap --unshare-net` 下，**沙箱内看不到 host 网络栈的 socket**，也连不上 host 上的 `127.0.0.1:18080` / `3002` / `5432`。
- 但你（用户）和浏览器在 host 网络里，所以**用户能直接访问 3002 和 18080**。
- 沙箱内可以验证数据库 / 编译 / 单测；需要端到端（HTTP 链路）验证时请你在浏览器里看，或者把 host 端口映射到沙箱（用 `socat` 在 bwrap 外的某个我们能控的进程转发）。
- 这就是为什么我 `curl http://127.0.0.1:18080` 在同一 exec_command 里能 200，连续两个 exec_command 之间就 7。**不要相信 7 / 6 的"服务挂了"** —— 看 `/data/hypercdr/platform-api.log` 实时在涨 + `pgrep` 不到 = 进程在沙箱外跑得好好的。
- 看后端是否真的在跑：直接看 `tail -f /data/hypercdr/platform-api.log` 是否有新请求日志；或者用 `ss` / `lsof` 在 host 查（沙箱里这两条命令受限，但能看到自己的子进程）。


## 10. 阶段 2 → 阶段 3 的容灾配置持久化（2026-06-12）

`finishProtectWizard` 现在做两件事：
1. `POST /api/v1/protection-plans`（创建 plan，行入库）
2. `PATCH /api/v1/applications/{id}` 把 `protectionStatus` 改为 `protected`（同步 app 阶段）

`disableProtection` 反向：`PATCH /api/v1/applications/{id}` 把 status 改回 `pending_protection`，
成功后 `await refreshPlatformData()` 重新拉数据。

如此 `applications.protection_status` 和 `protection_plans` 两边一致，刷新页面 / 重新进入
DR 页面阶段不会跳。


---

## 2026-06-16 demo-mysql 容灾恢复结论

### 恢复状态：数据一致性 ❌ 失败，应用资源 ✅ 成功

| 项目 | 136 源端 | 158 恢复端 | 状态 |
|------|---------|-----------|------|
| Deployment/Service/ConfigMap/Secret | demo-mysql 应用 | demo-mysql-v2 命名空间 | ✅ 完全恢复 |
| Pod/PVC 资源 | demo-mysql-data PVC | demo-mysql-data PVC bound | ✅ 恢复 |
| MySQL users 表数据 | 5 行 (id 10-14 alice/bob/carol/dave/eve) | 3 行 (id 1-3 alice/bob/carol) | ❌ 不一致 |

### 根因
136 集群的 demo-mysql PVC 走的是 `local-path` StorageClass（`local-path-provisioner`），其底层 PV 是 **hostPath** 类型。velero 1.17 的安全机制：
- backup 时：拒绝为 hostPath PV 做 fs-backup（warning: "is a hostPath volume which is not supported for pod volume backup, skipping"）
- 136 backup `hcdr-demo-mysql-20260615091028-682886a9` 共 309 个 items，phase=Completed，但 PodVolumeBackup 数量 = 0
- 数据从未被写入 S3 / MinIO

### 158 恢复端
- 静态 PV `pv-demo-mysql-data` (hostPath /data/kasten-storage/demo-mysql-data) 提前创建
- Velero restore 成功 restore 出所有 K8s 资源（apply 之后）
- MySQL pod 启动时，PV 是空的，init script 写入 3 条样本数据（id 1-3）
- 没有 5 条 136 原始数据，因为 S3 里压根没存

### 平台代码修复
- `agent/comm-agent/internal/velero/restore.go` 加了 `DefaultVolumesToFsBackup *bool` 字段
- 重新 build + 重打 image `192.168.8.149/hypercdr/comm-agent:v2-test`
- 158 集群 deployment 已更新到 v2-test
- 但**这个修复在 hostPath 场景下无效**（velero 本身就拒绝 hostPath 走 fs-backup）

### 后续可行方案
要让 136 → 158 demo-mysql 真正能恢复数据，必须：
1. **换 StorageClass**：从 `local-path`(hostPath) 换成支持 CSI snapshot 的 provisioner（如 nfs-subdir-external-provisioner）
2. 或 **用 velero CSI snapshot 模式** 而非 fs-backup（需要 CSI driver 支持）
3. **重新部署 demo-mysql** 用新 SC，重新做完整备份 → 恢复


---

## 2026-06-16 demo-mysql-csi 容灾恢复结论

### 恢复状态：应用页面 ✅ 成功，跨集群恢复链路 ✅ 成功

`demo-mysql-csi` namespace 已在 136 源集群完成备份，并在 158 目标集群恢复成功。用户已通过浏览器确认源端和目标端页面都可访问。

| 项目 | 136 源端 | 158 恢复端 | 状态 |
|------|---------|-----------|------|
| Namespace | demo-mysql-csi | demo-mysql-csi | ✅ 已存在 |
| Web Service | demo-mysql-web NodePort 30081 | demo-mysql-web NodePort 30081 | ✅ 已恢复 |
| MySQL Service | demo-mysql ClusterIP 3306 | demo-mysql ClusterIP 3306 | ✅ 已恢复 |
| Pods | demo-mysql / demo-mysql-web Running | demo-mysql / demo-mysql-web Running | ✅ 已运行 |
| 页面访问 | http://192.168.7.136:30081 | http://192.168.8.158:30081 | ✅ 浏览器可访问 |

### 验证到的访问地址
- 源集群 136：`http://192.168.7.136:30081`
- 源集群实际承载 Pod 节点也可访问：`http://192.168.7.135:30081`
- 目标集群 158：`http://192.168.8.158:30081`

### 本轮闭环意义
- 旧 `demo-mysql` 使用 `local-path`/`hostPath`，Velero 1.17 不支持 hostPath PVC 的 fs-backup，导致数据没有真正进入对象存储。
- 新 `demo-mysql-csi` 使用可恢复的 CSI 存储路径，避免了 hostPath 限制。
- 当前已经证明：HyperCDR 可以对 `demo-mysql-csi` namespace 完成备份、跨集群恢复，并让恢复后的业务页面正常对外访问。

### 下一步演示目标
用户已在页面上把 `demo-mysql-csi` 做完容灾配置，并进入阶段 3 `Start DR` 列表。下一步需要验证完整页面演示链路：
1. 在页面选中 `demo-mysql-csi` 并点击 `Start Sync`。
2. 按容灾配置参数备份到 `my-minio`。
3. 备份成功后生成 restore point。
4. 在页面从 restore point 点击 `Drill`。
5. 恢复到 158 集群并确认页面可访问。


---

## 2026-06-18 demo-mysql-csi 页面演示链路复核

### 状态：完整链路 ✅ 已闭环

已从数据库和 Kubernetes API 复核，`demo-mysql-csi` 的页面演示链路已经完成：

| 步骤 | 当前结果 |
|------|----------|
| 应用阶段 | 136 源端 `demo-mysql-csi` 为 `protected` |
| 保护计划 | `2a171784-7469-41a0-82cb-84ff414e0c2a` active，源端 136，目标端 158，storage `my-minio` |
| Start Sync / Backup | 任务 `d22f9945-c927-4d53-89db-cf187699a9a1` succeeded |
| Restore Point | `339c8e3d-96ac-4ba6-99bb-da8716cc6d8f` available |
| Velero Backup | `hcdr-demo-mysql-csi-20260617061956-d22f9945` phase Completed，315/315 items |
| Drill | 任务 `4d768c2f-0dff-492b-912a-7af2aced2d64` succeeded |
| Velero Restore | `hcdr-restore-demo-mysql-csi-4d768c2f` exists on 158 |
| 目标资源 | 158 `demo-mysql-csi` 下 `demo-mysql`、`demo-mysql-web` Pod Running，PVC Bound，NodePort 30081 |

### 实际查询命令

- 源端 kubeconfig：`/data/hypercdr/kubeconfigs/config-136`
- 目标端 kubeconfig：`/data/hypercdr/kubeconfigs/config-158`
- 源端备份查询：`kubectl --kubeconfig /data/hypercdr/kubeconfigs/config-136 get backups.velero.io -A`
- 目标端恢复查询：`kubectl --kubeconfig /data/hypercdr/kubeconfigs/config-158 get restores.velero.io -A`
- 目标资源查询：`kubectl --kubeconfig /data/hypercdr/kubeconfigs/config-158 get pods,svc,pvc -n demo-mysql-csi -o wide`

### 注意事项

- 数据库里仍有早期测试遗留的 `running` drill 任务（如 `03bffca7...`、`c9b8cd6d...`），它们属于修复前的超时/重试历史，不代表最新链路失败。
- `restore_points` 表实际 schema 仍把 `sourceNamespace`、`backupStorageName` 放在 `metadata` JSON 中；代码结构体已有字段，但当前数据库未单独建列。


---

## 2026-06-18 test2 恢复未出现问题排查

### 结论

- 用户在 136 `demo-mysql-csi` 页面新增 `test2` 后执行同步。
- 源端 MySQL 已确认存在 `test2`：
  - `id=101, username=test2, email=aa@11.com, phone=123`
- 本次备份 `hcdr-demo-mysql-csi-20260618005552-42a9ef6b` 成功：
  - Velero Backup phase `Completed`
  - PodVolumeBackup `Completed`
  - 上传数据约 `210428458` bytes
  - restore point `842f3cec-18cf-4a9c-b1cf-f70f72f97646` available
- 158 目标端 MySQL 没有 `test2`，原因不是备份缺数据，而是恢复任务没有真正提交成功：
  - task `5436d6d5-2022-41f4-a70e-7eb4a78a8759`
  - status `failed`
  - error `RESTORE_SUBMIT_FAILED`
  - 具体错误：`podvolumerestores.velero.io is forbidden: User "system:serviceaccount:hypercdr-agent:hypercdr-agent" cannot list resource "podvolumerestores" ...`

### 修复

- 158 和 136 集群已热修 `ClusterRole/hypercdr-agent`，补充：
  - apiGroup `velero.io`
  - resources `podvolumebackups`, `podvolumerestores`
  - verbs `get,list,watch,create,patch,update,delete`
- 后端 `install.sh` 模板已补充同样 RBAC：
  - `platform/backend/internal/httpserver/router.go`
- 前端已修正真实状态显示：
  - Application DR 页面恢复任务列不再使用本地模拟 `running -> completed` 状态。
  - 同步任务只看后端 `backup` task。
  - 恢复任务只看后端 `restore/drill/takeover` task。
  - Restore Points 页面按 restore point 的**最新**恢复任务显示状态，失败时展示 `errorMessage/errorCode`，不会被旧成功任务覆盖。

### 注意

- RBAC 修复后，重新发起 restore/drill 才会使用新权限；已经失败的 task 不会自动变成功。
- 如果恢复到目标集群同名 namespace 且已有 PVC，Velero full restore 不一定会覆盖正在使用的现有卷；若需要验证 `test2` 数据进入 158，建议先用一个新 namespace 做 drill，或明确执行清理/替换策略后再 restore。

### 2026-06-18 full restore 复测结果

- 已通过平台 API 对 158 正式 namespace `demo-mysql-csi` 发起同名 full restore：
  - task `e21430f8-6e66-4db2-8f5c-e74a695111c9`
  - restore point `842f3cec-18cf-4a9c-b1cf-f70f72f97646`
  - backup `hcdr-demo-mysql-csi-20260618005552-42a9ef6b`
  - target cluster `prod-158`
  - target namespace `demo-mysql-csi`
  - conflict policy `replace`
- 结果：
  - Platform task `succeeded`
  - Velero Restore `hcdr-restore-demo-mysql-csi-e21430f8` phase `Completed`
  - 158 `demo-mysql-csi` 下 Pod Running、PVC Bound
  - 158 正式 namespace MySQL 已包含 `test2`
- 关键条件：
  - 当前 full restore 同名 namespace 要使用 `conflictPolicy=replace`。
  - agent 会先清理旧 Restore/PodVolumeRestore 状态，再删除目标 namespace，最后创建 Velero Restore。
  - 如果选择 `skip/overwrite` 而目标 namespace/PVC 已存在，不能保证 PVC 数据被替换。


---

## 2026-07-14 149 主机源码恢复部署记录

### 恢复背景

- 原 149 主机损坏，Harbor 数据和 PostgreSQL 数据均丢失。
- 当前只从源码目录恢复，不做旧数据库/旧镜像仓库数据恢复。
- 当前源码根目录：`/data/hypercdr`
  - 平台源码：`/data/hypercdr/hypercdr-platform`
  - Velero 源码：`/data/hypercdr/hypercdr-velero/velero-1.17.1`

### 基础设施

- Docker 和 Docker Compose v2 已安装。
- Harbor 已全新部署在 `/data/harbor`，版本 `v2.15.2`。
- Harbor 访问地址：`https://192.168.8.149`
- Harbor 管理员：`admin`
- Harbor 管理员密码文件：`/data/harbor/harbor_admin_password`
- Harbor 项目：
  - `hypercdr`
  - `baseimage`
- PostgreSQL 作为独立容器运行：
  - 容器名：`hypercdr-postgres`
  - 镜像：`postgres:16`
  - 端口：`15432:5432`
  - 数据库连接：`postgres://hypercdr:hypercdr@127.0.0.1:15432/hypercdr?sslmode=disable`

### 证书

- Harbor 20 年 CA/服务证书：
  - `/data/harbor/cert/hypercdr-ca.crt`
  - `/data/harbor/cert/hypercdr-ca.key`
  - `/data/harbor/cert/harbor.crt`
  - `/data/harbor/cert/harbor.key`
  - 有效期至 `2046-07-09`
- Harbor CA 已安装到本机 Docker 和系统信任：
  - `/etc/docker/certs.d/192.168.8.149/ca.crt`
  - `/usr/local/share/ca-certificates/hypercdr-harbor-ca.crt`
- 平台 20 年证书：
  - `/data/hypercdr/certs/platform.crt`
  - `/data/hypercdr/certs/platform.key`
  - 有效期至 `2046-07-09`
- 当前平台 API/WS 按 HTTP/WS 运行，证书已准备但未启用 HTTPS/WSS。

### 编译版本

- 本机 Go：`/usr/local/go/bin/go version` -> `go1.24.9 linux/amd64`
- 统一使用 Go `1.24.9` 编译：
  - platform-api
  - platform-migrate
  - comm-agent
  - Velero 本地源码镜像

### Harbor 镜像

已推送到 `192.168.8.149/hypercdr`：

- `platform-api:dev`
- `comm-agent:dev`
- `postgres:16`
- `velero:v1.18.2-hcdr.2`
- `velero-plugin-for-aws:v1.13.0`

注意：`velero:v1.18.2-hcdr.2` 是从仓库内固定源码 `/data/hypercdr-main/third_party/velero` 构建并推送的镜像，不是直接使用官方 Velero 镜像。

### 平台服务

systemd 服务：

- `hypercdr-platform-api.service`
  - 监听：`0.0.0.0:18080`
  - 启动脚本：`/data/hypercdr/run-platform-api.sh`
  - 日志：`/data/hypercdr/logs/platform-api.log`
- `hypercdr-platform-frontend.service`
  - 监听：`0.0.0.0:3002`
  - 启动脚本：`/data/hypercdr/run-platform-frontend.sh`
  - 日志：`/data/hypercdr/logs/platform-frontend.log`

关键后端环境：

- `HCDR_PUBLIC_BASE_URL=http://192.168.8.149:18080`
- `HCDR_AGENT_WS_ENDPOINT=ws://192.168.8.149:18080/ws/agent`
- `HCDR_IMAGE_REGISTRY=192.168.8.149/hypercdr`
- `HCDR_AGENT_IMAGE=192.168.8.149/hypercdr/comm-agent:dev`
- `HCDR_VELERO_IMAGE=<registry>/hypercdr/velero:v1.18.2-hcdr.2`
- `HCDR_VELERO_AWS_PLUGIN_IMAGE=192.168.8.149/hypercdr/velero-plugin-for-aws:v1.13.0`
- `HCDR_REGISTRY_CA_PATH=/data/harbor/cert/hypercdr-ca.crt`

### 已验证

- `docker ps` 显示 Harbor 全部组件 healthy，`hypercdr-postgres` healthy。
- Harbor API 验证项目存在：`hypercdr`、`baseimage`。
- Harbor API 验证 `hypercdr` 项目仓库存在：
  - `hypercdr/platform-api`
  - `hypercdr/comm-agent`
  - `hypercdr/postgres`
  - `hypercdr/velero`
  - `hypercdr/velero-plugin-for-aws`
- `systemctl is-active hypercdr-platform-api.service hypercdr-platform-frontend.service` 均为 `active`。
- 端口监听：
  - `18080` platform-api
  - `3002` frontend
  - `15432` PostgreSQL
  - `80/443` Harbor
- `http://127.0.0.1:3002/` 返回前端首页。
- `http://127.0.0.1:18080/install.sh` 返回 200，并包含：
  - `ENDPOINT="ws://192.168.8.149:18080/ws/agent"`
  - `AGENT_IMAGE="192.168.8.149/hypercdr/comm-agent:dev"`
  - `VELERO_IMAGE="<registry>/hypercdr/velero:v1.18.2-hcdr.2"`
  - `VELERO_AWS_PLUGIN_IMAGE="192.168.8.149/hypercdr/velero-plugin-for-aws:v1.13.0"`
  - `REGISTRY_CA_URL="http://192.168.8.149:18080/assets/registry/ca.crt"`
- `http://127.0.0.1:18080/prepare-node.sh` 返回 200，并包含：
  - `REGISTRY_HOST="192.168.8.149"`
  - `REGISTRY_CA_URL="http://192.168.8.149:18080/assets/registry/ca.crt"`
  - Docker/containerd CA 安装路径和 `hosts.toml` 配置。

### 2026-07-14 标准镜像化部署切换

149 中控平台已从临时恢复部署方式切换为标准 Docker Compose 镜像化部署。

- 标准部署目录：`/data/hypercdr/deploy`
- Compose 文件：`/data/hypercdr/deploy/docker-compose.yaml`
- 环境文件：`/data/hypercdr/deploy/.env`
- 版本：`v20260714.1`
- 当前容器：
  - `hypercdr-platform-api` -> `192.168.8.149/hypercdr/platform-api:v20260714.1`
  - `hypercdr-platform-frontend` -> `192.168.8.149/hypercdr/platform-frontend:v20260714.1`
  - `hypercdr-postgres` -> `192.168.8.149/hypercdr/postgres:16`
- 对外入口：
  - 前端/API 统一入口：`http://192.168.8.149:3002`
  - Agent WebSocket：`ws://192.168.8.149:3002/ws/agent`
- `install.sh` 当前默认镜像：
  - `192.168.8.149/hypercdr/comm-agent:v20260714.1`
  - `<registry>/hypercdr/velero:v1.18.2-hcdr.2`
  - `192.168.8.149/hypercdr/velero-plugin-for-aws:v1.13.0`
- 旧 systemd 直跑服务已停止、禁用并移除 unit：
  - `hypercdr-platform-api.service`
  - `hypercdr-platform-frontend.service`
- 切换前数据库备份：
  - `/data/hypercdr/backups/hypercdr-20260714-before-standard-compose.sql`
- 旧 Docker volume `docker_hypercdr-postgres-data` 暂时保留，作为切换后的回滚保护。
- `baseimage` 项目已同步本地已有基础镜像：
  - `baseimage/debian:bookworm-slim`
  - `baseimage/nginx:1.27-alpine`
  - `baseimage/alpine:latest`
  - `baseimage/paketobuildpacks-run-jammy-tiny:0.2.78`
