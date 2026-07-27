# DR 任务协议与状态机

更新时间：2026-07-01

本文定义 HyperCDR 中控平台与集群侧 `comm-agent` 在 DR 计划、备份任务、恢复任务、恢复点、页面状态展示之间的目标协议与状态机。后续程序优化应以本文为准。

本文是任务状态转换的权威约定。前端不能再通过临时文案、进度数字、Velero 中间状态去猜业务状态；必须以平台任务状态、标准 Velero 事件、恢复点状态为准。

## 0. 目录

- 1. 核心对象
- 2. WebSocket 消息封装
- 3. 平台主动发起的交互
- 4. Agent 主动发起的交互与事件
- 5. 备份执行流程
- 6. Restore / Drill / Takeover 流程
- 7. Retention Cleanup 流程
- 8. Size 与进度约定
- 9. 页面状态映射
- 10. 目标任务 JSON 定义
- 11. 各任务目标定义
- 12. 幂等与乱序处理
- 13. 硬性不变量
- 14. 页面与平台 API 映射
- 15. 实现检查清单

## 1. 核心对象

### 1.1 Protection Plan

Protection Plan 是一个源 namespace 或应用的持久 DR 配置。

关键字段：

- `id`：中控平台计划 ID。
- `sourceClusterId`：产生备份的源集群。
- `appId`：被保护的 namespace/应用。
- `storageRepoId`：Velero 使用的对象存储仓库。
- `targetClusterId`：可选的恢复目标集群。
- `status`：配置状态，不是备份执行状态。

计划状态：

- `draft`：用户已选择 namespace，但还没有完成 DR 配置。
- `configuring`：平台正在准备存储或计划配置。
- `active`：存储和计划配置已生效。
- `storage_failed`：存储配置失败。
- `schedule_failed`：计划配置失败。
- `disabled`：计划被禁用。

硬性约束：

- 恢复点只属于源集群。
- 目标集群上的 Velero Backup CR 不能创建平台恢复点。
- `protection_plans.source_cluster_id` 是判断恢复点归属的权威字段。

### 1.2 Task

Task 是中控平台创建的一次可执行操作。

关键字段：

- `id`：平台任务 ID。
- `commandId`：一次下发的幂等/关联 ID。
- `type`：任务类型。
- `clusterId`：必须执行该任务的集群。
- `protectionPlanId`：关联计划。
- `restorePointId`：关联恢复点。
- `status`：平台任务生命周期状态。
- `progress`：`0..100` 的进度数字。
- `payload`：任务事实数据，例如 Velero 名称、size 信息。

支持的任务类型：

- `storage-sync`：下发对象存储凭据，并应用 Velero `BackupStorageLocation`。
- `schedule-sync`：为计划创建或更新 Velero `Schedule`。
- `backup`：创建或观测一次 Velero `Backup`。
- `restore`：从恢复点执行恢复。
- `drill`：执行演练恢复。
- `takeover`：执行接管恢复。
- `retention-cleanup`：删除过期 Velero 备份，并标记恢复点已删除。
- `unregister`：移除 agent/Velero 资源并注销集群。

任务终态：

- `succeeded`
- `failed`

任务活跃态：

- `queued`
- `dispatched`
- `accepted`
- `running`
- `syncing`

`stopped` 只能作为 UI/操作态使用，除非端到端取消协议实现完成，否则不能作为后端成功终态。

### 1.3 Restore Point

Restore Point 是源集群 Velero 备份完成后，在中控平台生成的持久记录。

职责边界：

- Velero 只负责产生和维护集群内的 Backup CR 以及对象存储中的备份数据。
- agent 只负责观测 Velero Backup CR，并向平台上报标准事件。
- 中控平台是唯一可以创建、更新、删除平台恢复点记录的组件。
- agent 不能直接创建平台恢复点；agent 也不能要求平台无条件创建恢复点。
- 平台必须在校验事件来源、计划归属、源集群、Backup 终态后，按幂等规则创建或更新恢复点。

身份字段：

- `sourceClusterId`
- `protectionPlanId`
- `appId`
- `veleroBackupName`

唯一性规则：

- `(sourceClusterId, veleroBackupName)` 唯一确定一个恢复点。
- 同一个 Velero backup 的重复事件只能更新已有恢复点，不能创建重复记录。

创建触发：

- 手动备份：平台先创建 backup task，agent 执行并上报终态；平台收到合法成功终态后创建或更新恢复点。
- 定时备份：Velero Schedule 自动产生 Backup CR，agent 观测并上报 `agent.velero.event`；平台第一次收到合法事件时创建本次 backup task，收到合法 `backup_completed` 后创建或更新恢复点。
- 补偿场景：平台可以通过 inventory 或 Velero 事件补偿缺失记录，但仍必须满足源集群、计划归属和 Backup `Completed` 校验。

不能创建恢复点的情况：

- Backup CR 不属于 HyperCDR 管理的 plan。
- Backup CR 来自目标集群。
- Backup 处于 `Failed`、`FailedValidation`、`PartiallyFailed`、`Canceled` 或非终态。
- 事件无法匹配到有效 `protectionPlanId/sourceClusterId/sourceNamespace/veleroBackupName`。
- 平台无法确认该 Backup 对应的对象存储数据可用于恢复。

恢复点状态：

- `available`：可用于 restore、drill、takeover。
- `deleted`：备份已从 Velero/仓库删除，或被清理隐藏。
- `clearing`：删除请求已发出。
- `failed`：兼容历史数据或补偿异常状态；新流程中失败备份默认不创建恢复点。

默认恢复点页面只展示 `available`。任务历史页面可以展示失败、已删除、已清除等关系。

## 2. WebSocket 消息封装

所有中控平台与 agent 的 WebSocket 消息使用统一 envelope。

```json
{
  "version": "v1",
  "messageId": "msg_01",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {}
}
```

规则：

- `messageId` 标识一条逻辑消息，必须全局唯一。
- 同一条逻辑消息重发时，`messageId` 不变，直接重发缓存的原始消息。
- `messageKind` 标识通信语义，取值为 `request`、`response`、`event`。
- `type` 标识业务类型，不用于判断是否需要响应。
- `request` 必须收到一个 `response`。
- `response` 不再响应，避免 ack 套 ack。
- `event` 是异步状态事件，通过 `payload.ackRequired` 决定是否需要响应。
- `payload.ackRequired=false` 的 event 不需要响应，也不重发。
- `payload.ackRequired=true` 的 event 必须收到响应，未收到响应时必须重发。
- 响应消息必须通过 `payload.ackMessageId` 指向被响应的请求消息。
- 响应消息可以通过 `payload.ackType` 标识被响应的请求类型。
- 任务类消息使用 `payload.commandId` 做业务关联和幂等。
- 非任务请求使用 `payload.requestId` 做业务关联。
- 顶层不再定义 `correlationId`，避免和 `payload.commandId/requestId` 重复或不一致。
- `clusterId` 必须是执行任务的集群。
- 平台必须忽略来自错误集群的任务或 Velero 消息。
- agent 不能执行 `clusterId` 与自身注册集群不一致的任务。

### 2.1 请求与响应总规则

目标协议要求每个 `request` 消息都有明确 `response`。`event` 是否需要响应由 `payload.ackRequired` 决定。

消息响应矩阵：

| 消息 | kind | 发送方 | 是否必须响应 | 正常响应 | 失败响应 | 说明 |
| --- | --- | --- | --- | --- | --- |
| `agent.register` | `request` | agent | 是 | `platform.register.accepted` | `platform.register.rejected` | 建立身份，agent 收到 accepted 后才算已认证 |
| `platform.task.dispatch` | `request` | 平台 | 是 | `agent.task.accepted` 接收确认 | `agent.task.failed` 接收失败 | accepted 只表示接收，后续进度和终态走 event |
| `platform.task.cancel` | `request` | 平台 | 预留 | 预留 | `agent.message.error` | 取消协议未实现前不能作为后端成功终态 |
| `platform.inventory.request` | `request` | 平台 | 是 | `agent.inventory.report` | `agent.message.error` | 按需拉取 inventory，不创建 task |
| `platform.register.accepted` | `response` | 平台 | 否 | 无 | 无 | `agent.register` 的成功响应 |
| `platform.register.rejected` | `response` | 平台 | 否 | 无 | 无 | `agent.register` 的失败响应 |
| `agent.task.accepted` | `response` | agent | 否 | 无 | 无 | `platform.task.dispatch` 的接收响应 |
| `agent.inventory.report` | `response` | agent | 否 | 无 | 无 | 作为 `platform.inventory.request` 的响应时 |
| `agent.message.error` | `response` | agent | 否 | 无 | 无 | agent 对非任务平台请求的失败响应 |
| `agent.heartbeat` | `event` | agent | 否 | 无 | 无 | `payload.ackRequired=false`，心跳是状态事件 |
| `agent.inventory.report` | `event` | agent | 否 | 无 | 无 | `payload.ackRequired=false`，作为变化推送或 5min 兜底上报时 |
| `agent.task.progress` | `event` | agent | 否 | 无 | 无 | `payload.ackRequired=false`，任务执行进度事件 |
| `agent.task.completed` | `event` | agent | 是 | `platform.event.ack` | `platform.event.error` | `payload.ackRequired=true`，任务成功终态事件 |
| `agent.task.failed` | `event` | agent | 是 | `platform.event.ack` | `platform.event.error` | `payload.ackRequired=true`，任务失败终态事件 |
| `agent.velero.event` | `event` | agent | 视事件类型 | `platform.event.ack` | `platform.event.error` | completed/failed 需要响应，progress 不需要响应 |

强制规则：

- `messageKind` 必须明确区分请求、响应、事件/状态上报，不能只靠 `type` 猜语义。
- `request` 必须响应。
- `response` 不再响应。
- `event` 按 `payload.ackRequired` 判断是否响应。
- `payload.ackRequired=false` 的 event 不响应，不重发。
- `payload.ackRequired=true` 的 event 必须响应，响应类型为 `platform.event.ack` 或 `platform.event.error`。
- `agent.task.accepted` 是 `platform.task.dispatch` 的响应消息。
- `agent.task.progress` 是任务执行过程中的临时进度事件，默认 `payload.ackRequired=false`。
- `agent.task.completed`、`agent.task.failed` 是任务终态事件，必须 `payload.ackRequired=true`。
- 任务类消息必须闭环：`dispatch -> accepted -> completed/failed`。
- `accepted` 不是成功，只表示 agent 接收了任务。
- 平台下发任务必须通过 `taskId + commandId` 关联；定时备份事件驱动任务可以在首次合法 event 到达平台时创建。
- 平台主动请求类消息如果要求响应，响应 payload 中必须写入 `ackMessageId`。
- 任务业务幂等必须使用 `commandId`，不能使用 `messageId`。
- agent 收到不认识的平台消息，不能执行任何动作，应记录日志；如果该消息是 task dispatch，则返回 `agent.task.failed`。

### 2.2 可靠事件响应

`payload.ackRequired=true` 的 event 必须由接收方返回轻量响应。当前主要用于 agent 向平台上报任务终态和 Velero 终态事件。

成功响应：`platform.event.ack`

```json
{
  "version": "v1",
  "messageId": "msg_ack_task_completed_001",
  "messageKind": "response",
  "type": "platform.event.ack",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:51Z",
  "payload": {
    "ackMessageId": "msg_task_completed_001",
    "ackType": "agent.task.completed",
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "persisted": true
  }
}
```

失败响应：`platform.event.error`

```json
{
  "version": "v1",
  "messageId": "msg_error_task_completed_001",
  "messageKind": "response",
  "type": "platform.event.error",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:51Z",
  "payload": {
    "ackMessageId": "msg_task_completed_001",
    "ackType": "agent.task.completed",
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "errorCode": "TASK_NOT_FOUND",
    "message": "task does not exist or does not belong to this cluster",
    "retryable": false
  }
}
```

响应规则：

- `platform.event.ack` 只表示平台已经收到并持久化该事件，不改变事件本身的业务含义。
- 对定时备份首次 `agent.velero.event`，平台应在幂等创建或查找到 backup task 后返回 ack，并在 ack payload 中带回平台侧 `taskId` 和 `commandId`。
- agent 收到定时备份首次 event 的 ack 后，后续同一 `veleroBackupName` 的事件应尽量携带平台返回的 `taskId` 和 `commandId`。
- `platform.event.error.retryable=true` 时，agent 必须继续重发该 event。
- `platform.event.error.retryable=false` 时，agent 停止重发，并记录本地错误。
- 平台不能在关键状态写入前返回 ack。
- 对 `agent.task.completed`，平台应在任务终态写入成功，且恢复点已创建/更新成功或已进入补偿队列后返回 ack。
- 对 `agent.task.failed`，平台应在失败状态和错误信息写入成功后返回 ack。

## 3. 平台主动发起的交互

本章只描述中控平台主动发起的 `request`，每个交互都按“请求、成功响应、失败响应、后续事件”成对展示。

### 3.1 下发任务：`platform.task.dispatch`

用途：平台要求 agent 执行一个任务，例如备份、恢复、演练、接管、存储同步、计划同步、保留清理或注销。

请求消息：

```json
{
  "version": "v1",
  "messageId": "msg_dispatch_backup_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "type": "backup",
    "deadline": "2026-07-01T00:30:00Z",
    "backup": {
      "planId": "plan_001",
      "sourceClusterId": "source_cluster_id",
      "sourceNamespace": "demo-mysql",
      "veleroBackupName": "hcdr-plan-plan001-20260701000034",
      "storageRepo": "my-minio"
    }
  }
}
```

接收确认响应：

```json
{
  "version": "v1",
  "messageId": "msg_task_accepted_001",
  "messageKind": "response",
  "type": "agent.task.accepted",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:01Z",
  "payload": {
    "ackMessageId": "msg_dispatch_backup_001",
    "ackType": "platform.task.dispatch",
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "acceptedAt": "2026-07-01T00:00:01Z"
  }
}
```

接收失败响应：

```json
{
  "version": "v1",
  "messageId": "msg_task_rejected_001",
  "messageKind": "response",
  "type": "agent.task.failed",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:01Z",
  "payload": {
    "ackMessageId": "msg_dispatch_backup_001",
    "ackType": "platform.task.dispatch",
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "errorCode": "INVALID_TASK_PAYLOAD",
    "message": "backup command is missing required field sourceNamespace",
    "details": {
      "missingFields": ["backup.sourceNamespace"]
    }
  }
}
```

后续进度事件：

```json
{
  "version": "v1",
  "messageId": "msg_task_progress_001",
  "messageKind": "event",
  "type": "agent.task.progress",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:10Z",
  "payload": {
    "ackRequired": false,
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "status": "running",
    "progress": 70,
    "totalBytes": 10737418240,
    "syncedBytes": 7516192768,
    "speedBytesPerSecond": 29625449,
    "percent": 70.0,
    "etaSeconds": 320,
    "message": "backup running"
  }
}
```

后续成功终态事件：

```json
{
  "version": "v1",
  "messageId": "msg_task_completed_001",
  "messageKind": "event",
  "type": "agent.task.completed",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:50Z",
  "payload": {
    "ackRequired": true,
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "status": "succeeded",
    "operation": "backup",
    "progress": 100,
    "message": "backup completed",
    "size": {
      "sizeStatus": "complete",
      "totalBytes": 10737442655,
      "metadataBytes": 24415,
      "volumeBytes": 10737418240,
      "uploadedBytes": 10737442655,
      "uploadedMetadataBytes": 24415,
      "uploadedVolumeBytes": 10737418240,
      "accuracy": {
        "totalBytes": "mixed",
        "metadataBytes": "accurate",
        "volumeBytes": "accurate",
        "uploadedBytes": "mixed",
        "uploadedMetadataBytes": "accurate",
        "uploadedVolumeBytes": "estimated"
      }
    }
  }
}
```

后续失败终态事件：

```json
{
  "version": "v1",
  "messageId": "msg_task_failed_001",
  "messageKind": "event",
  "type": "agent.task.failed",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:50Z",
  "payload": {
    "ackRequired": true,
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "errorCode": "VELERO_BACKUP_FAILED",
    "message": "velero backup failed",
    "details": {
      "phase": "Failed",
      "errors": 1
    }
  }
}
```

规则：

- `agent.task.accepted` 只表示 agent 接收任务，不表示任务成功。
- `agent.task.accepted` 是 `response`，但不是业务成功终态。
- `agent.task.progress` 是后续进度 `event`，`payload.ackRequired=false`，平台不返回响应。
- `agent.task.completed` 是后续成功终态 `event`，`payload.ackRequired=true`，平台必须返回 `platform.event.ack/error`。
- `agent.task.failed` 有两种语义：
  - 如果任务尚未被接受，用作 `platform.task.dispatch` 的接收失败 `response`。
  - 如果任务已被接受并开始执行，用作后续失败终态 `event`，`payload.ackRequired=true`，平台必须返回 `platform.event.ack/error`。
- agent 收到未知任务类型或 payload 缺失必填字段时，直接返回 `agent.task.failed` 作为本次请求的接收失败响应。
- 任务幂等使用 `payload.commandId`，不能使用 `messageId`。
- 进度 event 只携带轻量进度指标：`progress`、`totalBytes`、`syncedBytes`、`speedBytesPerSecond`、`percent`、`etaSeconds`。
- 备份任务的最终 size 明细只放在 `operation=backup` 的 `agent.task.completed` 终态事件中。

### 3.2 取消任务：`platform.task.cancel`

用途：预留。用于平台取消尚未完成的任务。取消协议未端到端实现前，不能把 stop/cancel 当作成功完成。

请求消息：

```json
{
  "version": "v1",
  "messageId": "msg_cancel_001",
  "messageKind": "request",
  "type": "platform.task.cancel",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:05:00Z",
  "payload": {
    "taskId": "task_cancel_001",
    "commandId": "cmd_cancel_001",
    "targetTaskId": "task_backup_001",
    "targetCommandId": "cmd_backup_001",
    "reason": "user requested cancellation"
  }
}
```

当前未实现时的失败响应：

```json
{
  "version": "v1",
  "messageId": "msg_agent_error_cancel_001",
  "messageKind": "response",
  "type": "agent.message.error",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:05:00Z",
  "payload": {
    "ackMessageId": "msg_cancel_001",
    "ackType": "platform.task.cancel",
    "taskId": "task_cancel_001",
    "commandId": "cmd_cancel_001",
    "errorCode": "UNSUPPORTED_MESSAGE",
    "message": "task cancellation is not implemented",
    "retryable": false
  }
}
```

取消实现后的成功响应：

```json
{
  "version": "v1",
  "messageId": "msg_cancel_accepted_001",
  "messageKind": "response",
  "type": "agent.task.accepted",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:05:01Z",
  "payload": {
    "ackMessageId": "msg_cancel_001",
    "ackType": "platform.task.cancel",
    "taskId": "task_cancel_001",
    "commandId": "cmd_cancel_001",
    "acceptedAt": "2026-07-01T00:05:01Z"
  }
}
```

规则：

- 当前版本应返回 `agent.message.error`，不能静默忽略。
- 取消实现后，取消任务自身也必须有 accepted 和终态事件。
- 被取消的原任务应进入明确的 `cancelled` 或 `failed`，不能伪装成 `succeeded`。

### 3.3 请求 Inventory：`platform.inventory.request`

用途：平台要求 agent 立即采集并返回 inventory。该请求不创建 task。

请求消息：

```json
{
  "version": "v1",
  "messageId": "msg_inventory_request_001",
  "messageKind": "request",
  "type": "platform.inventory.request",
  "tenantId": "tenant_id",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:10:00Z",
  "payload": {
    "requestId": "req_inventory_001",
    "scope": "namespaceResources",
    "namespace": "demo-mysql",
    "includeDetails": true,
    "reason": "user_refresh",
    "includeRecentVeleroObjects": true
  }
}
```

成功响应：

```json
{
  "version": "v1",
  "messageId": "msg_inventory_report_001",
  "messageKind": "response",
  "type": "agent.inventory.report",
  "tenantId": "tenant_id",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:10:03Z",
  "payload": {
    "ackMessageId": "msg_inventory_request_001",
    "ackType": "platform.inventory.request",
    "requestId": "req_inventory_001",
    "scope": "namespaceResources",
    "namespace": "demo-mysql",
    "collectedAt": "2026-07-01T00:10:02Z",
    "namespaceHash": "sha256:namespace_detail_hash",
    "resources": {
      "workloads": [],
      "network": [],
      "storage": [],
      "config": [],
      "rbac": [],
      "policy": [],
      "jobs": []
    },
    "sensitiveFieldsOmitted": true
  }
}
```

失败响应：

```json
{
  "version": "v1",
  "messageId": "msg_agent_error_inventory_001",
  "messageKind": "response",
  "type": "agent.message.error",
  "tenantId": "tenant_id",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:10:03Z",
  "payload": {
    "ackMessageId": "msg_inventory_request_001",
    "ackType": "platform.inventory.request",
    "requestId": "req_inventory_001",
    "errorCode": "INVENTORY_COLLECT_FAILED",
    "message": "failed to list namespaces",
    "retryable": true
  }
}
```

规则：

- 成功响应必须携带相同 `requestId`。
- 成功响应必须携带 `ackMessageId=请求 messageId`。
- `scope=summary` 表示请求当前全量 inventory summary。
- `scope=namespaceResources` 表示请求单个 namespace 的资源详情。
- `scope=fullDetail` 仅用于手动刷新、诊断或补偿，不作为页面默认请求。
- namespace 详情不能包含 Secret value、ConfigMap value、Pod logs、Events 全量或 Endpoint 明细。
- 失败时不使用 `agent.task.failed`，必须使用 `agent.message.error`。
- 如果失败响应 `retryable=true`，由平台按 UI 操作或退避策略重新发起新的 `platform.inventory.request`。
- agent 不主动重发 `agent.inventory.report` response，避免 response 重放导致页面状态混乱。
- 平台必须记录每次 inventory request 的状态，至少包括 `pending/succeeded/failed/timeout`。前端可以通过平台 API 查询该 request 状态，用于 Resource 弹窗展示刷新中、成功、失败或超时。

## 4. Agent 主动发起的交互与事件

本章描述 agent 主动发起的消息。`messageKind=request` 必须由平台响应；`messageKind=event` 是否需要响应由 `payload.ackRequired` 决定。

### 4.1 注册或重连：`agent.register`

用途：agent 启动、重启或 WebSocket 重连后向平台声明身份和当前集群摘要。

请求消息：

```json
{
  "version": "v1",
  "messageId": "msg_agent_register_001",
  "messageKind": "request",
  "type": "agent.register",
  "tenantId": "tenant_id",
  "clusterId": "",
  "agentId": "",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "requestId": "req_register_001",
    "installToken": "install_token_value",
    "agentCredential": "",
    "cluster": {
      "fingerprint": "cluster_fingerprint",
      "name": "prod-cluster-01",
      "kubeVersion": "v1.29.4",
      "nodeCount": 3,
      "namespaceCount": 42
    },
    "agent": {
      "version": "0.1.0",
      "namespace": "hypercdr-agent",
      "podName": "comm-agent-7c6b6d9b9c-abcde"
    },
    "velero": {
      "installed": true,
      "version": "v1.17.1",
      "status": "healthy"
    }
  }
}
```

成功响应：

```json
{
  "version": "v1",
  "messageId": "msg_register_accepted_001",
  "messageKind": "response",
  "type": "platform.register.accepted",
  "tenantId": "tenant_id",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:01Z",
  "payload": {
    "ackMessageId": "msg_agent_register_001",
    "ackType": "agent.register",
    "requestId": "req_register_001",
    "clusterId": "cluster_id",
    "agentId": "agent_id",
    "serverTime": "2026-07-01T00:00:01Z",
    "heartbeatIntervalSeconds": 30,
    "inventoryResyncIntervalSeconds": 300,
    "inventoryChangeDebounceSeconds": 8,
    "inventoryMinPushIntervalSeconds": 15,
    "protocolVersion": "v1",
    "features": {
      "taskDispatch": true,
      "inventoryReport": true,
      "veleroEvent": true,
      "sizeReport": true
    }
  }
}
```

失败响应：

```json
{
  "version": "v1",
  "messageId": "msg_register_rejected_001",
  "messageKind": "response",
  "type": "platform.register.rejected",
  "tenantId": "tenant_id",
  "clusterId": "",
  "agentId": "",
  "timestamp": "2026-07-01T00:00:01Z",
  "payload": {
    "ackMessageId": "msg_agent_register_001",
    "ackType": "agent.register",
    "requestId": "req_register_001",
    "errorCode": "INVALID_INSTALL_TOKEN",
    "message": "install token is invalid or expired",
    "retryable": false
  }
}
```

规则：

- `agent.register` 在首次启动、重启、重连时都必须发送。
- agent 收到 `platform.register.accepted` 后，才进入已连接/已认证状态。
- agent 收到 `platform.register.rejected` 后，不能执行平台任务。
- 首次注册可使用 `installToken`；后续重连应使用 `agentCredential + clusterId`。

### 4.2 心跳事件：`agent.heartbeat`

用途：周期性上报 agent 和集群健康状态。

事件消息：

```json
{
  "version": "v1",
  "messageId": "msg_heartbeat_001",
  "messageKind": "event",
  "type": "agent.heartbeat",
  "tenantId": "tenant_id",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:01:00Z",
  "payload": {
    "ackRequired": false,
    "status": "healthy",
    "agentVersion": "0.1.0",
    "veleroStatus": "healthy",
    "activeTasks": 1,
    "lastInventoryAt": "2026-07-01T00:00:30Z"
  }
}
```

规则：

- `messageKind=event`，平台不返回响应。
- `payload.ackRequired=false`，agent 不等待响应，也不重发。
- heartbeat 不是任务状态转换消息，不能用来标记任务完成。
- heartbeat 只承载 agent、连接和核心组件健康状态，不承载资源摘要。
- 平台收到后只更新 agent/cluster 健康状态。
- nodeCount、namespaceCount、applicationCount、inventoryHash 等资源信息必须通过 inventory summary 上报。

### 4.3 Inventory Summary 事件：`agent.inventory.report`

用途：agent 主动上报全量 inventory summary。按需 inventory 响应见 3.3。

事件消息：

```json
{
  "version": "v1",
  "messageId": "msg_inventory_report_periodic_001",
  "messageKind": "event",
  "type": "agent.inventory.report",
  "tenantId": "tenant_id",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:10:03Z",
  "payload": {
    "ackRequired": false,
    "scope": "summary",
    "reason": "changed",
    "collectedAt": "2026-07-01T00:10:02Z",
    "inventoryHash": "sha256:inventory_hash",
    "cluster": {
      "fingerprint": "cluster_fingerprint",
      "name": "prod-cluster-01",
      "kubeVersion": "v1.29.4",
      "nodeCount": 3,
      "namespaceCount": 42,
      "applicationCount": 28
    },
    "namespaces": [
      {
        "name": "demo-mysql",
        "status": "Active",
        "labels": {
          "app": "mysql"
        },
        "resourceSummary": {
          "deployments": 1,
          "statefulSets": 1,
          "daemonSets": 0,
          "services": 3,
          "ingresses": 1,
          "pvcs": 2,
          "configMaps": 5,
          "secrets": 4,
          "jobs": 0,
          "cronJobs": 0
        },
        "pvcSummary": {
          "count": 2,
          "requestedBytes": 21474836480,
          "boundCount": 2,
          "pendingCount": 0
        },
        "summaryHash": "sha256:namespace_summary_hash"
      }
    ],
    "velero": {
      "status": "healthy",
      "backupStorageLocations": [],
      "volumeSnapshotLocations": []
    }
  }
}
```

规则：

- `messageKind=event`，平台不返回响应。
- `payload.ackRequired=false`，agent 不等待响应，也不重发。
- 主动上报只传全量 summary，不传所有 namespace 的完整资源详情。
- 任意受关注资源变化导致 summaryHash/inventoryHash 变化时，agent debounce 后推送一份完整 summary。
- 无变化时，agent 每 `5min` 兜底推送一份完整 summary。
- inventory 可以用于补偿丢失的 Velero backup 事件，但不能绕过任务归属、ledger 和源集群校验。
- 创建恢复点前必须遵守源集群归属规则。

### 4.4 Inventory 观测范围、频度与性能保护

目标：agent 在不明显增加集群压力的前提下，保持平台上的集群、namespace、资源摘要足够新鲜，并支持三阶段 Resource 列和资源详情弹窗。

同步模型：

```text
agent watch 全集群受关注资源
 -> 本地维护 inventory summary cache
 -> 任意 summary 相关变化
 -> debounce 合并
 -> 推送一份全量 summary
 -> 无变化每 5min 推送一份全量 summary 兜底
 -> namespace 资源详情按需请求
```

默认频度：

| 类型 | 方向 | 触发 | 默认值 |
| --- | --- | --- | --- |
| heartbeat | agent -> 平台 | 固定周期 | `30s` |
| inventory summary | agent -> 平台 | summary hash 变化 | debounce `8s` |
| inventory summary 最小间隔 | agent -> 平台 | 限频 | `15s` |
| inventory summary 兜底 | agent -> 平台 | 无变化 | `5min` |
| namespace detail | 平台 -> agent -> 平台 | 用户打开弹窗/点击刷新/缓存过期 | 按需 |
| full detail | 平台 -> agent -> 平台 | 手动刷新/诊断/补偿 | 不自动 |

主动上报范围：

- 集群基本信息：kubeVersion、nodeCount、namespaceCount、applicationCount。
- 所有 namespace 的名称、状态、标签摘要。
- 每个 namespace 的资源计数 summary。
- 每个 namespace 的 PVC summary，包括数量、总申请容量、绑定状态统计。
- `inventoryHash` 和每个 namespace 的 `summaryHash`。
- Velero 健康摘要、BSL/VSL 摘要。

按需详情范围：

- `scope=namespaceResources` 时，返回单个 namespace 的资源清单。
- 资源按用户理解分组：workloads、network、storage、config、rbac、policy、jobs。
- ConfigMap 只返回名称、key 数量、引用关系和必要元数据，不返回 value。
- Secret 只返回名称、类型、key 数量、引用关系和必要元数据，不返回 value。
- Pod logs、Events 全量、Endpoint 明细默认不返回。

建议 watch 资源：

- Namespace、Node。
- Deployment、StatefulSet、DaemonSet。
- Service、Ingress。
- PVC、PV、StorageClass。
- ConfigMap metadata、Secret metadata。
- ServiceAccount、Role、RoleBinding。
- HPA、PDB、NetworkPolicy。
- Job、CronJob。
- Velero Backup、Schedule、Restore、BackupStorageLocation、VolumeSnapshotLocation、PodVolumeBackup、DataUpload。

谨慎 watch 资源：

- Pod：仅用于 Ready/Running/Restart 等摘要统计；不传完整 Pod spec。
- ReplicaSet：默认不作为 summary 必选资源，除非详情页需要。
- EndpointSlice：默认只计数，不传 endpoint 明细。

禁止自动采集/上传：

- Secret value。
- ConfigMap value。
- Pod logs。
- Events 全量。
- Endpoint 地址明细。

性能保护规则：

- agent 必须使用 shared informer/cache 模式，不能周期性对所有资源做全量 list。
- agent 启动或重连时允许初始 list，之后通过 watch 增量维护 cache。
- summaryHash 未变化时，不因单个 watch event 推送 inventory summary。
- 多个变化必须通过 debounce 合并。
- 两次 inventory summary 主动推送之间必须满足最小间隔，默认 `15s`。
- 大集群必须支持资源类型开关和 namespace 过滤。
- Kubernetes client 建议设置限流，例如 `QPS=5-10`、`Burst=10-20`。
- inventory 推送失败不重发历史 summary，下一次变化或 `5min` 兜底会覆盖。

### 4.5 任务进度与终态事件

用途：agent 在任务执行过程中异步上报进度和终态。

进度事件：

```json
{
  "version": "v1",
  "messageId": "msg_task_progress_001",
  "messageKind": "event",
  "type": "agent.task.progress",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:10Z",
  "payload": {
    "ackRequired": false,
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "status": "running",
    "progress": 70,
    "totalBytes": 10737418240,
    "syncedBytes": 7516192768,
    "speedBytesPerSecond": 29625449,
    "percent": 70.0,
    "etaSeconds": 320,
    "message": "backup running"
  }
}
```

成功终态事件：

```json
{
  "version": "v1",
  "messageId": "msg_task_completed_001",
  "messageKind": "event",
  "type": "agent.task.completed",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:50Z",
  "payload": {
    "ackRequired": true,
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "status": "succeeded",
    "operation": "backup",
    "progress": 100,
    "message": "backup completed",
    "size": {
      "sizeStatus": "complete",
      "totalBytes": 10737442655,
      "metadataBytes": 24415,
      "volumeBytes": 10737418240,
      "uploadedBytes": 10737442655,
      "uploadedMetadataBytes": 24415,
      "uploadedVolumeBytes": 10737418240,
      "accuracy": {
        "totalBytes": "mixed",
        "metadataBytes": "accurate",
        "volumeBytes": "accurate",
        "uploadedBytes": "mixed",
        "uploadedMetadataBytes": "accurate",
        "uploadedVolumeBytes": "estimated"
      }
    }
  }
}
```

失败终态事件：

```json
{
  "version": "v1",
  "messageId": "msg_task_failed_001",
  "messageKind": "event",
  "type": "agent.task.failed",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:50Z",
  "payload": {
    "ackRequired": true,
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "errorCode": "VELERO_BACKUP_FAILED",
    "message": "velero backup failed",
    "details": {
      "phase": "Failed",
      "errors": 1
    }
  }
}
```

规则：

- `agent.task.progress` 使用 `payload.ackRequired=false`，平台不返回响应，agent 不重发。
- `agent.task.completed` 和 `agent.task.failed` 使用 `payload.ackRequired=true`，平台必须返回 `platform.event.ack` 或 `platform.event.error`。
- 终态事件未收到 ack/error 时，agent 必须重发。
- `agent.task.*` 事件必须携带 `taskId` 和 `commandId`，用于关联平台任务。
- `progress` 不应倒退，失败终态除外。
- `progress=100` 不能单独作为成功依据。
- 进度 event 只携带轻量进度指标：`progress`、`totalBytes`、`syncedBytes`、`speedBytesPerSecond`、`percent`、`etaSeconds`。
- 备份任务的最终 size 明细只放在 `operation=backup` 的 `agent.task.completed` 终态事件中，包括 `totalBytes`、`metadataBytes`、`volumeBytes`、`uploadedBytes`、`uploadedMetadataBytes`、`uploadedVolumeBytes`。
- 只有 `agent.task.completed` 或标准完成事件才能把任务置为 `succeeded`。
- 失败事件必须携带 `errorCode` 和用户可理解的 `message`。

### 4.6 Velero 观测事件：`agent.velero.event`

用途：agent 观测到 Velero Backup/Restore 等 CR 状态变化后异步上报。

手动备份 Velero 事件示例：

```json
{
  "version": "v1",
  "messageId": "msg_velero_event_manual_001",
  "messageKind": "event",
  "type": "agent.velero.event",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:50Z",
  "payload": {
    "ackRequired": true,
    "eventType": "backup_completed",
    "backupName": "hcdr-plan-plan001-20260701000034",
    "namespace": "hypercdr-agent",
    "planId": "plan_001",
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "phase": "Completed",
    "progress": 100,
    "message": "Velero backup Completed: 22 / 22 items",
    "resourceVersion": "9573570",
    "storageLocation": "my-minio",
    "includedNamespaces": ["demo"],
    "startedAt": "2026-07-01T00:00:34Z",
    "completedAt": "2026-07-01T00:00:50Z",
    "labels": {
      "hypercdr.io/managed-by": "hypercdr",
      "hypercdr.io/plan-id": "plan_001",
      "hypercdr.io/task-id": "task_backup_001",
      "hypercdr.io/command-id": "cmd_backup_001",
      "hypercdr.io/source-cluster-id": "source_cluster_id",
      "hypercdr.io/source-namespace": "demo"
    },
    "velero": {}
  }
}
```

定时备份首次 Velero 事件示例：

```json
{
  "version": "v1",
  "messageId": "msg_velero_event_schedule_001",
  "messageKind": "event",
  "type": "agent.velero.event",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T01:00:50Z",
  "payload": {
    "ackRequired": true,
    "eventType": "backup_completed",
    "backupName": "hcdr-plan-plan001-20260701010000",
    "namespace": "hypercdr-agent",
    "planId": "plan_001",
    "scheduleName": "hcdr-plan-plan001",
    "sourceClusterId": "source_cluster_id",
    "sourceNamespace": "demo",
    "phase": "Completed",
    "progress": 100,
    "message": "Velero scheduled backup Completed: 22 / 22 items",
    "resourceVersion": "9574999",
    "storageLocation": "my-minio",
    "includedNamespaces": ["demo"],
    "startedAt": "2026-07-01T01:00:00Z",
    "completedAt": "2026-07-01T01:00:50Z",
    "labels": {
      "hypercdr.io/managed-by": "hypercdr",
      "hypercdr.io/plan-id": "plan_001",
      "hypercdr.io/source-cluster-id": "source_cluster_id",
      "hypercdr.io/source-namespace": "demo",
      "velero.io/schedule-name": "hcdr-plan-plan001"
    },
    "velero": {}
  }
}
```

规则：

- `messageKind=event`，是否响应由 `payload.ackRequired` 决定。
- `eventType` 取值包括 `backup_started`、`backup_progress`、`backup_completed`、`backup_failed`。
- `New` 或空 phase 映射为 `backup_started`。
- `InProgress` 映射为 `backup_progress`。
- `Completed` 映射为 `backup_completed`。
- `Failed`、`FailedValidation`、`PartiallyFailed`、`Canceled` 映射为 `backup_failed`。
- `backup_progress` 使用 `payload.ackRequired=false`，平台不返回响应。
- `backup_completed`、`backup_failed`、`restore_completed`、`restore_failed` 使用 `payload.ackRequired=true`，平台必须返回 `platform.event.ack` 或 `platform.event.error`。
- `clusterId != protectionPlan.sourceClusterId` 时不能创建恢复点。
- 平台下发任务产生的 Velero 事件必须携带 `taskId` 和 `commandId`。
- 定时备份产生的 Velero 事件首次上报时可以没有 `taskId` 和 `commandId`，但必须携带 `planId`、`scheduleName`、`backupName`、`sourceClusterId/sourceNamespace` 或等价标签。
- 定时备份事件的业务幂等键是 `sourceClusterId + planId + veleroBackupName`。

## 5. 备份执行流程

### 5.1 手动备份

1. 用户在 DR 阶段三点击 Start Sync。
2. 平台创建 `backup` task，状态为 `queued`。
3. 平台向源集群 agent 下发 `platform.task.dispatch`。
4. WebSocket 发送成功后，平台将任务置为 `dispatched`。
5. agent 上报 `agent.task.accepted`。
6. agent 创建 Velero `Backup`。
7. agent 上报 `agent.task.progress` 或 `agent.velero.event`。
8. Velero Backup 进入 `Completed`。
9. agent 上报 `agent.task.completed` 或 `backup_completed`。
10. 平台将任务置为 `succeeded`。
11. 平台按 `(sourceClusterId, veleroBackupName)` 创建或更新恢复点。
12. 页面显示 `Sync complete`。

手动备份恢复点创建规则：

- backup task 由平台先创建。
- agent 上报的进度和终态必须能匹配已有 `taskId + commandId`。
- 平台收到合法成功终态后，才创建或更新恢复点。
- 如果 agent 上报终态时 task 不存在，平台不能直接创建恢复点，应返回 `platform.event.error retryable=false`，错误码为 `TASK_NOT_FOUND`，除非该事件属于定时备份的合法 event 驱动场景。
- 手动备份以 `agent.task.completed/failed` 作为任务终态权威事件。
- 同一个手动备份如果同时收到 `agent.task.completed` 和 `agent.velero.event backup_completed`，平台必须按 `taskId + commandId + veleroBackupName` 幂等合并，不能重复创建 task event 或恢复点。
- 手动备份的 `agent.velero.event` 可以用于补充 Velero 细节、volume progress 和 size，但不能绕过 task 归属校验。

### 5.2 定时备份

1. 计划激活时通过 `schedule-sync` 创建或更新 Velero `Schedule`。
2. Velero 按 schedule 创建 Backup CR。
3. 源集群 agent 观测 Backup CR。
4. agent 上报 `agent.velero.event`。
5. 平台找到或创建对应 `backup` task。
6. 平台在 `backup_completed` 时创建或更新恢复点。

定时备份任务创建规则：

- `schedule-sync` task 只表示定时计划配置成功，不表示某一次备份执行成功。
- 每一次定时备份执行对应一条独立 `backup` task。
- 该 `backup` task 由平台在第一次收到合法的定时备份 `agent.velero.event` 时创建。
- 如果平台第一次收到的就是 `backup_completed`，也必须能补建本次 `backup` task，然后再按同一事务或同一处理流程创建恢复点。
- 后续相同 `sourceClusterId + planId + veleroBackupName` 的事件只能更新已有 task，不能重复创建 task。
- 定时备份从开始到结束通过 event 上传，不需要平台为每次定时执行提前 dispatch `backup` task。

定时备份必须携带足够标签：

- `hypercdr.io/managed-by=hypercdr`
- `hypercdr.io/plan-id`
- `hypercdr.io/source-cluster-id`
- `hypercdr.io/source-namespace`
- `velero.io/storage-location`

定时备份恢复点创建规则：

- agent 只上报 Backup CR 事实，不创建恢复点。
- 平台校验 Backup CR 属于 HyperCDR 管理的 Schedule。
- 平台校验 `event.clusterId == protectionPlan.sourceClusterId`。
- 平台校验 `backupName/sourceClusterId/planId/sourceNamespace` 与计划一致。
- 平台收到合法 `backup_completed` 后，按 `(sourceClusterId, veleroBackupName)` upsert 恢复点。
- 平台收到 `backup_failed` 时，只更新或创建失败的 backup task，不创建 `available` 恢复点。
- 目标集群上同名 Backup CR 的 event 必须拒绝或忽略，不能创建 task，也不能创建恢复点。

## 6. Restore / Drill / Takeover 流程

1. 用户选择一个 `available` 恢复点。
2. 平台创建 `restore`、`drill` 或 `takeover` task。
3. 平台向目标集群 agent 下发 `restore` 命令体。
4. agent 等待目标集群 Velero namespace 中可见该 backup。
5. agent 创建 Velero `Restore`。
6. agent 基于 Velero Restore 和 PodVolumeRestore 上报进度。
7. Velero `Completed` 后 agent 上报 `agent.task.completed`。
8. 平台将任务置为 `succeeded`。
9. 页面在任务历史中显示恢复/演练/接管成功。

Restore 类任务不创建恢复点。

## 7. Retention Cleanup 流程

1. 平台按计划保留策略选中过期恢复点。
2. 平台在源集群创建 `retention-cleanup` task。
3. agent 创建 Velero `DeleteBackupRequest` 或等价删除动作。
4. agent 上报删除进度。
5. 成功后，平台将对应恢复点标记为 `deleted`。

清理任务不能删除其他源集群的恢复点。

## 8. Size 与进度约定

第一版恢复点 Size 由 metadata size 和 volume 逻辑 size 组成。

原因：

- Velero metadata 通常远小于 PVC 卷数据，常见为 KB 到数 MB。
- metadata size 获取成本低，可以通过对象存储接口准确统计。
- Velero backup detail 可以提供 volume info，但没有稳定字段直接返回 metadata size。
- volume 的逻辑 size 从 Velero backup detail / BackupVolumeInfos 获取。

第一版字段含义：

- `totalBytes`：恢复点主 Size，等于 `metadataBytes + volumeBytes`。
- `metadataBytes`：Velero metadata 备份制品大小，通过对象存储接口统计 `backups/<backupName>/` 下 metadata artifact 的对象大小。
- `volumeBytes`：本次备份关联的卷逻辑总量。
- `uploadedBytes`：等于 `uploadedMetadataBytes + uploadedVolumeBytes`。
- `uploadedMetadataBytes`：第一版与 `metadataBytes` 相同。
- `uploadedVolumeBytes`：Kopia/FSB 真实新增上传量暂不能准确取得，第一版用 `volumeBytes` 估算。

标准 payload：

```json
{
  "size": {
    "sizeStatus": "complete",
    "totalBytes": 734748314,
    "metadataBytes": 24415,
    "volumeBytes": 734723899,
    "uploadedBytes": 734748314,
    "uploadedMetadataBytes": 24415,
    "uploadedVolumeBytes": 734723899,
    "accuracy": {
      "totalBytes": "mixed",
      "metadataBytes": "accurate",
      "volumeBytes": "accurate",
      "uploadedBytes": "mixed",
      "uploadedMetadataBytes": "accurate",
      "uploadedVolumeBytes": "estimated"
    },
    "sources": {
      "metadataBytes": "objectStoreBackupArtifacts",
      "volumeBytes": "veleroBackupVolumeInfos",
      "uploadedMetadataBytes": "objectStoreBackupArtifacts",
      "uploadedVolumeBytes": "estimatedFromVolumeBytes"
    }
  }
}
```

当前 FSB/Kopia 规则：

- metadata size 通过对象存储接口统计 `backups/<backupName>/` 下的 Velero metadata artifact 大小。
- metadata size 的统计对象应包含该 backupName 下 Velero 写入的元数据对象，例如 backup tar、logs、results、volumeinfo、CSI snapshot info 等。
- metadata size 获取失败不改变 Velero 备份成功事实，但平台应记录 size 采集失败告警，并允许后续补偿采集。
- metadata size 获取失败时，平台可以先创建恢复点，但 `sizeStatus` 应标记为 `partial` 或 `collecting`，页面不能把该 size 当作完整准确值。
- metadata size 补偿采集成功后，平台更新恢复点 `metadataBytes/totalBytes/uploadedMetadataBytes/uploadedBytes/sizeStatus`。
- volume 逻辑 size 优先从 Velero BackupVolumeInfos 获取。
- BackupVolumeInfos 内部来自 PodVolumeBackup/DataUpload 的 `progress.totalBytes`、snapshot 信息等。
- 如果 BackupVolumeInfos 中缺失 size，可回退到 PodVolumeBackup/DataUpload CR 的 `status.progress.totalBytes`。
- 如果 CR 进度也缺失，可用 `snapshotID` 查询 Kopia snapshot stats 作为补偿。
- volume 上传 size 当前不一定能从 Velero 直接得到；没有 Kopia 专用 reader 前，可以用原始 volume size 估算，但必须标记为 `estimated`。
- `sizeStatus` 建议取值：`complete`、`partial`、`collecting`、`failed`。

页面必须在详情或 tooltip 中展示准确度，不能把估算值当成精确值。

## 9. 页面状态映射

页面必须基于后端 task 和 restore point 状态展示，不能基于任意事件文案展示。

### 9.1 DR 阶段三 Sync 列

对每个 namespace/application：

1. 存在 active backup task：
   - 显示 `Syncing... N%`
   - 进度来源是最新 task progress 或 volume progress。
2. 最新 backup task 是 `succeeded`：
   - 显示 `Sync complete`
   - 副文本显示匹配恢复点时间、最新恢复点时间，或 `Restore point indexed`
   - 绝不能再显示运行中进度条。
3. 最新 backup task 是 `failed`：
   - 显示 `Sync failed`
   - 展示错误信息和任务详情。
4. 没有 backup task 但存在可用恢复点：
   - 显示 `Last snapshot`
5. 其他情况：
   - 显示 `No snapshot yet`

`Finalizing restore point` 不是合法最终 UI 状态。只有后端 task 仍处于 active 状态时，才可以短暂显示。

### 9.2 Restore Points 页面

默认列表：

- 只展示 `restore_points.status = available`。
- 集群过滤必须使用 `sourceClusterId`。
- namespace 过滤使用 `sourceNamespace` 或 included namespaces。

默认恢复点列表不能展示 failed、deleted、clearing 或目标集群备份制品。

### 9.3 Backup and Restore Tasks 页面

这是任务历史页面，可以展示：

- 成功任务。
- 失败任务和错误信息。
- 运行中任务。
- 恢复点已删除的任务关系，显示为已清除/已删除。

失败备份任务不要求必须存在恢复点。

默认时间窗口应有边界，例如最近 24 小时或最近 7 天，并允许用户切换。

## 10. 目标任务 JSON 定义

本节定义中控平台下发给 agent 的目标 JSON 格式。所有任务都通过 `platform.task.dispatch` 下发，具体任务内容放在统一消息封装的 `payload` 中。

### 10.1 统一消息封装

```json
{
  "version": "v1",
  "messageId": "msg_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "execute_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {}
}
```

字段定义：

- `version`：协议版本，当前为 `v1`。
- `messageId`：逻辑消息 ID，必须全局唯一。同一条逻辑消息重发时保持不变。
- `messageKind`：通信语义，取值为 `request`、`response`、`event`。
- `type`：消息类型。任务下发固定为 `platform.task.dispatch`。
- `tenantId`：租户 ID。
- `clusterId`：执行任务的集群 ID。
- `agentId`：目标 agent ID，可为空。
- `timestamp`：消息发送时间，UTC。
- `payload`：任务内容。

`payload` 的基础结构：

```json
{
  "taskId": "task_id",
  "commandId": "command_id",
  "type": "backup",
  "deadline": "2026-07-01T00:30:00Z"
}
```

字段定义：

- `taskId`：平台任务 ID，必填。
- `commandId`：本次命令 ID，必填。用于幂等、日志关联、消息关联。
- `type`：任务类型，必填。
- `deadline`：任务截止时间，必填。agent 超时后必须失败任务，而不是无限等待。

目标规则：

- 每个 `platform.task.dispatch` 只能携带一个具体命令体。
- `payload.type=backup` 时必须携带 `payload.backup`。
- `payload.type=restore/drill/takeover` 时必须携带 `payload.restore`。
- `payload.type=storage-sync` 时必须携带 `payload.storageSync`。
- `payload.type=schedule-sync` 时必须携带 `payload.scheduleSync`。
- `payload.type=retention-cleanup` 时必须携带 `payload.retentionCleanup`。
- `payload.type=unregister` 时必须携带 `payload.unregister`。
- agent 收到未知任务类型必须返回 `agent.task.failed`，不能静默忽略。

### 10.2 通用 Agent 回包

每个任务的标准回包流程：

```text
agent.task.accepted   response，表示 agent 接收任务，不代表执行成功
agent.task.progress   event，payload.ackRequired=false，可出现 0 次或多次
agent.task.completed  event，payload.ackRequired=true，成功终态
agent.task.failed     response 或 event：接收失败时是 response，执行失败时是 payload.ackRequired=true 的 event
```

`agent.task.accepted`：

```json
{
  "version": "v1",
  "messageId": "msg_accepted_001",
  "messageKind": "response",
  "type": "agent.task.accepted",
  "tenantId": "tenant_id",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:01Z",
  "payload": {
    "ackMessageId": "msg_001",
    "ackType": "platform.task.dispatch",
    "taskId": "task_id",
    "commandId": "command_id",
    "acceptedAt": "2026-07-01T00:00:01Z"
  }
}
```

`agent.task.progress`：

```json
{
  "version": "v1",
  "messageId": "msg_progress_001",
  "messageKind": "event",
  "type": "agent.task.progress",
  "tenantId": "tenant_id",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:10Z",
  "payload": {
    "ackRequired": false,
    "taskId": "task_id",
    "commandId": "command_id",
    "status": "running",
    "progress": 70,
    "totalBytes": 10737418240,
    "syncedBytes": 7516192768,
    "speedBytesPerSecond": 29625449,
    "percent": 70.0,
    "etaSeconds": 320,
    "message": "backup running"
  }
}
```

目标统一备份完成事件：`agent.backup.completed`

用途：手动备份和定时备份成功完成后，agent 使用同一个 JSON 格式上报。通过 `payload.triggerType` 区分触发来源：

- `manual`：平台下发的手动同步任务。
- `schedule`：Velero Schedule 自动触发的定时备份。

进度明细只出现在 `agent.task.progress` 或 Velero progress 事件中；完成事件只携带最终结果、恢复点 size 和 plan 对象存储总占用。

```json
{
  "version": "v1",
  "messageId": "msg_backup_completed_001",
  "messageKind": "event",
  "type": "agent.backup.completed",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:50Z",
  "payload": {
    "ackRequired": true,
    "planId": "plan_001",
    "triggerType": "manual",
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "scheduleName": "",
    "status": "succeeded",
    "progress": 100,
    "message": "backup completed",
    "sourceClusterId": "source_cluster_id",
    "sourceNamespaces": ["demo"],
    "primarySourceNamespace": "demo",
    "storageLocation": "my-minio",
    "backup": {
      "name": "hcdr-57d00ddd027e40389218a93166bb7858-m-20260701000034-abcd1234",
      "namespace": "hypercdr-agent",
      "phase": "Completed",
      "startedAt": "2026-07-01T00:00:34Z",
      "completedAt": "2026-07-01T00:00:50Z",
      "resourceVersion": "9573570",
      "errors": 0,
      "warnings": 0,
      "itemsTotal": 22,
      "itemsDone": 22,
      "labels": {
        "hypercdr.io/managed-by": "hypercdr",
        "hypercdr.io/plan-id": "plan_001",
        "hypercdr.io/source-cluster-id": "source_cluster_id",
        "hypercdr.io/source-namespace": "demo",
        "hypercdr.io/backup-mode": "manual"
      }
    },
    "sizeStatus": "complete",
    "sizeWarnings": [],
    "restorePointSize": {
      "totalBytes": 10737442655,
      "metadataBytes": 24415,
      "volumeBytes": 10737418240,
      "uploadedBytes": 10737442655,
      "uploadedMetadataBytes": 24415,
      "uploadedVolumeBytes": 10737418240
    },
    "planStorageSize": {
      "totalBytes": 53687091200,
      "metadataBytes": 104857600,
      "kopiaBytes": 53582233600
    }
  }
}
```

定时备份使用同一格式，仅触发字段不同：

```json
{
  "triggerType": "schedule",
  "taskId": "",
  "commandId": "",
  "scheduleName": "hcdr-57d00ddd027e40389218a93166bb7858"
}
```

字段说明：

- `status` 是备份完成事件状态，成功固定为 `succeeded`。
- `sizeStatus` 是 size 统计状态，取值为 `complete`、`partial`、`unavailable`。
- `sizeWarnings` 只有在 `sizeStatus != complete` 时填写，用于说明哪个统计范围失败或不完整。
- `restorePointSize` 表示本次恢复点大小，不再包含单独的 `status` 或 `accuracy`。
- `planStorageSize` 表示该 plan 当前在对象存储中的累计占用，不再包含单独的 `status` 或 `accuracy`。
- `restorePointSize.uploadedVolumeBytes` 当前在 Kopia 场景下可先使用 `volumeBytes` 替代，若无法准确获取，应通过 `sizeStatus=partial` 和 `sizeWarnings` 说明。

`agent.task.failed`：

```json
{
  "version": "v1",
  "messageId": "msg_failed_001",
  "messageKind": "event",
  "type": "agent.task.failed",
  "tenantId": "tenant_id",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:50Z",
  "payload": {
    "ackRequired": true,
    "taskId": "task_id",
    "commandId": "command_id",
    "errorCode": "BACKUP_FAILED",
    "message": "velero backup failed",
    "details": {
      "phase": "Failed",
      "errors": 1
    }
  }
}
```

回包规则：

- `accepted` 只能表示 agent 接收了任务，不代表任务开始成功。
- `accepted` 是 response，必须携带 `ackMessageId` 指向 `platform.task.dispatch`。
- `progress` 是 `payload.ackRequired=false` 的 event，平台不返回响应。
- `completed` 是 `payload.ackRequired=true` 的 event，平台必须返回 `platform.event.ack/error`。
- `failed` 如果表示接收失败，则是 response，必须携带 `ackMessageId`。
- `failed` 如果表示执行失败，则是 `payload.ackRequired=true` 的 event，平台必须返回 `platform.event.ack/error`。
- `progress.status` 目标值统一使用 `running`。
- 只有 `agent.task.completed` 或标准完成事件才能把任务置为 `succeeded`。
- `progress=100` 不能单独作为成功依据。
- `failed` 必须带 `errorCode` 和用户可理解的 `message`。
- `details` 用于机器可读诊断，不应替代 `message`。

### 10.3 标准 Velero Payload

`velero` 字段用于描述 agent 观测到的 Velero 对象状态。目标上应逐步从 `map` 收敛为稳定结构。

```json
{
  "kind": "Backup",
  "name": "hcdr-plan-plan001-20260701000034",
  "namespace": "hypercdr-agent",
  "phase": "Completed",
  "resourceVersion": "9573570",
  "storageLocation": "my-minio",
  "includedNamespaces": ["demo-mysql-csi"],
  "labels": {
    "hypercdr.io/managed-by": "hypercdr",
    "hypercdr.io/plan-id": "plan_001",
    "hypercdr.io/task-id": "task_backup_001",
    "hypercdr.io/command-id": "cmd_backup_001",
    "hypercdr.io/source-cluster-id": "source_cluster_id",
    "hypercdr.io/source-namespace": "demo-mysql-csi",
    "velero.io/storage-location": "my-minio"
  },
  "startedAt": "2026-07-01T00:00:34Z",
  "completedAt": "2026-07-01T00:00:50Z",
  "itemsTotal": 22,
  "itemsDone": 22,
  "errors": 0,
  "warnings": 0,
  "volumeProgress": {},
  "size": {}
}
```

`volumeProgress` 标准结构：

```json
{
  "operation": "backup",
  "bytesDone": 734723899,
  "totalBytes": 734723899,
  "knownTotal": true,
  "allTotalsKnown": true,
  "knownTotalCount": 1,
  "unknownTotalCount": 0,
  "percent": 100,
  "speedBytesPerSecond": 29625449,
  "etaSeconds": 0,
  "itemCount": 1,
  "runningCount": 0,
  "completedCount": 1,
  "failedCount": 0,
  "items": [
    {
      "kind": "PodVolumeBackup",
      "name": "backup-pvb",
      "phase": "Completed",
      "bytesDone": 734723899,
      "totalBytes": 734723899,
      "knownTotal": true,
      "message": ""
    }
  ]
}
```

`size` 标准结构：

```json
{
  "sizeStatus": "complete",
  "totalBytes": 734748314,
  "metadataBytes": 24415,
  "volumeBytes": 734723899,
  "uploadedBytes": 734748314,
  "uploadedMetadataBytes": 24415,
  "uploadedVolumeBytes": 734723899,
  "sources": {
    "metadataBytes": "objectStoreBackupArtifacts",
    "volumeBytes": "veleroBackupVolumeInfos",
    "uploadedMetadataBytes": "objectStoreBackupArtifacts",
    "uploadedVolumeBytes": "estimatedFromVolumeBytes"
  },
  "accuracy": {
    "totalBytes": "mixed",
    "metadataBytes": "accurate",
    "volumeBytes": "accurate",
    "uploadedBytes": "mixed",
    "uploadedMetadataBytes": "accurate",
    "uploadedVolumeBytes": "estimated"
  },
  "metadataPrefix": "backups/hcdr-plan-plan001-20260701000034/",
  "storageLocation": "my-minio"
}
```

## 11. 各任务目标定义

### 11.1 `storage-sync`

用途：把对象存储配置和凭据下发到集群，创建或更新 Velero `BackupStorageLocation`。

执行集群：需要使用该存储仓库的集群。源集群和目标集群都可能需要执行。

源/目标集群职责：

- 源集群 `storage-sync` 是备份链路的前置条件。源集群 BSL 不可用时，不能下发或继续执行 `schedule-sync`，DR Config 不能进入 `active/ready`。
- 目标集群 `storage-sync` 是恢复、演练、接管链路的前置条件。目标集群 BSL 不可用时，不阻塞源集群 `schedule-sync`，也不阻塞 DR Config 进入 `active/ready`，但 DR Config 必须携带 warning。
- 目标集群 BSL warning 必须明确说明受影响能力是恢复、演练、接管，而不是定时备份。
- 页面展示 DR Config 时，应在主状态显示 `Ready`，并以 warning 图标或弱提示显示目标集群存储配置异常；鼠标悬浮时展示目标集群名称、BSL 名称、失败码、失败信息和影响范围。
- `storage-sync` 失败后平台应自动重试，最多 3 次。3 次仍失败后进入最终失败处理：源集群失败时 DR Config 显示 storage failed；目标集群失败时 DR Config 显示 Ready with warning。
- 最终失败后页面必须提供手动重新下发 BSL 的入口。源集群手动重试用于恢复备份链路；目标集群手动重试用于清除 Ready warning 并恢复 restore/drill/takeover 能力。

命令体字段：

- `repositoryId`：平台存储仓库 ID，必填。
- `name`：Velero `BackupStorageLocation` 名称，必填。
- `type`：存储类型，当前主要是 `S3`。
- `endpoint`：对象存储 endpoint。
- `bucket`：bucket 名称，必填。
- `region`：region，可选。
- `tlsEnabled`：是否启用 TLS。
- `secretRef`：Kubernetes Secret 名称。
- `credentials.accessKey`：访问密钥，仅任务下发时携带。
- `credentials.secretKey`：访问密钥，仅任务下发时携带。
- `config`：额外配置。

请求样例：

```json
{
  "version": "v1",
  "messageId": "msg_storage_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_storage_001",
    "commandId": "cmd_storage_001",
    "type": "storage-sync",
    "deadline": "2026-07-01T00:10:00Z",
    "storageSync": {
      "repositoryId": "repo_001",
      "name": "my-minio",
      "type": "S3",
      "endpoint": "http://minio.example.local:9000",
      "bucket": "velero",
      "region": "us-east-1",
      "tlsEnabled": false,
      "secretRef": "hypercdr-repo-my-minio",
      "credentials": {
        "accessKey": "minioadmin",
        "secretKey": "minioadmin"
      },
      "config": {
        "s3ForcePathStyle": "true",
        "publicUrl": "http://minio.example.local:9000"
      }
    }
  }
}
```

目标状态转换：

```text
queued -> dispatched -> accepted -> running -> succeeded
queued -> dispatched -> accepted -> running -> failed
```

成功条件：

- Secret/配置写入成功。
- Velero `BackupStorageLocation` 状态为 `Available`。

失败错误码建议：

- `STORAGE_SYNC_COMMAND_INVALID`
- `BSL_MANIFEST_INVALID`
- `BSL_SUBMIT_FAILED`
- `BSL_STATUS_READ_FAILED`
- `BSL_STATUS_TIMEOUT`
- `BSL_UNAVAILABLE`

### 11.2 `schedule-sync`

用途：为保护计划创建或更新 Velero `Schedule`，用于定时备份。

执行集群：源集群。

命令体字段：

- `planId`：保护计划 ID，必填。
- `scheduleName`：Velero Schedule 名称，必填。
- `cron`：cron 表达式，必填。
- `sourceNamespace`：要备份的 namespace，必填。
- `sourceNamespaces`：预留，多 namespace 计划使用；单 namespace 计划不使用。
- `scope`：备份范围，例如 `namespace`。
- `labelSelector`：标签选择器，可选。
- `storageRepo`：Velero `BackupStorageLocation` 名称，必填。
- `includeClusterResources`：是否包含集群级资源。
- `excludeResources`：排除资源规则。
- `hooks`：备份 hooks。

请求样例：

```json
{
  "version": "v1",
  "messageId": "msg_schedule_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_schedule_001",
    "commandId": "cmd_schedule_001",
    "type": "schedule-sync",
    "deadline": "2026-07-01T00:10:00Z",
    "scheduleSync": {
      "planId": "plan_001",
      "scheduleName": "hcdr-plan-plan001",
      "cron": "0 * * * *",
      "sourceNamespace": "demo-mysql-csi",
      "scope": "namespace",
      "labelSelector": "",
      "storageRepo": "my-minio",
      "includeClusterResources": true,
      "excludeResources": [
        {
          "group": "",
          "resource": "events",
          "name": "",
          "version": "v1",
          "labels": ""
        }
      ],
      "hooks": {
        "pre": [],
        "post": []
      }
    }
  }
}
```

目标状态转换：

```text
queued -> dispatched -> accepted -> running -> succeeded
queued -> dispatched -> accepted -> running -> failed
```

成功条件：

- 目标 BSL 可用。
- Velero `Schedule` apply 成功。
- Schedule 必须携带 `hypercdr.io/source-cluster-id`、`hypercdr.io/plan-id`、`hypercdr.io/source-namespace` 等标签。

注意：

- `schedule-sync` 成功只代表 schedule 配置成功，不代表已经产生备份。
- 计划状态可以由 `schedule-sync` 成功后变为 `active`。
- agent 创建 Schedule 时必须确保 Schedule 产生的 Backup CR 能被识别为 HyperCDR 受管备份。
- 如果 Velero 支持 Schedule template metadata，agent 应把 `hypercdr.io/managed-by`、`hypercdr.io/source-cluster-id`、`hypercdr.io/plan-id`、`hypercdr.io/source-namespace` 写入 template metadata，使 Backup CR 继承。
- 如果当前 Velero 版本或插件不能自动继承标签，agent 必须在观测 Backup CR 时通过 Schedule owner/label/名称关系补齐归属判断，不能仅凭 backupName 猜测。

失败错误码建议：

- `SCHEDULE_SYNC_COMMAND_INVALID`
- `BSL_NOT_READY`
- `SCHEDULE_MANIFEST_INVALID`
- `SCHEDULE_APPLY_FAILED`
- `SCHEDULE_STATUS_TIMEOUT`

### 11.3 `backup`

用途：手动触发一次备份，创建 Velero `Backup`。

执行集群：源集群。

命令体字段：

- `planId`：保护计划 ID，必填。
- `sourceNamespace`：要备份的 namespace，必填。
- `scope`：备份范围，例如 `namespace`。
- `labelSelector`：标签选择器，可选。
- `storageRepo`：Velero `BackupStorageLocation` 名称，必填。
- `includeClusterResources`：是否包含集群级资源。
- `excludeResources`：排除资源规则。
- `hooks`：备份 hooks。

TTL 规则：

- HyperCDR 备份不使用 Velero Backup TTL 作为恢复点保留机制。
- Velero Backup 创建时不应设置短 TTL；目标上应不设置 TTL，或设置为等价不过期的值。
- 恢复点保留、过期、删除必须由平台 `retention-cleanup` 统一控制。
- 不能让 Velero 自动 TTL 删除备份，否则平台恢复点会出现“记录还在但底层备份已消失”的状态不一致。

请求样例：

```json
{
  "version": "v1",
  "messageId": "msg_backup_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "type": "backup",
    "deadline": "2026-07-01T00:30:00Z",
    "backup": {
      "planId": "plan_001",
      "sourceClusterId": "source_cluster_id",
      "sourceNamespace": "demo-mysql-csi",
      "veleroBackupName": "hcdr-plan-plan001-20260701000034",
      "scope": "namespace",
      "labelSelector": "",
      "storageRepo": "my-minio",
      "includeClusterResources": true,
      "excludeResources": [
        {
          "group": "",
          "resource": "events",
          "name": "",
          "version": "v1",
          "labels": ""
        }
      ],
      "hooks": {
        "pre": [],
        "post": []
      }
    }
  }
}
```

Velero Backup 必须携带标签：

```json
{
  "hypercdr.io/managed-by": "hypercdr",
  "hypercdr.io/plan-id": "plan_001",
  "hypercdr.io/task-id": "task_backup_001",
  "hypercdr.io/command-id": "cmd_backup_001",
  "hypercdr.io/source-cluster-id": "source_cluster_id",
  "hypercdr.io/source-namespace": "demo-mysql-csi",
  "velero.io/storage-location": "my-minio"
}
```

目标状态转换：

```text
queued -> dispatched -> accepted -> running -> succeeded
queued -> dispatched -> accepted -> running -> failed
```

成功条件：

- Velero Backup phase 为 `Completed`。
- agent 上报 `agent.task.completed` 或 `agent.velero.event` 的 `backup_completed`。
- 平台创建或更新恢复点。

恢复点生成规则：

- 恢复点归属 `sourceClusterId`。
- 唯一键是 `(sourceClusterId, veleroBackupName)`。
- 失败备份不能生成 `available` 恢复点。
- 目标集群 Backup CR 不能生成恢复点。

失败错误码建议：

- `BACKUP_COMMAND_INVALID`
- `BSL_NOT_READY`
- `BACKUP_MANIFEST_INVALID`
- `BACKUP_CONFLICT`
- `BACKUP_SUBMIT_FAILED`
- `BACKUP_STATUS_READ_FAILED`
- `BACKUP_STATUS_TIMEOUT`
- `BACKUP_FAILED`

### 11.4 `restore`

用途：从一个可用恢复点恢复应用。

执行集群：目标恢复集群。可以是源集群，也可以是其他集群。

命令体字段：

- `restorePointId`：平台恢复点 ID，必填。
- `veleroBackupName`：Velero Backup 名称，必填。
- `storageRepo`：Velero `BackupStorageLocation` 名称，必填。
- `sourceNamespace`：源 namespace，必填。
- `targetNamespace`：目标 namespace，必填。
- `targetMode`：目标模式，例如 `inPlace`、`sandbox`、`crossCluster`。
- `restoreMode`：恢复模式，例如 `full`。
- `artifactMode`：恢复内容模式。
- `conflictPolicy`：冲突策略。
- `includeClusterScoped`：是否包含集群级资源。
- `useTransforms`：是否启用转换。
- `transformPreset`：转换预设。
- `storageProfileMode`：存储映射模式。
- `alternateProfileId`：备用存储 profile。

请求样例：

```json
{
  "version": "v1",
  "messageId": "msg_restore_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "target_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_restore_001",
    "commandId": "cmd_restore_001",
    "type": "restore",
    "deadline": "2026-07-01T00:30:00Z",
    "restore": {
      "restorePointId": "rp_001",
      "veleroBackupName": "hcdr-plan-plan001-20260701000034",
      "storageRepo": "my-minio",
      "sourceNamespace": "demo-mysql-csi",
      "targetNamespace": "demo-mysql-csi",
      "targetMode": "inPlace",
      "restoreMode": "full",
      "artifactMode": "all",
      "conflictPolicy": "skip",
      "includeClusterScoped": false,
      "useTransforms": true,
      "transformPreset": "namespace",
      "storageProfileMode": "default",
      "alternateProfileId": ""
    }
  }
}
```

目标状态转换：

```text
queued -> dispatched -> accepted -> running -> succeeded
queued -> dispatched -> accepted -> running -> failed
```

成功条件：

- 目标集群 Velero 可见 `veleroBackupName`。
- Velero Restore phase 为 `Completed`。
- 如果启用 readiness 检查，目标应用必须 ready。

失败错误码建议：

- `RESTORE_COMMAND_INVALID`
- `BSL_NOT_READY`
- `RESTORE_BACKUP_NOT_FOUND`
- `RESTORE_MANIFEST_INVALID`
- `RESTORE_SUBMIT_FAILED`
- `RESTORE_STATUS_READ_FAILED`
- `RESTORE_STATUS_TIMEOUT`
- `RESTORE_FAILED`
- `RESTORE_READINESS_TIMEOUT`

### 11.5 `drill`

用途：演练恢复。协议上复用 `restore` 命令体，但 `payload.type` 为 `drill`。

执行集群：演练目标集群。

业务约束：

- `targetNamespace` 应为隔离 namespace，例如 `demo-mysql-csi-drill`。
- `targetMode` 通常为 `sandbox` 或 `crossCluster`。
- 不应覆盖生产 namespace。
- drill 任务不创建新的恢复点。

请求样例：

```json
{
  "version": "v1",
  "messageId": "msg_drill_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "target_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_drill_001",
    "commandId": "cmd_drill_001",
    "type": "drill",
    "deadline": "2026-07-01T00:30:00Z",
    "restore": {
      "restorePointId": "rp_001",
      "veleroBackupName": "hcdr-plan-plan001-20260701000034",
      "storageRepo": "my-minio",
      "sourceNamespace": "demo-mysql-csi",
      "targetNamespace": "demo-mysql-csi-drill",
      "targetMode": "sandbox",
      "restoreMode": "full",
      "artifactMode": "all",
      "conflictPolicy": "skip",
      "includeClusterScoped": false,
      "useTransforms": true,
      "transformPreset": "namespace",
      "storageProfileMode": "default",
      "alternateProfileId": ""
    }
  }
}
```

成功条件：

- Velero Restore 完成。
- 演练 namespace ready。

### 11.6 `takeover`

用途：接管恢复。协议上复用 `restore` 命令体，但 `payload.type` 为 `takeover`。

执行集群：接管目标集群。

业务约束：

- takeover 是生产接管动作，页面必须有明确确认。
- takeover 成功后，任务历史应记录目标集群、目标 namespace、使用的恢复点。
- takeover 不创建新的恢复点。

请求样例：

```json
{
  "version": "v1",
  "messageId": "msg_takeover_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "target_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_takeover_001",
    "commandId": "cmd_takeover_001",
    "type": "takeover",
    "deadline": "2026-07-01T00:30:00Z",
    "restore": {
      "restorePointId": "rp_001",
      "veleroBackupName": "hcdr-plan-plan001-20260701000034",
      "storageRepo": "my-minio",
      "sourceNamespace": "demo-mysql-csi",
      "targetNamespace": "demo-mysql-csi",
      "targetMode": "crossCluster",
      "restoreMode": "full",
      "artifactMode": "all",
      "conflictPolicy": "skip",
      "includeClusterScoped": false,
      "useTransforms": true,
      "transformPreset": "namespace",
      "storageProfileMode": "default",
      "alternateProfileId": ""
    }
  }
}
```

成功条件：

- Velero Restore 完成。
- 目标应用 ready。
- 平台任务状态为 `succeeded`。

### 11.7 `retention-cleanup`

用途：按保留策略删除过期恢复点对应的 Velero backup。

执行集群：源集群。

命令体字段：

- `planId`：保护计划 ID，必填。
- `restorePoints`：要清理的恢复点列表，必填。
- `restorePoints[].id`：平台恢复点 ID。
- `restorePoints[].veleroBackupName`：Velero Backup 名称。
- `restorePoints[].namespace`：Velero Backup 所在 namespace，默认 agent namespace。

请求样例：

```json
{
  "version": "v1",
  "messageId": "msg_retention_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_retention_001",
    "commandId": "cmd_retention_001",
    "type": "retention-cleanup",
    "deadline": "2026-07-01T00:30:00Z",
    "retentionCleanup": {
      "planId": "plan_001",
      "restorePoints": [
        {
          "id": "rp_old_001",
          "veleroBackupName": "hcdr-plan-plan001-20260630000034",
          "namespace": "hypercdr-agent"
        }
      ]
    }
  }
}
```

目标状态转换：

```text
queued -> dispatched -> accepted -> running -> succeeded
queued -> dispatched -> accepted -> running -> failed
```

成功条件：

- 删除请求提交成功。
- 如果底层支持等待删除，则 Velero Backup 已不可见。
- 平台将对应恢复点标记为 `deleted`。

失败错误码建议：

- `RETENTION_CLEANUP_COMMAND_INVALID`
- `RETENTION_DELETE_MANIFEST_INVALID`
- `RETENTION_DELETE_SUBMIT_FAILED`
- `RETENTION_DELETE_TIMEOUT`
- `RETENTION_DELETE_FAILED`

### 11.8 `unregister`

用途：注销集群，清理集群侧 agent/Velero 资源。

执行集群：要注销的集群。

命令体字段：

- `clusterId`：要注销的集群 ID，必填。
- `namespace`：agent namespace，必填。
- `deleteVelero`：是否删除 Velero 相关资源。
- `deleteNamespace`：是否删除 agent namespace。
- `reason`：注销原因。

请求样例：

```json
{
  "version": "v1",
  "messageId": "msg_unregister_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "cluster_to_unregister",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_unregister_001",
    "commandId": "cmd_unregister_001",
    "type": "unregister",
    "deadline": "2026-07-01T00:10:00Z",
    "unregister": {
      "clusterId": "cluster_to_unregister",
      "namespace": "hypercdr-agent",
      "deleteVelero": true,
      "deleteNamespace": false,
      "reason": "user requested unregister"
    }
  }
}
```

目标状态转换：

```text
queued -> dispatched -> accepted -> running -> succeeded
queued -> dispatched -> accepted -> running -> failed
```

成功条件：

- agent 完成集群侧 cleanup。
- 平台收到 `agent.task.completed`。
- 平台完成集群注销状态更新。

失败错误码建议：

- `UNREGISTER_COMMAND_INVALID`
- `UNINSTALLER_NOT_CONFIGURED`
- `UNREGISTER_CLEANUP_FAILED`

## 12. 幂等与乱序处理

平台和 agent 必须容忍重复消息与乱序消息。

规则：

- 终态优先于更早的非终态消息。
- 进度不能倒退。
- `backup_completed` 可以更新已有恢复点的 size/metadata。
- 恢复点按 `(sourceClusterId, veleroBackupName)` upsert。
- 重放 Velero 事件不能创建重复 task event。

### 12.1 发送失败、超时与重发

平台下发任务后，如果没有收到 `agent.task.accepted`，不能无限等待，也不能直接创建新的业务任务。必须按同一个 `taskId + commandId` 重试。

固定超时：

- WebSocket 写入失败：立即认为本次发送失败。
- WebSocket 写入成功但未收到 `agent.task.accepted`：等待 `8s`。
- 最大重发次数：`3` 次。
- 重发间隔：`2s -> 5s -> 10s`。
- 总失败窗口：约 `50s`。
- 超过 3 次重发仍无响应：平台把任务标记为 `failed`，错误码为 `AGENT_ACCEPT_TIMEOUT`。
- 如果发送时 agent 已断线，不向空连接重发；任务保留在可恢复状态，等待 agent 重连后按同一个 `commandId` 重新下发。如果超过任务 `deadline`，标记为 `failed`，错误码为 `AGENT_DISCONNECTED` 或 `TASK_DEADLINE_EXCEEDED`。

平台重发规则：

- 重发必须使用同一个 `taskId`。
- 重发必须使用同一个 `commandId`。
- 重发必须使用同一个 `messageId`，因为 `messageId` 表示逻辑消息 ID。
- 平台应缓存原始请求 JSON，重发时直接重新投递原始消息。
- 如需记录第几次投递，应写入本地发送日志，不应修改协议消息体。
- 重发不能生成新的 task。
- 重发不能改变 payload 中的业务字段，例如 `veleroBackupName`、`sourceNamespace`、`restorePointId`。

agent 去重规则：

- agent 必须以 `commandId` 作为命令幂等键。
- 如果第一次收到命令但还没有开始执行，返回 `agent.task.accepted`，然后执行。
- 如果重复收到相同 `commandId`，不能重复创建 Velero Backup/Restore/DeleteBackupRequest。
- 如果该任务正在执行，返回当前状态：
  - 尚未上报 accepted：重新返回 `agent.task.accepted`。
  - 已经 running：返回 `agent.task.accepted`，并继续按正常节奏上报 `agent.task.progress` event。
  - 已经 succeeded：返回 `agent.task.accepted`，并通过可靠事件补发 `agent.task.completed`。
  - 已经 failed：返回 `agent.task.accepted`，并通过可靠事件补发 `agent.task.failed`。
- 如果同一个 `commandId` 携带了不同 payload，agent 必须返回 `agent.task.failed`，错误码为 `COMMAND_PAYLOAD_CONFLICT`，并拒绝执行。

重连恢复规则：

- 平台重连后可以重新下发仍处于 `queued/dispatched/accepted/running` 的任务。
- agent 重连后必须上报本地正在执行或最近完成的任务状态。
- 平台收到重复的终态消息必须幂等处理，不能重复创建恢复点或 task event。

迟到响应规则：

- 平台把任务标记为 `failed(AGENT_ACCEPT_TIMEOUT)` 后，仍可能收到同一个 `taskId + commandId` 的迟到 `accepted/progress/completed`。
- 如果迟到消息能证明 agent 实际已经执行了同一个命令，平台允许纠正任务状态。
- 迟到 `accepted`：如果没有 Velero 执行证据，不能单独把任务改回 running；可以记录事件并等待后续 progress/event。
- 迟到 `progress`：可把 `failed(AGENT_ACCEPT_TIMEOUT)` 纠正为 `running`，并记录 `late_response_recovered` 事件。
- 迟到 `completed`：可把 `failed(AGENT_ACCEPT_TIMEOUT)` 纠正为 `succeeded`，并按幂等规则创建或更新恢复点。
- 迟到 `failed`：保持或更新失败信息，不能创建恢复点。
- 如果任务失败原因不是 `AGENT_ACCEPT_TIMEOUT/AGENT_DISCONNECTED/TASK_DEADLINE_EXCEEDED`，迟到消息默认不能覆盖人工取消、payload 冲突、权限失败等明确失败。

状态转换：

```text
queued -> dispatched -> accepted -> running -> succeeded
queued -> dispatched -> accepted -> running -> failed
queued -> dispatched -> failed(AGENT_ACCEPT_TIMEOUT)
queued -> dispatched -> failed(AGENT_DISCONNECTED)
failed(AGENT_ACCEPT_TIMEOUT) -> running    # 仅限同 commandId 迟到 progress 纠正
failed(AGENT_ACCEPT_TIMEOUT) -> succeeded  # 仅限同 commandId 迟到 completed 纠正
```

示例：第一次下发未收到 accepted，平台重发同一个 command。

第一次请求：

```json
{
  "version": "v1",
  "messageId": "msg_dispatch_backup_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "type": "backup",
    "deadline": "2026-07-01T00:30:00Z",
    "backup": {
      "planId": "plan_001",
      "sourceClusterId": "source_cluster_id",
      "sourceNamespace": "demo-mysql",
      "veleroBackupName": "hcdr-plan-plan001-20260701000034",
      "storageRepo": "my-minio"
    }
  }
}
```

重发请求：

```json
{
  "version": "v1",
  "messageId": "msg_dispatch_backup_001",
  "messageKind": "request",
  "type": "platform.task.dispatch",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:00Z",
  "payload": {
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "type": "backup",
    "deadline": "2026-07-01T00:30:00Z",
    "backup": {
      "planId": "plan_001",
      "sourceClusterId": "source_cluster_id",
      "sourceNamespace": "demo-mysql",
      "veleroBackupName": "hcdr-plan-plan001-20260701000034",
      "storageRepo": "my-minio"
    }
  }
}
```

agent 对重复请求的响应：

```json
{
  "version": "v1",
  "messageId": "msg_task_accepted_retry_002",
  "messageKind": "response",
  "type": "agent.task.accepted",
  "tenantId": "tenant_id",
  "clusterId": "source_cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:10Z",
  "payload": {
    "ackMessageId": "msg_dispatch_backup_001",
    "ackType": "platform.task.dispatch",
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "acceptedAt": "2026-07-01T00:00:01Z"
  }
}
```

### 12.2 可靠事件重发

`payload.ackRequired=true` 的 event 必须可靠送达，典型包括：

- `agent.task.completed`
- `agent.task.failed`
- `agent.velero.event` 中的 `backup_completed`
- `agent.velero.event` 中的 `backup_failed`
- `agent.velero.event` 中的 `restore_completed`
- `agent.velero.event` 中的 `restore_failed`
- 其他会改变任务终态、恢复点状态、保留清理结果的事件。

不需要可靠重发的 event：

- `agent.heartbeat`
- `agent.inventory.report` 变化推送或 5min 兜底事件。
- `agent.task.progress`
- `agent.velero.event` 中的 progress 类事件。

重发规则：

- `payload.ackRequired=false`：agent 不等待响应，不重发；下一条状态可以覆盖上一条状态。
- `payload.ackRequired=true`：agent 必须等待 `platform.event.ack` 或 `platform.event.error`。
- 发送后 `8s` 没有收到响应，agent 必须重发同一条缓存 event。
- 前 3 次重发间隔为 `2s -> 5s -> 10s`。
- 之后每 `30s` 重发一次，不限制次数。
- agent 重启或 WebSocket 重连后，必须立即补发本地 outbox 中未确认的可靠事件。
- 重发停止条件只有两个：
  - 收到 `platform.event.ack`。
  - 收到 `platform.event.error` 且 `retryable=false`。
- 如果收到 `platform.event.error` 且 `retryable=true`，agent 必须继续重发。

outbox 规则：

- `payload.ackRequired=true` 的 event 在发送前必须写入 agent 本地持久化 outbox。
- outbox 不能只存在内存中；必须落盘，确保 agent 进程重启后仍可恢复。
- 推荐使用 SQLite；简单场景可以使用本地 JSON 文件，但必须保证写入原子性。
- agent 启动后必须先加载 outbox。
- agent 完成 `agent.register` 并收到 `platform.register.accepted` 后，必须立即补发 outbox 中未确认的可靠事件。
- WebSocket 断线重连并重新注册成功后，也必须立即补发 outbox 中未确认的可靠事件。
- 收到 `platform.event.ack` 后才能从 outbox 删除。
- 收到 `platform.event.error retryable=false` 后可以从 outbox 删除，但必须保留错误日志。
- outbox 至少要持久化 `messageId`、`messageKind`、`payload.ackRequired`、`type`、`tenantId`、`clusterId`、`agentId`、`timestamp`、完整 `payload`、首次发送时间、最近发送时间、发送次数、最近错误。
- 没有持久化 outbox，就不能声称支持终态事件可靠送达。
- 该机制用于避免平台没有收到最终成功/失败事件时，页面长期停留在 pending/running 状态。

平台幂等规则：

- 平台收到重复可靠事件时，必须幂等处理。
- 消息层可按 `messageId` 去重。
- 业务层必须按 `taskId + commandId + type` 去重。
- 定时备份 event 驱动创建 task 时，必须按 `sourceClusterId + planId + veleroBackupName` 去重。
- 手动备份同时收到 task 终态和 Velero 终态时，必须按同一 `taskId + commandId + veleroBackupName` 合并为一个业务终态。
- 恢复点必须按 `(sourceClusterId, veleroBackupName)` upsert。
- 已处理过的可靠事件再次到达时，平台应直接返回 `platform.event.ack`。

### 12.3 Agent 任务归属、Ledger 与重启恢复

仅靠监听 Velero Backup/Restore CR 不足以判断一个事件是否属于当前平台任务。平台下发任务必须写入 agent 本地任务账本，用来约束“哪些 Velero 对象是本 agent 接收到的平台任务产生的”。定时备份由 Velero Schedule 自动产生 Backup CR，不走平台提前 dispatch，但也必须通过 HyperCDR 管理标签和源集群校验后才能上报。

核心原则：

- agent 只执行和上报自己被平台明确下发的任务。
- agent 不能把任意 Velero Backup CR 当作平台恢复点来源。
- 任务归属必须由平台下发任务、本地 ledger、Velero 对象标签/注解共同确认。
- 定时备份归属必须由受管 Schedule、Velero Backup 标签/注解、源集群和 plan 共同确认。
- 目标集群 agent 即使看到了同名 Backup CR，也不能上报为源集群恢复点。
- agent 重启后必须能从本地 ledger 恢复未完成任务的观测和终态补发。

接收任务前的归属校验：

- agent 收到 `platform.task.dispatch` 后，必须先校验 envelope 中的 `clusterId` 是否等于自身注册成功的 `clusterId`。
- 如果 `clusterId` 不匹配，agent 必须拒绝任务，返回 `agent.task.failed` response，错误码为 `TASK_CLUSTER_MISMATCH`，不能创建 Velero 资源。
- 备份任务必须校验 `payload.backup.sourceClusterId` 等于 agent 注册成功后的 `clusterId`。
- 恢复、演练、接管任务必须校验该任务的执行集群等于 agent 自身注册集群。
- 清理任务必须校验要清理的恢复点来源和执行目标符合任务定义，不能在目标集群清理源集群恢复点记录。
- `taskId`、`commandId`、任务类型、执行集群、源集群、Velero 资源名称缺任一关键字段时，必须拒绝任务。

接收任务后的持久化顺序：

1. 校验任务归属和 payload。
2. 将任务写入本地 durable ledger。
3. 返回 `agent.task.accepted`。
4. 创建或更新 Velero Backup/Restore CR。
5. 监听本任务对应的 Velero 对象并上报进度。
6. 生成终态事件前，先写入 reliable outbox。
7. 发送 `agent.task.completed` 或 `agent.task.failed`。

agent 不能在 ledger 写入成功前返回 `agent.task.accepted`。否则 agent 进程在 accepted 后立即重启时，平台会认为任务已接收，但 agent 无法恢复任务，页面容易长期 pending。

ledger 推荐字段：

```json
{
  "taskId": "task_backup_001",
  "commandId": "cmd_backup_001",
  "taskType": "backup",
  "executionClusterId": "source_cluster_id",
  "sourceClusterId": "source_cluster_id",
  "targetClusterId": "target_cluster_id",
  "planId": "plan_001",
  "sourceNamespace": "demo-mysql",
  "restorePointId": "rp_001",
  "veleroBackupName": "hcdr-plan-plan001-20260701000034",
  "veleroRestoreName": "",
  "status": "running",
  "acceptedAt": "2026-07-01T00:00:01Z",
  "startedAt": "2026-07-01T00:00:02Z",
  "completedAt": null,
  "lastProgress": {
    "progress": 70,
    "totalBytes": 10737418240,
    "syncedBytes": 7516192768,
    "percent": 70.0
  },
  "lastObservedResourceVersion": "123456",
  "payloadHash": "sha256:payload_hash"
}
```

ledger 持久化规则：

- ledger 必须落盘，不能只存在内存中。
- 推荐和 outbox 使用同一个 SQLite 数据库。
- `commandId` 必须唯一；同一个 `commandId` 重复下发时，agent 必须返回已有接收结果，不能重复创建 Velero 资源。
- `taskId + commandId + taskType` 是任务业务幂等键。
- ledger 中 `executionClusterId` 必须等于 agent 当前注册集群。
- 任务到达终态后，ledger 记录不能立即删除，至少保留到平台 ack 终态事件之后，并建议保留一段审计时间。

Velero 对象归属判断：

- 平台下发任务产生的 Velero Backup/Restore，agent 只处理 ledger 中存在的 `veleroBackupName` 或 `veleroRestoreName`。
- agent 监听到不在 ledger 中的 Velero Restore CR，默认只作为普通 inventory 信息，不得上报任务进度或终态。
- agent 监听到不在 ledger 中的 Velero Backup CR 时，只有满足“HyperCDR 受管定时备份”条件，才能作为 `agent.velero.event` 上报；否则只能作为普通 inventory 信息。
- 平台下发任务时，agent 创建的 Velero CR 必须带上可校验的 label/annotation，例如：
  - `hypercdr.io/task-id`
  - `hypercdr.io/command-id`
  - `hypercdr.io/plan-id`
  - `hypercdr.io/source-cluster-id`
  - `hypercdr.io/execution-cluster-id`
  - `hypercdr.io/source-namespace`
- agent 观测 Velero CR 时，应同时校验名称、ledger、label/annotation；任一关键字段冲突时，必须忽略该 CR 并记录告警。
- 目标集群同步过来的同名 Backup CR 如果不在本地 ledger 中，或 `sourceClusterId/executionClusterId` 不匹配，必须忽略，不能生成恢复点、不能上报备份终态。

HyperCDR 受管定时备份条件：

- Backup CR 必须带 `hypercdr.io/managed-by=hypercdr`。
- Backup CR 必须带 `hypercdr.io/plan-id`。
- Backup CR 必须带 `hypercdr.io/source-cluster-id`，且等于 agent 当前注册集群。
- Backup CR 必须带 `hypercdr.io/source-namespace` 或可从 included namespaces 唯一推导。
- Backup CR 必须能关联到 HyperCDR 创建的 Velero Schedule，例如通过 schedule 名称或 `velero.io/schedule-name`。
- 不满足以上条件的 Backup CR 不能创建平台 task，不能创建恢复点。

重启恢复规则：

1. agent 启动后先加载 ledger 和 reliable outbox。
2. agent 重新发送 `agent.register`。
3. 收到 `platform.register.accepted` 后，立即补发 outbox 中未 ack 的可靠事件。
4. 对 ledger 中仍处于 `accepted/running/syncing` 的任务，重新 watch 对应 Velero Backup/Restore CR。
5. 如果 Velero CR 已经是终态，但 outbox 中没有对应终态事件，agent 必须生成并持久化新的终态事件。
6. 如果 Velero CR 不存在，agent 不能直接标记成功；应按任务类型进入恢复判断：
   - 对刚 accepted 但尚未创建 CR 的任务，可以重新创建 CR。
   - 对已创建过 CR 但重启后找不到 CR 的任务，应上报 `agent.task.failed`，错误码为 `VELERO_OBJECT_MISSING`，除非能通过平台任务重试重新下发。
7. 对已经终态且已收到平台 ack 的任务，agent 不再主动补发终态，但仍可保留 ledger 审计记录。

平台侧二次校验：

- 平台不能只相信 agent 上报内容；必须结合连接身份、任务表、计划表、恢复点表二次校验。
- WebSocket 认证后的连接集群必须等于 envelope `clusterId`。
- `agent.task.progress/completed/failed` 的 `clusterId` 必须等于任务表中的执行集群。
- 平台下发任务的 `taskId + commandId` 必须能匹配平台已有任务。
- 定时备份 `agent.velero.event` 首次到达时允许没有 `taskId + commandId`，平台必须按 `sourceClusterId + planId + veleroBackupName` 幂等创建或查找 backup task。
- 备份任务的终态事件只有在 `event.clusterId == protectionPlan.sourceClusterId` 时，才能创建或更新恢复点。
- 恢复、演练、接管任务的终态事件必须来自任务指定的执行目标集群。
- 如果 agent 上报的 `sourceClusterId`、`planId`、`veleroBackupName` 与平台任务事实不一致，平台必须返回 `platform.event.error retryable=false`。
- 平台收到来自目标集群的 Backup completed 事件时，即使 `veleroBackupName` 与源集群相同，也必须拒绝创建恢复点。

建议错误码：

| 错误码 | 发送方 | 场景 | retryable |
| --- | --- | --- | --- |
| `TASK_CLUSTER_MISMATCH` | agent / 平台 | envelope 集群或任务执行集群不匹配 | false |
| `TASK_PAYLOAD_MISMATCH` | agent / 平台 | payload 与已记录任务事实冲突 | false |
| `TASK_NOT_FOUND` | 平台 | 终态事件无法匹配平台任务 | false |
| `NOT_SOURCE_CLUSTER_BACKUP` | 平台 | 非源集群上报备份终态并试图生成恢复点 | false |
| `VELERO_OBJECT_MISSING` | agent | ledger 记录的 Velero 对象丢失且无法恢复 | false |
| `LOCAL_LEDGER_WRITE_FAILED` | agent | agent 接收任务前 ledger 写入失败 | true |

这套规则解决两个问题：

- 避免目标集群 agent 因看到同步过来的同名 Backup CR 而上传错误进度、错误终态、错误恢复点。
- 避免 agent accepted 后重启导致任务无人继续观测，平台页面长期停在 pending/running。

## 13. 硬性不变量

- 恢复点只属于源集群。
- `protection_plans.source_cluster_id` 是恢复点归属的权威来源。
- 目标集群 Backup CR 必须被忽略，不能创建恢复点，即使名称与源集群 Backup CR 完全相同。
- agent 只能上报本地 ledger 中存在且归属校验通过的任务进度和终态；受管定时备份 Backup CR 例外，但必须满足 sourceClusterId、planId、scheduleName、backupName 标签校验。
- agent 不能在本地 ledger 写入成功前返回 `agent.task.accepted`。
- `payload.ackRequired=true` 的终态事件必须先写入本地 outbox，再发送给平台。
- agent 重启后必须先恢复 ledger/outbox，再恢复任务观测和可靠事件补发。
- 平台必须对 agent 的进度和终态事件做二次校验，不能只相信 payload。
- 备份终态事件只有来自计划的源集群时，才能创建或更新恢复点。
- 恢复、演练、接管终态事件必须来自任务指定的执行目标集群。
- 备份成功基于 Velero Backup 终态 `Completed`，不是 UI 进度到 100。
- `progress=100` 不足以代表任务成功。
- 失败备份不能生成 `available` 恢复点。
- 新流程中失败备份只进入 task history，不创建新的 restore_point；`restore_points.failed` 仅用于历史兼容或补偿异常。
- UI active/running 状态必须来自 task status，不能只看 progress。
- `veleroBackupName` 是任务历史和恢复点之间最主要的用户可理解关联字段。

## 14. 页面与平台 API 映射

本章定义页面需要的数据从哪里来、是否需要 agent 通信、刷新策略是什么。页面不应直接依赖 agent 实时响应；页面默认读取平台数据库和缓存，agent 通信只负责持续更新平台状态。

### 14.1 总体页面数据原则

- 页面默认读取平台 API 返回的数据，不直接连接 agent。
- agent 的 `heartbeat`、`inventory`、task event、Velero event 写入平台 store 后，再由平台 API 提供给页面。
- 后台 event 只能局部更新状态，不能重置用户分页、筛选、搜索、排序、勾选项。
- 用户点击刷新时，平台可以触发按需 agent request，但页面仍应通过平台 API 获取结果。
- 页面打开时可以拉取最新平台缓存；是否触发 agent 按需刷新由平台根据缓存时间、用户动作和页面需求决定。
- 长耗时任务状态通过 task event 局部更新，不应定时刷新整张表。
- 平台 API 的过滤条件必须与业务归属字段一致，例如恢复点集群过滤必须使用 `sourceClusterId`。

页面与数据源总表：

| 页面 | 主要数据 | 平台数据源 | Agent 通信 | 刷新策略 |
| --- | --- | --- | --- | --- |
| 集群注册/列表 | cluster、agent、Velero 健康 | clusters、agent sessions、latest inventory summary | `agent.register`、`agent.heartbeat`、`agent.inventory.report` | heartbeat 局部更新，inventory 变化/5min 兜底 |
| 集群注销 | unregister task、cluster 状态 | clusters、tasks | `platform.task.dispatch unregister` | task event 局部更新 |
| DR 阶段一 | namespace summary、resource summary、draft plan | inventory summary cache、protection plans | inventory summary、namespace detail 按需 | 默认缓存，资源弹窗按需 |
| DR 阶段二 | storage repo、schedule、DR Config 状态 | storage repositories、plans、tasks | `storage-sync`、`schedule-sync` | 用户保存触发，task event 更新 |
| DR 阶段三 | sync status、latest restore point、resource summary | tasks、restore_points、inventory summary | backup task、Velero event、inventory | active task 优先，局部更新 |
| Restore Points | available restore points、size、namespace filter | restore_points、plans | 无直接 agent 请求 | 用户刷新/过滤，必要时平台补偿校验 |
| Backup and Restore Tasks | task history、错误、恢复点关系 | tasks、task events、restore_points | task event | 运行中状态局部更新 |
| Resource 弹窗 | 单 namespace 资源详情 | inventory detail cache | `platform.inventory.request scope=namespaceResources` | 打开弹窗按需 |

### 14.2 集群注册与集群列表

页面数据：

- 集群名称、fingerprint、kubeVersion。
- agent 在线状态、agentVersion、lastHeartbeatAt。
- Velero 安装状态和健康状态。
- nodeCount、namespaceCount、applicationCount。
- 最近 inventory summary 时间。

数据来源：

- 注册和重连来自 `agent.register`。
- 在线状态来自 `agent.heartbeat`。
- 资源数量来自 `agent.inventory.report scope=summary`，不是 heartbeat。

状态规则：

- `online`：最近 heartbeat 在健康窗口内。
- `degraded`：heartbeat 延迟，但未超过离线阈值。
- `disconnected`：超过离线阈值仍无 heartbeat。

建议默认阈值：

```text
lastHeartbeatAt <= 90s: online
90s < lastHeartbeatAt <= 5min: degraded
lastHeartbeatAt > 5min: disconnected
```

注册相关平台 API 负责：

- 创建 install token。
- 展示 token 过期时间。
- 撤销 install token。
- 展示注册失败原因。

agent 协议负责：

- `agent.register` 首次注册、重启、重连。
- `platform.register.accepted/rejected` 返回注册结果。
- `agent.heartbeat` 维持在线状态。

### 14.3 集群注销

注销分两类：

- `soft unregister`：平台侧禁用或移除集群记录，不要求 agent 在线。
- `agent cleanup unregister`：agent 在线时，下发 `unregister` task 清理集群侧 agent/Velero 资源。

页面动作：

- 用户点击注销。
- 页面展示确认项：是否删除 Velero、是否删除 agent namespace、是否保留历史数据。
- 平台创建 unregister task 或执行 soft unregister。

Agent 通信：

- 在线清理使用 `platform.task.dispatch type=unregister`。
- agent 返回 `accepted`、`completed/failed`。

状态处理：

- agent 在线：优先执行 cleanup unregister。
- agent 离线：允许 soft unregister，但必须提示集群侧资源不会被自动清理。
- 注销不应删除历史 task 和 restore point，除非用户明确选择清理历史数据。
- 注销后关联 protection plan 应进入 `disabled` 或不可执行状态，不能继续触发 schedule-sync、backup、restore。

### 14.4 DR 阶段一：保护对象选择

页面数据：

- 源集群列表。
- namespace 列表。
- 每个 namespace 的 resource summary。
- 每个 namespace 的 PVC summary。
- 资源详情弹窗所需的 namespace resources。
- Protection Plan draft 或已有配置。

数据来源：

- 列表资源摘要来自平台缓存的 `agent.inventory.report scope=summary`。
- 资源详情弹窗来自 `platform.inventory.request scope=namespaceResources`。
- plan draft 来自平台 protection plans store。

页面规则：

- 阶段一默认展示平台缓存 summary。
- 如果 summary 缺失或过期，平台可以触发 `platform.inventory.request scope=summary`。
- 用户点击 Resource 弹窗时，请求单个 namespace detail。
- ConfigMap/Secret 不展示 value，只展示名称、类型、key 数量和引用关系。
- 保存选择时，只创建或更新 Protection Plan draft，不触发 backup。

### 14.5 DR 阶段二：存储、策略与 DR Config

页面数据：

- Storage Repository 列表和状态。
- Protection Plan 配置。
- Schedule policy，例如 cron、保留策略。
- storage-sync task 状态。
- schedule-sync task 状态。
- DR Config 状态。

平台 API 负责：

- Storage Repository CRUD。
- Storage Repository 连接测试。
- Protection Plan 更新。
- Schedule policy 更新。
- Retention policy 更新。

Agent 通信：

- `storage-sync`：把对象存储配置同步到需要使用该 repo 的集群。
- `schedule-sync`：在源集群创建或更新 Velero Schedule。

DR Config 状态建议：

```text
active = source storage-sync succeeded + schedule-sync succeeded + plan config valid
active_with_warning = active + target storage-sync failed/degraded
source_storage_failed = source storage-sync failed
target_storage_warning = target storage-sync failed/degraded, but source backup schedule is ready
schedule_failed = schedule-sync failed
configuring = source storage-sync 或 schedule-sync 正在执行
disabled = plan 被禁用或源集群不可执行
```

状态判定规则：

- 源集群 `storage-sync` 失败时，`schedule-sync` 不应继续下发；如果已存在旧 schedule，平台应把 DR Config 标记为 `source_storage_failed`，并提示源集群备份链路不可用。
- 源集群 `storage-sync` 成功且 `schedule-sync` 成功时，DR Config 可以显示 `Ready/active`。
- 目标集群 `storage-sync` 失败时，不改变源集群 schedule 的下发和运行，不阻塞 DR Config 显示 `Ready/active`。
- 目标集群 `storage-sync` 失败时，DR Config 应显示 `Ready with warning` 或等价 UI。warning 内容至少包括 `targetClusterId`、`targetClusterName`、`storageLocation`、`errorCode`、`message`、`impact`。
- `impact` 建议取值：`restore_unavailable`、`drill_unavailable`、`takeover_unavailable`，可以同时出现。
- 如果目标集群后续 `storage-sync` 成功，平台应清除该 warning。
- 如果目标集群暂未配置或用户尚未选择目标集群，不能把目标 BSL 状态算作失败。
- 目标集群 storage-sync 状态必须单独记录，不能混入源集群 storage-sync 或 schedule-sync 结果。

### 14.6 DR 阶段三：同步、恢复与 Resource 列

页面数据：

- 每个 namespace 的 DR Config。
- 当前 active backup task。
- 最新 available restore point。
- sync status 和 progress。
- resource summary。
- More 菜单动作：View Restore Point、Restore、Drill、Takeover 等。

数据来源：

- Resource 列来自 inventory summary，不拉取完整 detail。
- Sync 运行态来自 tasks。
- 最新恢复点来自 restore_points。
- 定时备份状态来自平台根据 `agent.velero.event` 创建或更新的 backup task。

Sync 状态优先级：

1. 存在 active backup task：显示 running/syncing/progress。
2. 最新 backup task succeeded：显示 Sync complete，并关联最新恢复点。
3. 最新 backup task failed：显示 Sync failed 和错误信息。
4. 没有 active task 但存在 available restore point：显示 Last snapshot。
5. 无恢复点：显示 No snapshot yet。

最新恢复点选择规则：

```text
latestRestorePoint =
  max(completedAt)
  where sourceClusterId = plan.sourceClusterId
    and sourceNamespace = selected namespace
    and status = available
```

View Restore Point 跳转规则：

- 单选 namespace：跳转 Restore Points 页面，并设置 namespace 多选过滤为该 namespace。
- 多选 namespace：跳转 Restore Points 页面，并设置 namespace 多选过滤为所有选中 namespace。
- 页面不显示额外提示条；过滤器本身表达当前条件。

Resource 弹窗规则：

- 列表 Resource 列只展示图标 + 数字 summary。
- 点击任一资源图标打开同一个 namespace resource detail 弹窗。
- 弹窗打开时，如果 detail cache 缺失或过期，平台触发 `platform.inventory.request scope=namespaceResources`。

### 14.7 Restore Points 页面

页面数据：

- `available` restore points。
- namespace 多选过滤。
- source cluster 过滤。
- size 和 sizeStatus。
- restore/drill/takeover 入口。

数据来源：

- restore_points 表。
- protection_plans 表。
- task history 用于展示来源任务和错误关系。

过滤规则：

- 集群过滤使用 `sourceClusterId`。
- namespace 过滤使用 `sourceNamespace` 或 included namespaces。
- 默认只展示 `status=available`。
- failed、deleted、clearing 不进入默认列表。

Size 展示规则：

- 主字段显示 `totalBytes`。
- tooltip 展示 `metadataBytes`、`volumeBytes`、`uploadedBytes`、`uploadedVolumeBytes` 及 accuracy。
- `sizeStatus=collecting/partial` 时，页面必须显示 size 仍在采集或部分准确。
- `uploadedVolumeBytes` 第一版是估算值，tooltip 必须标明 estimated。

从阶段三跳转：

- namespace 条件写入页面过滤器。
- 多选 namespace 使用多选过滤器，不使用提示条模拟过滤。

### 14.8 Backup and Restore Tasks 页面

页面数据：

- task type。
- task status：running/succeeded/failed。
- source namespace、target namespace。
- restore point display。
- repository/storageLocation。
- startedAt、completedAt、duration。
- errorCode、message。
- size 和 sizeStatus。

默认时间窗口：

- 默认固定为最近 `7d`。
- 页面必须允许用户切换时间范围。
- 不默认查询所有历史任务。

列表字段建议：

- Task Type。
- Namespace。
- Operation Time。
- Restore Point。
- Repository。
- Duration。
- Result/Error。

运行中更新规则：

- 页面打开时查询当前筛选范围的数据。
- 后台 task event 只更新当前可见 task 的状态、进度、错误和完成时间。
- 不自动重置分页、筛选、排序。
- 用户点击刷新时重新查询列表。

恢复点关系：

- 成功 backup task 关联 restore point。
- failed backup task 不要求存在 restore point。
- restore point 已被清理时，任务历史显示已清除/已删除，不隐藏任务。

### 14.9 Resource Detail 弹窗

请求：

```json
{
  "requestId": "req_ns_detail_001",
  "scope": "namespaceResources",
  "namespace": "demo-mysql",
  "includeDetails": true,
  "reason": "resource_modal_open"
}
```

返回分组：

- `workloads`：Deployment、StatefulSet、DaemonSet、Pod 摘要。
- `network`：Service、Ingress、EndpointSlice 计数。
- `storage`：PVC、PV 引用、StorageClass、requestedBytes、bound status。
- `config`：ConfigMap、Secret 名称、类型、key 数量。
- `rbac`：ServiceAccount、Role、RoleBinding。
- `policy`：NetworkPolicy、PDB、HPA。
- `jobs`：Job、CronJob。

PVC 字段建议：

- name。
- status。
- storageClass。
- requestedBytes。
- capacityBytes。
- volumeName。
- accessModes。

敏感信息规则：

- Secret value 禁止上传。
- ConfigMap value 默认禁止上传。
- Pod logs 禁止上传。
- Events 全量禁止上传。
- Endpoint 地址明细默认禁止上传。

缓存规则：

- namespace detail cache 建议 `5min`。
- cache 未过期时弹窗直接展示。
- cache 过期或缺失时触发按需 request。
- request 失败时可以展示旧 cache，并显示数据采集失败和采集时间。

### 14.10 页面级平台 API 契约

本节不限定最终 HTTP 路由命名，但限定页面与平台之间必须具备的 API 能力。前端只能调用平台 API；平台再决定是否读取数据库缓存、触发 agent 请求或等待后台 event。

#### 14.10.1 集群注册与注销 API

集群注册页面至少需要：

- 创建 install token。
- 查询 install token 状态和过期时间。
- 撤销 install token。
- 查询已注册集群列表。
- 查询单集群详情。

集群列表 API 返回字段建议：

```json
{
  "items": [
    {
      "id": "cluster_001",
      "name": "prod-cluster-01",
      "fingerprint": "cluster_fingerprint",
      "status": "online",
      "connectionStatus": "connected",
      "kubeVersion": "v1.29.4",
      "agentVersion": "0.1.0",
      "veleroStatus": "healthy",
      "nodeCount": 3,
      "namespaceCount": 42,
      "applicationCount": 28,
      "lastHeartbeatAt": "2026-07-01T00:01:00Z",
      "lastInventoryAt": "2026-07-01T00:00:30Z"
    }
  ]
}
```

集群注销页面至少需要：

- 查询注销影响范围，例如关联 plan、active task、restore point 数量。
- 创建 unregister task。
- 查询 unregister task 状态。
- 离线集群 soft unregister。

注销 API 规则：

- 在线清理必须创建 `unregister` task，并通过 `platform.task.dispatch` 下发 agent。
- 离线 soft unregister 不能假装集群侧资源已清理。
- 注销操作必须返回一个可追踪的 operation/task id，页面不能只依赖同步 HTTP 返回值判断最终成功。

#### 14.10.2 DR 阶段一 API

阶段一页面至少需要：

- 查询源集群列表。
- 查询某源集群 namespace summary。
- 查询或创建 plan draft。
- 保存保护对象选择。
- 打开 Resource 弹窗时查询 namespace detail。

阶段一 namespace summary API 返回字段建议：

```json
{
  "clusterId": "source_cluster_id",
  "inventoryAt": "2026-07-01T00:10:02Z",
  "items": [
    {
      "namespace": "demo-mysql",
      "status": "Active",
      "summaryHash": "sha256:namespace_summary_hash",
      "resourceSummary": {
        "deployments": 1,
        "statefulSets": 1,
        "daemonSets": 0,
        "services": 3,
        "ingresses": 1,
        "pvcs": 2,
        "configMaps": 5,
        "secrets": 4,
        "jobs": 0,
        "cronJobs": 0
      },
      "pvcSummary": {
        "count": 2,
        "requestedBytes": 21474836480,
        "boundCount": 2,
        "pendingCount": 0
      },
      "plan": {
        "id": "plan_001",
        "status": "draft"
      }
    }
  ]
}
```

规则：

- `status=Active` 是 Kubernetes namespace 状态，可在阶段一作为参考；但不应在阶段二/三表格中占用关键列。
- Resource 列摘要必须来自 `resourceSummary/pvcSummary`，不触发逐 namespace detail 拉取。
- 保存保护对象只影响 plan draft，不应创建 Velero Backup 或 Schedule。

#### 14.10.3 DR 阶段二 API

阶段二页面至少需要：

- 查询 plan 配置详情。
- 查询 Storage Repository 列表。
- 创建、更新、删除 Storage Repository。
- 测试 Storage Repository 连接。
- 保存 schedule policy。
- 保存 retention policy。
- 下发或重新下发 `storage-sync`。
- 下发或重新下发 `schedule-sync`。
- 查询 DR Config 聚合状态。

DR Config API 返回字段建议：

```json
{
  "planId": "plan_001",
  "sourceClusterId": "source_cluster_id",
  "sourceNamespace": "demo-mysql",
  "status": "active",
  "warningLevel": "warning",
  "warnings": [
    {
      "code": "TARGET_BSL_UNAVAILABLE",
      "targetClusterId": "target_cluster_id",
      "targetClusterName": "cluster-158",
      "storageLocation": "my-minio",
      "message": "BackupStorageLocation my-minio is unavailable on target cluster.",
      "impact": ["restore_unavailable", "drill_unavailable", "takeover_unavailable"],
      "lastCheckedAt": "2026-07-01T00:05:20Z"
    }
  ],
  "storage": {
    "repositoryId": "repo_001",
    "name": "my-minio",
    "sourceSyncStatus": "succeeded",
    "targetSyncStatus": "failed",
    "targetSyncWarning": true,
    "lastSyncedAt": "2026-07-01T00:05:10Z"
  },
  "schedule": {
    "enabled": true,
    "cron": "0 * * * *",
    "syncStatus": "succeeded",
    "lastSyncedAt": "2026-07-01T00:05:30Z"
  },
  "retention": {
    "keepLast": 7
  }
}
```

规则：

- `DR Config` 是配置是否生效，不是最近一次备份是否成功。
- 源集群 `schedule-sync` 和目标集群 `storage-sync` 必须分开记录。
- 源集群 `storage-sync` 失败阻塞 `schedule-sync` 和 DR Config ready。
- 目标集群 `storage-sync` 失败不阻塞源集群 `schedule-sync`，DR Config 可以 ready，但必须返回 warning。
- 页面主状态可以显示 `Ready`，但必须在 tooltip 或详情中展示目标 BSL warning。
- Storage Repository 为空的 task history 记录应被视为数据缺失或任务未关联仓库，不能在页面展示空白；页面应显示 `Not set` 或从 plan/repository 关系补齐。

#### 14.10.4 DR 阶段三 API

阶段三页面至少需要：

- 查询每个 plan/namespace 的同步状态列表。
- 发起单个或批量手动 backup。
- 查询最新可用恢复点。
- 查询 active backup task。
- 跳转 Restore Points 页面时生成 namespace 多选过滤条件。
- 打开 Resource 弹窗时查询 namespace detail。

阶段三列表 API 返回字段建议：

```json
{
  "items": [
    {
      "planId": "plan_001",
      "sourceClusterId": "source_cluster_id",
      "sourceNamespace": "demo-mysql",
      "drConfig": "active",
      "sync": {
        "status": "running",
        "progress": 70,
        "taskId": "task_backup_001",
        "startedAt": "2026-07-01T00:00:34Z",
        "message": "Syncing"
      },
      "latestRestorePoint": {
        "id": "rp_001",
        "veleroBackupName": "hcdr-plan-plan001-20260701000034",
        "completedAt": "2026-07-01T00:00:50Z",
        "sizeBytes": 10737442655
      },
      "resourceSummary": {
        "deployments": 1,
        "statefulSets": 1,
        "services": 3,
        "pvcs": 2,
        "configMaps": 5,
        "secrets": 4
      },
      "pvcSummary": {
        "count": 2,
        "requestedBytes": 21474836480
      }
    }
  ]
}
```

规则：

- 点击 Start Sync 后，页面首先展示平台创建的 active task，不应瞬间显示成功。
- `sync.status=succeeded` 必须来自任务终态和恢复点 upsert 完成，不得只因为 HTTP 创建任务成功或 Velero phase 中间状态而成功。
- 最新恢复点必须只从源集群 `available` 恢复点中选取。
- 如果 backup task 已成功但恢复点 size 仍在采集，sync 可以成功，恢复点 size 显示 `collecting/partial`。
- View Restore Point 只设置 Restore Points 页面的过滤器，不显示额外文字提示条。

#### 14.10.5 Restore Points API

恢复点页面至少需要：

- 查询恢复点列表。
- 查询 namespace 过滤选项，支持多选。
- 查询恢复点详情。
- 发起 restore、drill、takeover。
- 查询相关 task history。

恢复点列表 API 返回字段建议：

```json
{
  "items": [
    {
      "id": "rp_001",
      "sourceClusterId": "source_cluster_id",
      "sourceNamespace": "demo-mysql",
      "displayName": "demo-mysql / 2026-07-01 00:00:50",
      "veleroBackupName": "hcdr-plan-plan001-20260701000034",
      "status": "available",
      "completedAt": "2026-07-01T00:00:50Z",
      "size": {
        "sizeStatus": "complete",
        "totalBytes": 10737442655,
        "metadataBytes": 24415,
        "volumeBytes": 10737418240,
        "uploadedBytes": 10737442655,
        "uploadedMetadataBytes": 24415,
        "uploadedVolumeBytes": 10737418240
      },
      "repository": {
        "id": "repo_001",
        "name": "my-minio"
      }
    }
  ]
}
```

规则：

- 默认 `status=available`。
- size 主列只显示 `totalBytes`，细分信息放 tooltip 或详情。
- `unknown` 只能作为临时异常显示；目标实现中应通过 `sizeStatus=collecting/partial/failed` 表达原因。
- 恢复点页面不展示任务运行状态；任务运行状态在 Task History 页面展示。

#### 14.10.6 Backup and Restore Tasks API

任务历史页面至少需要：

- 查询任务列表。
- 查询任务详情。
- 查询错误详情。
- 查询关联恢复点。
- 支持时间窗口过滤。
- 支持 task type、task status、namespace、repository 等过滤。

默认查询规则：

- 默认时间窗口建议固定为最近 `7d`。
- 用户可以切换 `24h`、`7d`、`30d` 或自定义范围。
- 页面不默认查询全部历史。

任务列表 API 返回字段建议：

```json
{
  "timeRange": {
    "from": "2026-06-24T00:00:00Z",
    "to": "2026-07-01T00:00:00Z"
  },
  "summary": {
    "total": 42,
    "backup": 30,
    "restore": 8,
    "drill": 4,
    "running": 2,
    "succeeded": 35,
    "failed": 5
  },
  "items": [
    {
      "id": "task_backup_001",
      "type": "backup",
      "status": "succeeded",
      "sourceNamespace": "demo-mysql",
      "targetNamespace": "",
      "repository": {
        "id": "repo_001",
        "name": "my-minio"
      },
      "restorePoint": {
        "id": "rp_001",
        "displayName": "demo-mysql / 2026-07-01 00:00:50",
        "status": "available",
        "veleroBackupName": "hcdr-plan-plan001-20260701000034"
      },
      "startedAt": "2026-07-01T00:00:34Z",
      "completedAt": "2026-07-01T00:00:50Z",
      "durationSeconds": 16,
      "progress": 100,
      "error": null
    }
  ]
}
```

规则：

- 第一列叫 `Task Type`，显示 backup/restore/drill/takeover/cleanup 等用户可理解类型和图标。
- `cluster` 字段默认不在列表展示；源/目标集群可放详情或过滤条件中。
- backup 类型下方不展示 commandId、taskId 等长串技术 ID。
- 失败任务必须展示用户可理解错误信息，技术详情放详情弹窗。
- running 状态统一显示 `Running`，不要显示 `Accepted/Pending` 给普通用户。
- 任务状态只收敛为 `running/succeeded/failed` 给页面展示。

### 14.11 弹窗与抽屉页面覆盖

所有弹窗/抽屉都必须从平台 API 读取数据。弹窗打开可以触发平台按需刷新，但不能由前端直接等待 agent。

| 弹窗/抽屉 | 触发页面 | 需要的数据 | 是否触发 agent | 规则 |
| --- | --- | --- | --- | --- |
| Resource Detail | DR 阶段一、阶段三 | namespace resources | 可能触发 `platform.inventory.request scope=namespaceResources` | 展示 workloads/network/storage/config/rbac/policy/jobs |
| Restore Point Detail | Restore Points、Task History | restore point、size、Velero backup 摘要 | 默认不触发 | 只展示源集群恢复点 |
| Task Detail | Task History、阶段三 active task | task events、错误、关联 Velero 对象 | 默认不触发 | 显示状态时间线和错误详情 |
| Restore 确认 | Restore Points、阶段三 | restore point、目标集群、目标 namespace | 不触发 | 创建 restore task 后通过 task event 更新 |
| Drill 确认 | Restore Points、阶段三 | restore point、演练目标 namespace | 不触发 | 创建 drill task，不创建新恢复点 |
| Takeover 确认 | Restore Points、阶段三 | restore point、目标集群、风险确认 | 不触发 | 必须有二次确认 |
| Storage Test | 阶段二 | repository 连接配置 | 由平台执行或下发测试 | 返回明确成功/失败和原因 |
| Unregister 确认 | 集群列表 | 关联 plan/task/restore point 影响范围 | 在线 cleanup 时触发 unregister task | 离线只能 soft unregister |

Resource Detail 展示规则：

- 顶部展示 namespace 名称、采集时间、资源总数、PVC 总申请容量。
- 主体按资源组分区，不把所有资源混成一张难读的大表。
- 每组内部表格字段要少而明确，例如 name、kind、status、age、关键规格。
- ConfigMap/Secret 只显示 key 数量和引用关系，不显示内容。
- PVC 容量放在 storage 组中，同时列表 Resource 摘要 tooltip 也可以展示 PVC requested capacity。

Task Detail 展示规则：

- 主标题使用用户可理解名称，例如 `Backup demo-mysql`。
- 副标题展示 startedAt、duration、repository、restore point displayName。
- 时间线展示 accepted、running、completed/failed。
- 错误详情默认折叠，展开后显示 errorCode、message、Velero phase、原始 details。

Restore Point Detail 展示规则：

- 主标题使用 namespace + 完成时间。
- 主要字段：source cluster、namespace、repository、completedAt、size、status。
- Velero backup name 作为次要技术信息展示，不作为主标题。
- size 细分放 tooltip 或详情区：metadata、volume、uploaded、accuracy。

### 14.12 协议覆盖评估与补充约定

按当前目标协议，页面需求可以被覆盖，但实现时必须补齐以下平台侧能力：

1. 平台 API 层必须提供页面聚合数据，前端不能自己拼 task、restore point、inventory。
2. inventory summary/detail 必须落库或缓存，并带 collectedAt/hash，支持页面判断新鲜度。
3. Task History 必须有 task events 表或等价事件日志，支持任务详情时间线。
4. Restore Point 必须保存 size 明细、sizeStatus、repository、sourceNamespace、sourceClusterId。
5. DR 阶段三同步状态必须由后端聚合生成，不能让前端根据多个接口自行判断。
6. 平台必须实现可见行局部更新通道，例如 SSE/WebSocket 或轮询当前行状态，但不能整表自动刷新。
7. 平台必须在收到终态 event 并完成持久化后再 ack，否则 agent 会误以为最终结果已被平台可靠接收。
8. 定时备份首次 event 到达时，平台必须能幂等创建 task，并在 ack 中返回 taskId/commandId。
9. 目标集群 Backup CR 必须在 agent 和平台双层被过滤，不能进入恢复点和源集群 backup task。
10. 所有 restore/drill/takeover 操作都必须以 `available` restore point 为输入，不能直接以 Velero backup name 为输入。

如果以上能力未实现，页面会出现这些典型问题：

- 点击同步后瞬间成功、随后又出现进度条。
- 恢复点先显示旧数据，几秒后才跳到新数据。
- 翻页后一段时间自动回第一页。
- 目标集群同名 Backup CR 导致恢复点减少、错乱或被标记清除。
- 任务长期停在 pending/running/finalizing。
- Resource 弹窗数据遮挡、缺字段或混乱，因为页面拿不到稳定 detail schema。

### 14.13 页面刷新与局部更新硬规则

- 打开页面时获取平台当前数据。
- 翻页、筛选、排序只操作当前查询条件，不触发全局自动刷新。
- 后台 event 只局部更新当前可见行状态。
- 除用户点击刷新、页面首次加载、筛选条件变化外，列表不应整体重新查询。
- agent inventory summary 的后台更新不能重置页面分页和选择项。
- running 任务状态应及时更新，但不能刷新整张表。
- 终态事件可靠送达后，平台更新任务和恢复点；页面通过局部状态更新反映结果。

## 15. 实现检查清单

每新增任务类型、状态转换、页面数据来源或 agent 消息，都必须同步更新：

- `platform/backend/internal/protocol/messages.go`
- 后端 task 状态处理逻辑。
- agent executor 和 reporter。
- task event reason 映射。
- 恢复点创建规则。
- inventory summary/detail schema。
- 前端 UI 状态映射。
- 页面平台 API 过滤和刷新规则。
- 本文档。
