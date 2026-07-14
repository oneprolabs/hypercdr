# 中控平台下发 Agent 的任务 JSON 定义

更新时间：2026-07-01

本文只定义中控平台发送给 `comm-agent` 的任务消息格式。所有任务都通过 WebSocket 消息 `platform.task.dispatch` 下发，具体任务内容放在统一消息封装的 `payload` 中。

## 统一消息封装

所有任务下发消息都使用同一个外层结构：

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

字段说明：

- `version`：协议版本，当前为 `v1`。
- `messageId`：本条 WebSocket 消息 ID。
- `messageKind`：消息种类。任务下发为 `request`。
- `type`：消息类型。任务下发固定为 `platform.task.dispatch`。
- `tenantId`：租户 ID。
- `clusterId`：执行任务的集群 ID。
- `agentId`：目标 agent ID，可为空。
- `timestamp`：消息发送时间。
- `payload`：具体任务内容。

`payload` 基础结构：

```json
{
  "taskId": "task_id",
  "commandId": "command_id",
  "type": "backup",
  "deadline": "2026-07-01T00:30:00Z"
}
```

`payload.type` 是任务类型。当前任务类型包括：

- `storage-sync`
- `schedule-sync`
- `backup`
- `restore`
- `drill`
- `takeover`
- `retention-cleanup`
- `unregister`

规则：

- 每个任务只能有一个具体命令体。
- `backup` 任务使用 `payload.backup`。
- `restore`、`drill`、`takeover` 任务都使用 `payload.restore`。
- `storage-sync` 使用 `payload.storageSync`。
- `schedule-sync` 使用 `payload.scheduleSync`。
- `retention-cleanup` 使用 `payload.retentionCleanup`。
- `unregister` 使用 `payload.unregister`。

## 1. `storage-sync`

用途：把对象存储配置和凭据下发到执行集群，创建或更新 Velero `BackupStorageLocation`。

执行集群：需要使用该存储仓库的集群。源集群和目标集群都可能需要执行。

业务规则：

- 源集群 `storage-sync` 失败会阻塞 `schedule-sync`，DR Config 不能显示 ready。
- 目标集群 `storage-sync` 失败不阻塞源集群 `schedule-sync`，DR Config 可以显示 ready，但必须带 warning。
- 目标集群 warning 应说明恢复、演练、接管可能不可用。
- 平台收到失败结果后自动重试 `storage-sync`，最多 3 次。
- 3 次仍失败时，源集群失败显示配置失败，目标集群失败显示 Ready with warning。
- 最终失败后页面提供手动重新下发 BSL 的入口，目标集群 BSL 可单独通过该入口重试并清除 warning。

命令体字段：

- `repositoryId`：平台存储仓库 ID，必填。
- `name`：Velero BackupStorageLocation 名称，必填。
- `type`：存储类型，当前主要是 `S3`。
- `endpoint`：对象存储 endpoint。
- `bucket`：bucket 名称，必填。
- `region`：region，可选。
- `tlsEnabled`：是否启用 TLS。
- `secretRef`：Kubernetes Secret 名称。
- `credentials.accessKey`：访问密钥，仅下发时携带。
- `credentials.secretKey`：访问密钥，仅下发时携带。
- `config`：额外配置。

完整样例：

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

期望 agent 回报：

- `agent.task.accepted`
- `agent.task.progress`
- `agent.task.completed` 或 `agent.task.failed`

成功条件：

- Velero `BackupStorageLocation` 状态为 `Available`。

## 2. `schedule-sync`

用途：为保护计划创建或更新 Velero `Schedule`，用于定时生成备份。

执行集群：源集群。

命令体字段：

- `planId`：保护计划 ID，必填。
- `scheduleName`：Velero Schedule 名称，必填。
- `cron`：cron 表达式，必填。
- `sourceNamespaces`：要备份的 namespace 列表，必填。
- `scope`：备份范围，例如 `namespace`。
- `labelSelector`：标签选择器，可选。
- `storageRepo`：Velero BackupStorageLocation 名称，必填。
- `includeClusterResources`：是否包含集群级资源。
- `excludeResources`：排除资源规则。
- `hooks`：备份 hooks。

完整样例：

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
      "sourceNamespaces": ["demo-mysql-csi"],
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

成功条件：

- Velero `Schedule` manifest apply 成功。
- 当前实现 apply 成功后即认为 `schedule-sync` 成功，不等待下一次 backup 产生。

## 3. `backup`

用途：手动触发一次备份，创建 Velero `Backup`。

执行集群：源集群。

命令体字段：

- `planId`：保护计划 ID。
- `sourceNamespace`：要备份的 namespace，必填。
- `scope`：备份范围，例如 `namespace`。
- `labelSelector`：标签选择器，可选。
- `storageRepo`：Velero BackupStorageLocation 名称，必填。
- `includeClusterResources`：是否包含集群级资源。
- `excludeResources`：排除资源规则。
- `hooks`：备份 hooks。

完整样例：

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

Agent 创建的 Velero Backup 必须携带关键 labels：

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

成功条件：

- Velero Backup phase 为 `Completed`。
- 平台收到 `agent.task.completed` 或 `agent.velero.event` 的 `backup_completed`。
- 平台创建或更新恢复点。

注意：

- `progress=100` 不代表备份成功。
- 只有 task status 为 `succeeded`，或者 Velero phase 为 `Completed` 后，才算成功。

## 4. `restore`

用途：从一个恢复点恢复应用。

执行集群：目标恢复集群，可以是源集群，也可以是其他集群。

命令体字段：

- `restorePointId`：平台恢复点 ID，必填。
- `veleroBackupName`：Velero Backup 名称，必填。
- `storageRepo`：Velero BackupStorageLocation 名称，必填。
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

完整样例：

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

成功条件：

- 目标集群可见对应 Velero Backup。
- Velero Restore phase 为 `Completed`。
- 如果启用 readiness reader，还需要目标 namespace/application ready。

## 5. `drill`

用途：演练恢复。协议上与 `restore` 使用同一个 `restore` 命令体，只是 `payload.type` 为 `drill`。

执行集群：演练目标集群。

典型差异：

- `targetNamespace` 通常是隔离 namespace，例如 `demo-mysql-csi-drill`。
- `targetMode` 通常是 `sandbox` 或 `crossCluster`。
- 不应覆盖生产 namespace。

完整样例：

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

## 6. `takeover`

用途：接管恢复。协议上与 `restore` 使用同一个 `restore` 命令体，只是 `payload.type` 为 `takeover`。

执行集群：接管目标集群。

完整样例：

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
- 平台任务状态变为 `succeeded`。

## 7. `retention-cleanup`

用途：按保留策略删除过期恢复点对应的 Velero backup。

执行集群：源集群。

命令体字段：

- `planId`：保护计划 ID。
- `restorePoints`：要清理的恢复点列表。
- `restorePoints[].id`：平台恢复点 ID。
- `restorePoints[].veleroBackupName`：Velero Backup 名称。
- `restorePoints[].namespace`：Velero Backup 所在 namespace，默认 agent namespace。

完整样例：

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
        },
        {
          "id": "rp_old_002",
          "veleroBackupName": "hcdr-plan-plan001-20260630010034",
          "namespace": "hypercdr-agent"
        }
      ]
    }
  }
}
```

成功条件：

- agent 成功提交 Velero `DeleteBackupRequest`。
- 如果 delete waiter 可用，需要等待 Velero Backup 被删除。
- 平台将对应恢复点标记为 `deleted`。

## 8. `unregister`

用途：注销集群，清理集群侧 agent/Velero 资源。

执行集群：要注销的集群。

命令体字段：

- `clusterId`：要注销的集群 ID。
- `namespace`：agent namespace。
- `deleteVelero`：是否删除 Velero 相关资源。
- `deleteNamespace`：是否删除 agent namespace。
- `reason`：注销原因。

完整样例：

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

成功条件：

- agent 执行集群侧 cleanup 成功。
- 平台收到 `agent.task.completed`。
- 平台完成集群注销状态更新。

## Agent 回包格式

所有任务都应按以下生命周期回包：

```text
agent.task.accepted
agent.task.progress   可出现 0 次或多次
agent.task.completed  成功终态
agent.task.failed     失败终态
```

`agent.task.accepted`：

```json
{
  "version": "v1",
  "messageId": "msg_accepted_001",
  "messageKind": "request",
  "type": "agent.task.accepted",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:01Z",
  "payload": {
    "taskId": "task_backup_001",
    "commandId": "cmd_backup_001",
    "acceptedAt": "2026-07-01T00:00:01Z"
  }
}
```

`agent.task.progress`：

```json
{
  "version": "v1",
  "messageId": "msg_progress_001",
  "messageKind": "request",
  "type": "agent.task.progress",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:10Z",
  "payload": {
    "taskId": "task_backup_001",
    "status": "running",
    "progress": 70,
    "message": "velero backup running",
    "velero": {
      "kind": "Backup",
      "name": "hcdr-plan-plan001-20260701000034",
      "namespace": "hypercdr-agent",
      "phase": "InProgress"
    }
  }
}
```

目标统一备份完成事件：`agent.backup.completed`

```json
{
  "version": "v1",
  "messageId": "msg_backup_completed_001",
  "messageKind": "event",
  "type": "agent.backup.completed",
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
      "name": "hcdr-plan-plan001-20260701000034",
      "namespace": "hypercdr-agent",
      "phase": "Completed",
      "startedAt": "2026-07-01T00:00:34Z",
      "completedAt": "2026-07-01T00:00:50Z"
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

`triggerType=manual` 表示手动同步，必须携带 `taskId` 和 `commandId`；`triggerType=schedule` 表示定时备份，`taskId` 和 `commandId` 可以为空，由平台按 `sourceClusterId + planId + backup.name` 幂等创建或查找任务。

`agent.task.failed`：

```json
{
  "version": "v1",
  "messageId": "msg_failed_001",
  "messageKind": "request",
  "type": "agent.task.failed",
  "clusterId": "cluster_id",
  "agentId": "agent_id",
  "timestamp": "2026-07-01T00:00:50Z",
  "payload": {
    "taskId": "task_backup_001",
    "errorCode": "BACKUP_FAILED",
    "message": "velero backup failed",
    "details": {
      "phase": "Failed",
      "errors": 1
    }
  }
}
```

## 必须遵守的规则

- 平台下发任务时，业务命令必须放在统一消息封装的 `payload` 中。
- `payload.type` 决定任务类型。
- 每个任务只能携带一个命令体。
- `progress=100` 不代表成功，只有 `agent.task.completed` 或标准完成事件才代表成功。
- 失败任务必须通过 `agent.task.failed` 返回错误码和错误信息。
- `backup` 成功后才允许创建恢复点。
- 恢复点只属于源集群。
- 目标集群 Backup CR 不能创建恢复点。
