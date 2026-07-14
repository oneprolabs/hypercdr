# DR Task Protocol and State Machine

Last updated: 2026-07-01

This document defines the target runtime contract between the HyperCDR central
platform and the cluster-side `comm-agent` for DR plans, backup tasks, restore
tasks, restore points, inventory, and UI state mapping. Program changes should
use this document as the target behavior.

This is the source of truth for task state transitions. The UI must not infer
business state from ad hoc strings, progress numbers, or intermediate Velero
states when platform task state, standard Velero events, or restore point state
is available.

## 0. Table of Contents

- 1. Core Objects
- 2. WebSocket Envelope
- 3. Platform-Initiated Interactions
- 4. Agent-Initiated Interactions and Events
- 5. Backup Execution Flow
- 6. Restore / Drill / Takeover Flow
- 7. Retention Cleanup Flow
- 8. Size and Progress Contract
- 9. UI State Mapping
- 10. Target Task JSON Definitions
- 11. Task Definitions
- 12. Idempotency and Ordering
- 13. Hard Invariants
- 14. Page and Platform API Mapping
- 15. Implementation Checklist

## 1. Core Objects

### 1.1 Protection Plan

A protection plan is the durable DR configuration for one source namespace or
application.

Key fields:

- `id`: platform plan id.
- `sourceClusterId`: cluster where backups are produced.
- `appId`: protected namespace/application.
- `storageRepoId`: object repository used by Velero.
- `targetClusterId`: optional recovery target cluster.
- `status`: configuration status, not backup execution status.

Plan statuses:

- `draft`: user selected a namespace, but DR has not been fully configured.
- `configuring`: platform is preparing storage or schedule configuration.
- `active`: storage and schedule configuration are effective.
- `storage_failed`: storage configuration failed.
- `schedule_failed`: schedule configuration failed.
- `disabled`: plan is disabled.

Hard constraints:

- Restore points belong to the source cluster only.
- Velero Backup CRs on target clusters must not create platform restore points.
- `protection_plans.source_cluster_id` is the authority for restore point
  ownership.

### 1.2 Task

A task is one executable operation created by the platform.

Key fields:

- `id`: platform task id.
- `commandId`: idempotency/business correlation id for one command.
- `type`: task type.
- `clusterId`: cluster that must execute the task.
- `protectionPlanId`: related plan.
- `restorePointId`: related restore point.
- `status`: platform task lifecycle state.
- `progress`: progress number from `0..100`.
- `payload`: task facts, such as Velero names and size data.

Supported task types:

- `storage-sync`: apply object storage credentials and Velero
  `BackupStorageLocation`.
- `schedule-sync`: create or update a Velero `Schedule` for a plan.
- `backup`: create or observe one Velero `Backup`.
- `restore`: restore from one restore point.
- `drill`: perform drill restore.
- `takeover`: perform takeover restore.
- `retention-cleanup`: delete expired Velero backups and mark restore points
  deleted.
- `unregister`: remove agent/Velero resources and unregister the cluster.

Terminal task states:

- `succeeded`
- `failed`

Active task states:

- `queued`
- `dispatched`
- `accepted`
- `running`
- `syncing`

`stopped` is only a UI/operator state until an end-to-end cancellation protocol
exists. It must not be treated as a successful backend terminal state.

### 1.3 Restore Point

A restore point is a durable platform record created after a source-cluster
Velero backup completes.

Responsibility boundary:

- Velero owns Backup CRs in the cluster and backup data in object storage.
- The agent observes Velero Backup CRs and reports standard events.
- The platform is the only component that creates, updates, or deletes platform
  restore point records.
- The agent must not directly create restore points or request unconditional
  restore point creation.
- The platform must validate event origin, plan ownership, source cluster, and
  Velero terminal state before creating or updating a restore point.

Identity fields:

- `sourceClusterId`
- `protectionPlanId`
- `appId`
- `veleroBackupName`

Uniqueness rule:

- `(sourceClusterId, veleroBackupName)` identifies one restore point.
- Duplicate events for the same Velero backup must update the existing restore
  point, not create another one.

Creation triggers:

- Manual backup: platform creates a backup task first; the agent executes and
  reports terminal state; the platform creates or updates the restore point
  after receiving a valid successful terminal event.
- Scheduled backup: Velero Schedule creates a Backup CR; the agent observes it
  and reports `agent.velero.event`; the platform creates the backup task on the
  first valid event and creates or updates the restore point on valid
  `backup_completed`.
- Reconciliation: the platform may compensate missing records from inventory or
  Velero events, but must still enforce source cluster, plan ownership, and
  Velero `Completed` validation.

Cases that must not create restore points:

- Backup CR is not managed by a HyperCDR plan.
- Backup CR comes from a target cluster.
- Backup phase is `Failed`, `FailedValidation`, `PartiallyFailed`, `Canceled`,
  or any non-terminal phase.
- The event cannot be matched to a valid
  `protectionPlanId/sourceClusterId/sourceNamespace/veleroBackupName`.
- The platform cannot confirm that the backup data is usable for restore.

Restore point statuses:

- `available`: can be used for restore, drill, or takeover.
- `deleted`: backup has been removed from Velero/repository or hidden by cleanup.
- `clearing`: deletion request has been issued.
- `failed`: historical/compensation compatibility state only; the new flow
  should not create a restore point for failed backups.

The Restore Points page shows only `available` by default. Task History may show
failed, deleted, or cleared relationships.

## 2. WebSocket Envelope

All platform-agent WebSocket messages use a unified envelope.

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

Rules:

- `messageId` identifies one logical message and must be globally unique.
- Retrying the same logical message must resend the same cached JSON with the
  same `messageId`.
- `messageKind` defines communication semantics: `request`, `response`, or
  `event`.
- `type` defines business message type. It must not be used to infer whether a
  response is required.
- Every `request` must receive one `response`.
- A `response` is never acknowledged again.
- An `event` requires a response only when `payload.ackRequired=true`.
- Events with `payload.ackRequired=false` are not acknowledged and are not
  retried.
- Events with `payload.ackRequired=true` must be acknowledged and retried until
  acknowledged, unless the receiver returns a non-retryable error.
- Response payloads must include `ackMessageId` pointing to the message being
  acknowledged.
- Response payloads may include `ackType` for the acknowledged business type.
- Task messages use `payload.commandId` for business idempotency.
- Non-task requests use `payload.requestId` for business correlation.
- There is no top-level `correlationId`; it would duplicate
  `payload.commandId/requestId`.
- `clusterId` is the executing cluster.
- The platform must ignore task or Velero messages from the wrong cluster.
- The agent must not execute tasks whose `clusterId` does not match its
  registered cluster id.

### 2.1 Request and Response Matrix

| Message | kind | Sender | Must respond | Success response | Failure response | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| `agent.register` | `request` | agent | yes | `platform.register.accepted` | `platform.register.rejected` | Agent is authenticated only after accepted |
| `platform.task.dispatch` | `request` | platform | yes | `agent.task.accepted` | `agent.task.failed` | Accepted only means received |
| `platform.task.cancel` | `request` | platform | reserved | reserved | `agent.message.error` | Not implemented yet |
| `platform.inventory.request` | `request` | platform | yes | `agent.inventory.report` | `agent.message.error` | On-demand inventory, not a task |
| `agent.heartbeat` | `event` | agent | no | none | none | `ackRequired=false` |
| `agent.inventory.report` | `event` | agent | no | none | none | Summary push or 5-minute resync |
| `agent.task.progress` | `event` | agent | no | none | none | Transient progress |
| `agent.task.completed` | `event` | agent | yes | `platform.event.ack` | `platform.event.error` | Terminal success |
| `agent.task.failed` | `event` | agent | yes | `platform.event.ack` | `platform.event.error` | Terminal failure |
| `agent.velero.event` | `event` | agent | depends | `platform.event.ack` | `platform.event.error` | Terminal Velero events require ack |

Mandatory rules:

- `messageKind` must explicitly distinguish requests, responses, and events.
- `request` must be answered.
- `response` is never answered.
- `event` response is controlled by `payload.ackRequired`.
- `agent.task.accepted` is the response to `platform.task.dispatch`.
- `agent.task.progress` is a transient event and defaults to
  `payload.ackRequired=false`.
- `agent.task.completed` and `agent.task.failed` are terminal events and must
  use `payload.ackRequired=true`.
- Task messages must close the loop as `dispatch -> accepted -> completed/failed`.
- `accepted` is not success; it only means the agent received the task.
- Platform-dispatched tasks must correlate by `taskId + commandId`.
- Scheduled backup tasks may be created by the platform on the first valid event.
- Unknown task dispatches must return `agent.task.failed`; they must not be
  silently ignored.

### 2.2 Reliable Event Acknowledgement

Events with `payload.ackRequired=true` must receive a lightweight response. This
is mainly used for task terminal events and Velero terminal events.

Success response:

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

Failure response:

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

Rules:

- `platform.event.ack` only means the platform has persisted the event. It does
  not change the event's business meaning.
- For the first scheduled backup `agent.velero.event`, the platform should
  idempotently create or find the backup task, then return the created/found
  `taskId` and `commandId` in the ack payload.
- After receiving that ack, the agent should include the returned `taskId` and
  `commandId` in later events for the same `veleroBackupName` when possible.
- `platform.event.error.retryable=true` means the agent must retry.
- `platform.event.error.retryable=false` means the agent stops retrying and
  records a local error.
- The platform must not ack before critical state is written.
- For `agent.task.completed`, the platform should ack only after the task
  terminal state is persisted and the restore point is created/updated or queued
  for compensation.
- For `agent.task.failed`, the platform should ack only after failure state and
  error information are persisted.

## 3. Platform-Initiated Interactions

### 3.1 Dispatch Task: `platform.task.dispatch`

Purpose: ask an agent to execute one task, such as backup, restore, drill,
takeover, storage sync, schedule sync, retention cleanup, or unregister.

Request:

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

Accepted response:

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

Receive failure response:

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

Progress event:

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

Successful terminal event:

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

Failed terminal event:

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

Rules:

- `agent.task.accepted` only means the agent accepted the task.
- `agent.task.accepted` is a `response`, not business success.
- `agent.task.progress` is a later `event` with `payload.ackRequired=false`.
- `agent.task.completed` is a terminal success event with
  `payload.ackRequired=true`.
- `agent.task.failed` has two meanings:
  - receive failure response before the task is accepted.
  - terminal failure event after the task was accepted and started.
- Unknown task type or missing required payload fields must return
  `agent.task.failed` as the receive failure response.
- Task idempotency uses `payload.commandId`, not `messageId`.
- Progress events carry only lightweight metrics: `progress`, `totalBytes`,
  `syncedBytes`, `speedBytesPerSecond`, `percent`, `etaSeconds`.
- Final backup size detail is included only in the backup terminal event.

### 3.2 Cancel Task: `platform.task.cancel`

Purpose: reserved for cancelling an unfinished task. Until cancellation is
implemented end to end, stop/cancel must not be treated as successful
completion.

Current not-implemented response:

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

Rules:

- Current version should return `agent.message.error`, not silently ignore it.
- Once implemented, cancellation itself must have accepted and terminal events.
- The original task must enter a clear `cancelled` or `failed` state, not
  masquerade as `succeeded`.

### 3.3 Request Inventory: `platform.inventory.request`

Purpose: ask the agent to collect and return inventory immediately. This does
not create a task.

Request:

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

Success response:

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

Rules:

- Success response must carry the same `requestId`.
- Success response must include `ackMessageId` pointing to the request message.
- `scope=summary` requests current full inventory summary.
- `scope=namespaceResources` requests resource detail for one namespace.
- `scope=fullDetail` is reserved for manual refresh, diagnostics, or
  compensation. It is not the default page request.
- Namespace detail must not include Secret values, ConfigMap values, pod logs,
  full Events, or Endpoint details.
- Failure uses `agent.message.error`, not `agent.task.failed`.
- The agent does not replay `agent.inventory.report` responses.

## 4. Agent-Initiated Interactions and Events

### 4.1 Register or Reconnect: `agent.register`

Purpose: agent startup, restart, or WebSocket reconnect identity declaration.

Request:

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

Success response:

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

Rules:

- `agent.register` is sent on first startup, restart, and reconnect.
- The agent is authenticated only after `platform.register.accepted`.
- After `platform.register.rejected`, the agent must not execute platform tasks.
- First registration may use `installToken`; later reconnects should use
  `agentCredential + clusterId`.

### 4.2 Heartbeat Event: `agent.heartbeat`

Purpose: periodically report agent and core component health.

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

Rules:

- `messageKind=event`; the platform does not respond.
- `payload.ackRequired=false`; the agent does not wait or retry.
- Heartbeat is not a task state transition and must not complete tasks.
- Heartbeat carries only connection, agent, and core component health.
- Resource counts such as `nodeCount`, `namespaceCount`, `applicationCount`, and
  `inventoryHash` must come from inventory summary, not heartbeat.

### 4.3 Inventory Summary Event: `agent.inventory.report`

Purpose: agent proactively reports full inventory summary. On-demand inventory
responses are defined in 3.3.

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

Rules:

- `messageKind=event`; no platform response.
- `payload.ackRequired=false`; no retry.
- Proactive reports send full summary, not all namespace resource details.
- Any watched resource change that changes `summaryHash/inventoryHash` triggers
  a debounced full summary push.
- If nothing changes, the agent sends a full summary every `5min`.
- Inventory may compensate missed Velero events, but must not bypass task
  ownership, ledger, or source-cluster validation.

### 4.4 Inventory Scope, Frequency, and Performance Protection

Target model:

```text
agent watches relevant cluster resources
 -> maintains local inventory summary cache
 -> summary-related change occurs
 -> debounce and merge
 -> push one full summary
 -> if no change, push one full summary every 5min
 -> namespace resource detail is requested on demand
```

Default frequency:

| Type | Direction | Trigger | Default |
| --- | --- | --- | --- |
| heartbeat | agent -> platform | fixed interval | `30s` |
| inventory summary | agent -> platform | summary hash changed | debounce `8s` |
| inventory min interval | agent -> platform | rate limit | `15s` |
| inventory resync | agent -> platform | no change | `5min` |
| namespace detail | platform -> agent -> platform | modal open/refresh/cache expired | on demand |
| full detail | platform -> agent -> platform | manual refresh/diagnostics/compensation | not automatic |

Proactive summary scope:

- Cluster basics: kubeVersion, nodeCount, namespaceCount, applicationCount.
- All namespaces: name, status, label summary.
- Resource count summary for every namespace.
- PVC summary for every namespace: count, total requested capacity, binding
  status counts.
- `inventoryHash` and per-namespace `summaryHash`.
- Velero health summary, BSL/VSL summary.

On-demand detail scope:

- `scope=namespaceResources` returns resource list for one namespace.
- Resources are grouped for user understanding: `workloads`, `network`,
  `storage`, `config`, `rbac`, `policy`, `jobs`.
- ConfigMap returns name, key count, references, and metadata, not values.
- Secret returns name, type, key count, references, and metadata, not values.
- Pod logs, full Events, and Endpoint details are not returned by default.

Recommended watched resources:

- Namespace, Node.
- Deployment, StatefulSet, DaemonSet.
- Service, Ingress.
- PVC, PV, StorageClass.
- ConfigMap metadata, Secret metadata.
- ServiceAccount, Role, RoleBinding.
- HPA, PDB, NetworkPolicy.
- Job, CronJob.
- Velero Backup, Schedule, Restore, BackupStorageLocation,
  VolumeSnapshotLocation, PodVolumeBackup, DataUpload.

Performance rules:

- The agent must use shared informer/cache, not periodic full list of all
  resources.
- Initial list is allowed on startup or reconnect; after that the agent updates
  cache from watch events.
- If summary hash is unchanged, do not push inventory for a single watch event.
- Merge multiple changes through debounce.
- Enforce minimum push interval, default `15s`.
- Large clusters must support resource-type switches and namespace filters.
- Suggested Kubernetes client limits: `QPS=5-10`, `Burst=10-20`.
- Failed inventory push is not replayed; the next change or 5-minute resync
  overwrites it.

### 4.5 Task Progress and Terminal Events

Rules:

- `agent.task.progress` uses `payload.ackRequired=false`; no platform response.
- `agent.task.completed` and `agent.task.failed` use
  `payload.ackRequired=true`; the platform must respond with
  `platform.event.ack/error`.
- Terminal events must be retried until ack/error.
- `agent.task.*` events must carry `taskId` and `commandId`.
- Progress must not regress, except terminal failure.
- `progress=100` is not success by itself.
- Progress events carry only lightweight fields: `progress`, `totalBytes`,
  `syncedBytes`, `speedBytesPerSecond`, `percent`, `etaSeconds`.
- Backup final size detail is included only in the terminal backup event.
- Only `agent.task.completed` or a standard completed event can set task state
  to `succeeded`.
- Failure events must include `errorCode` and a user-readable `message`.

### 4.6 Velero Observation Event: `agent.velero.event`

Purpose: agent reports observed Velero Backup/Restore CR status changes.

Manual backup example:

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

Rules:

- `messageKind=event`; response depends on `payload.ackRequired`.
- `eventType` values include `backup_started`, `backup_progress`,
  `backup_completed`, `backup_failed`.
- `New` or empty phase maps to `backup_started`.
- `InProgress` maps to `backup_progress`.
- `Completed` maps to `backup_completed`.
- `Failed`, `FailedValidation`, `PartiallyFailed`, and `Canceled` map to
  `backup_failed`.
- `backup_progress` uses `payload.ackRequired=false`.
- `backup_completed`, `backup_failed`, `restore_completed`, and
  `restore_failed` use `payload.ackRequired=true`.
- `clusterId != protectionPlan.sourceClusterId` must not create restore points.
- Platform-dispatched task Velero events must carry `taskId` and `commandId`.
- Scheduled backup events may initially omit `taskId/commandId`, but must carry
  `planId`, `scheduleName`, `backupName`, `sourceClusterId/sourceNamespace`, or
  equivalent labels.
- Scheduled backup event idempotency key is
  `sourceClusterId + planId + veleroBackupName`.

## 5. Backup Execution Flow

### 5.1 Manual Backup

1. User clicks Start Sync in DR stage 3.
2. Platform creates a `backup` task with status `queued`.
3. Platform dispatches `platform.task.dispatch` to the source cluster agent.
4. Platform marks task `dispatched` after WebSocket send succeeds.
5. Agent reports `agent.task.accepted`.
6. Agent creates Velero `Backup`.
7. Agent reports `agent.task.progress` or `agent.velero.event`.
8. Velero Backup enters `Completed`.
9. Agent reports `agent.task.completed` or `backup_completed`.
10. Platform marks the task `succeeded`.
11. Platform creates or updates restore point `(sourceClusterId, veleroBackupName)`.
12. UI shows `Sync complete`.

Manual backup restore point rules:

- The backup task is created by the platform first.
- Agent progress and terminal events must match an existing `taskId + commandId`.
- Platform creates or updates restore point only after a valid successful
  terminal event.
- If a manual terminal event cannot match a task, the platform must not create a
  restore point; return non-retryable `platform.event.error TASK_NOT_FOUND`
  unless the event is a valid scheduled backup event.
- `agent.task.completed/failed` is authoritative for manual backup terminal
  state.
- If both `agent.task.completed` and `agent.velero.event backup_completed`
  arrive, merge idempotently by `taskId + commandId + veleroBackupName`.

### 5.2 Scheduled Backup

1. Plan activation creates or updates Velero `Schedule` through `schedule-sync`.
2. Velero creates Backup CRs on schedule.
3. Source cluster agent observes Backup CR.
4. Agent reports `agent.velero.event`.
5. Platform finds or creates the corresponding `backup` task.
6. Platform creates or updates the restore point on `backup_completed`.

Scheduled backup task rules:

- `schedule-sync` only means schedule configuration succeeded. It is not one
  backup execution.
- Every scheduled backup execution maps to one independent `backup` task.
- The platform creates that backup task on the first valid scheduled
  `agent.velero.event`.
- If the first event is already `backup_completed`, the platform must still
  create or find the backup task and create the restore point in the same
  handling path.
- Repeated events with the same `sourceClusterId + planId + veleroBackupName`
  update the existing task only.
- Scheduled executions are uploaded by events from start to finish. The platform
  does not dispatch a separate backup task for every scheduled run.

Required scheduled backup labels:

- `hypercdr.io/managed-by=hypercdr`
- `hypercdr.io/plan-id`
- `hypercdr.io/source-cluster-id`
- `hypercdr.io/source-namespace`
- `velero.io/storage-location`

Scheduled backup restore point rules:

- The agent only reports Backup CR facts; it does not create restore points.
- The platform validates that the Backup CR belongs to a HyperCDR-managed
  Schedule.
- The platform validates `event.clusterId == protectionPlan.sourceClusterId`.
- The platform validates `backupName/sourceClusterId/planId/sourceNamespace`
  against the plan.
- A valid `backup_completed` upserts restore point by
  `(sourceClusterId, veleroBackupName)`.
- `backup_failed` updates or creates a failed backup task, but does not create
  an `available` restore point.
- Target cluster events for same-name Backup CRs must be rejected or ignored.

## 6. Restore / Drill / Takeover Flow

1. User selects one `available` restore point.
2. Platform creates a `restore`, `drill`, or `takeover` task.
3. Platform dispatches a `restore` command body to the target cluster agent.
4. Agent waits until the referenced backup is visible in the target cluster
   Velero namespace.
5. Agent creates Velero `Restore`.
6. Agent reports progress based on Velero Restore and PodVolumeRestore.
7. After Velero `Completed`, agent reports `agent.task.completed`.
8. Platform marks task `succeeded`.
9. Task History shows restore/drill/takeover success.

Restore-like tasks do not create restore points.

## 7. Retention Cleanup Flow

1. Platform selects expired restore points according to retention policy.
2. Platform creates a `retention-cleanup` task on the source cluster.
3. Agent creates Velero `DeleteBackupRequest` or equivalent deletion action.
4. Agent reports deletion progress.
5. On success, platform marks related restore points `deleted`.

Cleanup tasks must not delete restore points from other source clusters.

## 8. Size and Progress Contract

First-version restore point size is composed of metadata size and logical volume
size.

Rationale:

- Velero metadata is usually much smaller than PVC data.
- Metadata size is cheap and accurate through object storage object stats.
- Velero backup detail does not provide a stable metadata-size field.
- Logical volume size comes from Velero backup detail / BackupVolumeInfos.

Field meanings:

- `totalBytes`: primary restore point size, `metadataBytes + volumeBytes`.
- `metadataBytes`: size of Velero metadata artifacts under
  `backups/<backupName>/`, collected from object storage API.
- `volumeBytes`: logical total of volumes involved in this backup.
- `uploadedBytes`: `uploadedMetadataBytes + uploadedVolumeBytes`.
- `uploadedMetadataBytes`: same as `metadataBytes` in the first version.
- `uploadedVolumeBytes`: true Kopia/FSB incremental upload size is not reliably
  available in the first version, so use `volumeBytes` as an estimate.

Canonical payload:

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

Current FSB/Kopia rules:

- Metadata size is collected by object storage stats for Velero artifacts under
  `backups/<backupName>/`.
- Those artifacts include backup tar, logs, results, volume info, CSI snapshot
  info, and other metadata objects written by Velero for that backup name.
- Metadata size collection failure does not change Velero backup success.
- If metadata size collection fails, platform may create the restore point with
  `sizeStatus=partial` or `collecting`; the UI must not present it as complete.
- Compensation can later update
  `metadataBytes/totalBytes/uploadedMetadataBytes/uploadedBytes/sizeStatus`.
- Volume logical size prefers Velero BackupVolumeInfos.
- BackupVolumeInfos are derived from PodVolumeBackup/DataUpload progress,
  snapshot information, and related Velero status.
- If BackupVolumeInfos lack size, fall back to PodVolumeBackup/DataUpload
  `status.progress.totalBytes`.
- If CR progress also lacks size, a Kopia snapshot stats lookup can be used as
  compensation.
- Volume uploaded size is not always directly available from Velero; until a
  Kopia-specific reader exists, estimate it from volume size and mark it
  `estimated`.
- Suggested `sizeStatus` values: `complete`, `partial`, `collecting`, `failed`.

The UI must show accuracy in details or tooltips. It must not silently present
estimated values as exact values.

## 9. UI State Mapping

The UI must map from backend task and restore point state, not arbitrary event
text.

### 9.1 DR Stage 3 Sync Column

For each namespace/application:

1. Active backup task exists:
   - show `Syncing... N%`.
   - progress source is latest task progress or volume progress.
2. Latest backup task is `succeeded`:
   - show `Sync complete`.
   - subtext shows matched restore point time, latest restore point time, or
     `Restore point indexed`.
   - never show a running progress bar.
3. Latest backup task is `failed`:
   - show `Sync failed`.
   - expose error message and task details.
4. No backup task but an available restore point exists:
   - show `Last snapshot`.
5. Otherwise:
   - show `No snapshot yet`.

`Finalizing restore point` is not a valid final UI state. It can only be shown
briefly while a backend task is still active.

### 9.2 Restore Points Page

Default list:

- Show only `restore_points.status = available`.
- Cluster filter must use `sourceClusterId`.
- Namespace filter uses `sourceNamespace` or included namespaces.

The default list must not show failed, deleted, clearing, or target-cluster
backup artifacts.

### 9.3 Backup and Restore Tasks Page

This is a task history page. It can show:

- Successful tasks.
- Failed tasks with error messages.
- Running tasks.
- Restore point relationships that have been deleted/cleared.

Failed backup tasks do not require restore points.

Default time window is fixed to recent `7d` and must be user-switchable.

## 10. Target Task JSON Definitions

All tasks are dispatched through `platform.task.dispatch`. The concrete task
body lives under the unified envelope `payload`.

Base payload:

```json
{
  "taskId": "task_id",
  "commandId": "command_id",
  "type": "backup",
  "deadline": "2026-07-01T00:30:00Z"
}
```

Rules:

- One `platform.task.dispatch` carries one command body only.
- `payload.type=backup` requires `payload.backup`.
- `payload.type=restore/drill/takeover` requires `payload.restore`.
- `payload.type=storage-sync` requires `payload.storageSync`.
- `payload.type=schedule-sync` requires `payload.scheduleSync`.
- `payload.type=retention-cleanup` requires `payload.retentionCleanup`.
- `payload.type=unregister` requires `payload.unregister`.
- Unknown task types must return `agent.task.failed`.

Standard agent response flow:

```text
agent.task.accepted   response; received by agent, not business success
agent.task.progress   event; payload.ackRequired=false; 0 or more times
agent.task.completed  event; payload.ackRequired=true; terminal success
agent.task.failed     response or event; receive failure as response, execution failure as ackRequired event
```

Standard Velero payload should converge to a stable structure:

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

Standard `volumeProgress`:

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

## 11. Task Definitions

### 11.1 `storage-sync`

Purpose: apply object storage configuration and credentials to a cluster and
create/update Velero `BackupStorageLocation`.

Execution cluster: any cluster that needs this repository. Source and target
clusters may both need it.

Required command fields include:

- `repositoryId`
- `name`
- `type`
- `endpoint`
- `bucket`
- `secretRef`
- `credentials.accessKey`
- `credentials.secretKey`
- `config`

Success condition:

- Secret/config is written successfully.
- Velero `BackupStorageLocation` reaches `Available`.

Suggested failure codes:

- `STORAGE_SYNC_COMMAND_INVALID`
- `BSL_MANIFEST_INVALID`
- `BSL_SUBMIT_FAILED`
- `BSL_STATUS_READ_FAILED`
- `BSL_STATUS_TIMEOUT`
- `BSL_UNAVAILABLE`

### 11.2 `schedule-sync`

Purpose: create or update Velero `Schedule` for a protection plan.

Execution cluster: source cluster.

Important rules:

- HyperCDR backups should not use Velero Backup TTL as restore point retention.
- Restore point retention/expiration/deletion is controlled by platform
  `retention-cleanup`.
- Do not let Velero auto-delete backups through TTL, or platform records may
  point to missing backup data.
- Scheduled Backup CRs must carry HyperCDR labels, including managed-by, plan id,
  source cluster id, and source namespace.

### 11.3 `backup`

Purpose: create one Velero Backup manually, or represent one scheduled Velero
Backup execution created by a schedule.

Execution cluster: source cluster.

Restore point generation rules:

- Restore point belongs to `sourceClusterId`.
- The key is `(sourceClusterId, veleroBackupName)`.
- Failed backup does not generate an `available` restore point.
- Target cluster Backup CR does not generate restore points.

### 11.4 `restore`

Purpose: restore an application from one available restore point.

Execution cluster: target cluster.

Required fields include:

- `restorePointId`
- `veleroBackupName`
- `sourceClusterId`
- `sourceNamespace`
- `targetClusterId`
- `targetNamespace`

Restore tasks do not create restore points.

### 11.5 `drill`

Purpose: restore one restore point into a drill namespace/target for validation.

Rules:

- The task body uses `restore`.
- It must be distinguishable by task `type=drill`.
- Drill does not create a new restore point.

### 11.6 `takeover`

Purpose: production takeover restore.

Rules:

- The page must require explicit confirmation.
- Task History should record target cluster, target namespace, and restore point.
- Takeover does not create a new restore point.

### 11.7 `retention-cleanup`

Purpose: delete expired Velero backups for restore points.

Rules:

- The task runs on the source cluster that owns the restore points.
- It must validate each restore point source before deletion.
- On success, platform marks restore points `deleted`.

### 11.8 `unregister`

Purpose: unregister or clean up a cluster-side agent/Velero installation.

Rules:

- Online cleanup uses a task.
- Offline soft unregister is platform-only and must not pretend cluster-side
  cleanup succeeded.
- Historical tasks and restore points are retained unless the user explicitly
  chooses history cleanup.

## 12. Idempotency and Ordering

The platform and agent must tolerate duplicates, retries, out-of-order events,
and reconnects.

Rules:

- Replayed logical messages reuse the same cached JSON and `messageId`.
- Task business idempotency uses `commandId`.
- Non-task request correlation uses `requestId`.
- Terminal task states win over older non-terminal messages.
- Progress must not regress.
- `backup_completed` can update size/metadata of an existing restore point.
- Restore points are upserted by `(sourceClusterId, veleroBackupName)`.
- Duplicate Velero events must not create duplicate task events when payload is
  identical.
- Late `completed` may correct `failed(AGENT_ACCEPT_TIMEOUT)` to `succeeded`
  if the event is valid and idempotent.
- Late `failed` must not create restore points.

### 12.1 Platform Dispatch Retry

For platform-to-agent `request` messages:

- If no response arrives within the request timeout, resend the same cached
  message with the same `messageId`.
- Maximum dispatch retry is 3 attempts for normal task dispatch.
- If all attempts fail, mark the task dispatch as failed.
- Do not create a new `messageId` for the same logical retry.

### 12.2 Reliable Terminal Event Outbox

For agent terminal events:

- Persist event to local outbox before sending.
- Retry terminal events until `platform.event.ack` or non-retryable
  `platform.event.error`.
- First retry sequence can use short backoff; durable retry interval is `30s`.
- Retry survives agent restart.
- This prevents platform pages from staying pending/running when the final
  result was produced but not received by the platform.

### 12.3 Agent Ledger and Target-Cluster Filtering

Agent must persist accepted platform-dispatched tasks before returning
`agent.task.accepted`.

Ledger goals:

- Resume observation after agent restart.
- Avoid accepting a task and then losing it before reporting final state.
- Prevent target cluster duplicate Backup CRs from being uploaded as source
  backup progress or terminal state.

Target cluster filtering:

- A Backup CR observed on a cluster whose id does not match the plan source
  cluster must not create a restore point.
- Target-cluster same-name Backup CRs not present in local ledger, or whose
  source/execution cluster does not match, must be ignored for restore point and
  backup terminal reporting.
- Scheduled Backup CRs are the exception to ledger lookup only when they satisfy
  managed schedule labels:
  - `hypercdr.io/managed-by=hypercdr`
  - `hypercdr.io/plan-id`
  - `hypercdr.io/source-cluster-id` equals the agent registered cluster
  - `hypercdr.io/source-namespace` or unique included namespace
  - association with a HyperCDR-created Schedule through schedule name or
    `velero.io/schedule-name`.

## 13. Hard Invariants

- Restore points belong to the source cluster only.
- `protection_plans.source_cluster_id` is the restore point ownership authority.
- Target cluster Backup CRs must be ignored for restore point creation even if
  they have the same backup name.
- Backup success is based on Velero Backup terminal phase `Completed` or
  authoritative task terminal success, not progress reaching 100.
- `progress=100` is not enough to mark success.
- Failed backups must not create `available` restore points.
- UI active/running state must come from task state, not progress alone.
- `veleroBackupName` is the primary user-understandable link between Task
  History and Restore Points.
- The platform must validate connection identity, task table, plan table, and
  restore point table. It must not trust agent payload alone.
- A backup terminal event can create/update a restore point only when
  `event.clusterId == protectionPlan.sourceClusterId`.

## 14. Page and Platform API Mapping

This chapter defines where page data comes from, whether agent communication is
needed, and how refresh should behave. Pages must not depend on direct agent
responses. Pages read platform APIs and platform cache; agent communication only
keeps platform state fresh.

### 14.1 General Page Data Principles

- Pages read platform API responses by default, not the agent directly.
- Agent heartbeat, inventory, task events, and Velero events are written into
  the platform store and then served by platform APIs.
- Background events may update visible row state only. They must not reset
  pagination, filters, search, sorting, or selected rows.
- User refresh may cause the platform to trigger an on-demand agent request, but
  the page still reads platform API results.
- Page open can load current platform cache; the platform decides whether to
  trigger agent refresh based on cache age, user action, and page need.
- Long-running task state updates through task events. The whole table must not
  auto-refresh periodically.
- Platform API filters must match business ownership fields. For example,
  restore point cluster filtering must use `sourceClusterId`.

Page data source summary:

| Page | Main data | Platform source | Agent communication | Refresh strategy |
| --- | --- | --- | --- | --- |
| Cluster registration/list | cluster, agent, Velero health | clusters, agent sessions, latest inventory summary | `agent.register`, `agent.heartbeat`, `agent.inventory.report` | heartbeat local update; inventory change/5min resync |
| Cluster unregister | unregister task, cluster state | clusters, tasks | `platform.task.dispatch unregister` | task event local update |
| DR Stage 1 | namespace summary, resource summary, draft plan | inventory summary cache, protection plans | inventory summary; namespace detail on demand | cached by default; modal on demand |
| DR Stage 2 | storage repo, schedule, DR Config status | repositories, plans, tasks | `storage-sync`, `schedule-sync` | user save triggers; task event updates |
| DR Stage 3 | sync status, latest restore point, resource summary | tasks, restore_points, inventory summary | backup task, Velero event, inventory | active task first; local update |
| Restore Points | available restore points, size, namespace filter | restore_points, plans | no direct agent request | user refresh/filter; compensation if needed |
| Backup and Restore Tasks | task history, errors, restore point relation | tasks, task events, restore_points | task event | running state local update |
| Resource modal | one namespace resource detail | inventory detail cache | `platform.inventory.request scope=namespaceResources` | on modal open |

### 14.2 Cluster Registration and Cluster List

Page data:

- Cluster name, fingerprint, kubeVersion.
- Agent online state, agentVersion, lastHeartbeatAt.
- Velero installed/health state.
- nodeCount, namespaceCount, applicationCount.
- Latest inventory summary time.

Data sources:

- Register and reconnect come from `agent.register`.
- Online state comes from `agent.heartbeat`.
- Resource counts come from `agent.inventory.report scope=summary`, not
  heartbeat.

State rules:

```text
lastHeartbeatAt <= 90s: online
90s < lastHeartbeatAt <= 5min: degraded
lastHeartbeatAt > 5min: disconnected
```

Registration APIs handle:

- Create install token.
- Show token expiry.
- Revoke install token.
- Show registration failure reason.

Agent protocol handles:

- `agent.register` for first registration, restart, reconnect.
- `platform.register.accepted/rejected`.
- `agent.heartbeat`.

### 14.3 Cluster Unregister

Two unregister modes:

- `soft unregister`: platform disables or removes the cluster record; agent may
  be offline.
- `agent cleanup unregister`: when agent is online, dispatch an `unregister`
  task to clean cluster-side agent/Velero resources.

Page actions:

- User clicks unregister.
- Page shows confirmation options: delete Velero, delete agent namespace, keep
  history.
- Platform creates unregister task or performs soft unregister.

Rules:

- Online agent: prefer cleanup unregister.
- Offline agent: allow soft unregister, but clearly state cluster-side resources
  will not be cleaned automatically.
- Unregister should not delete historical tasks or restore points unless the
  user explicitly chooses history cleanup.
- After unregister, related protection plans should become `disabled` or
  non-executable.

### 14.4 DR Stage 1: Protected Object Selection

Page data:

- Source cluster list.
- Namespace list.
- Per-namespace resource summary.
- Per-namespace PVC summary.
- Namespace resource detail for the Resource modal.
- Protection Plan draft or existing configuration.

Rules:

- List summary comes from platform-cached `agent.inventory.report scope=summary`.
- Resource detail modal comes from
  `platform.inventory.request scope=namespaceResources`.
- Plan draft comes from platform protection plan store.
- If summary is missing or stale, platform may trigger
  `platform.inventory.request scope=summary`.
- Clicking Resource modal requests detail for one namespace.
- ConfigMap/Secret values are not displayed.
- Saving selection only creates/updates Protection Plan draft. It does not
  create Velero Backup or Schedule.

### 14.5 DR Stage 2: Storage, Policy, and DR Config

Page data:

- Storage Repository list and status.
- Protection Plan configuration.
- Schedule policy such as cron and retention policy.
- storage-sync task status.
- schedule-sync task status.
- DR Config status.

Platform APIs handle:

- Storage Repository CRUD.
- Storage Repository connection test.
- Protection Plan update.
- Schedule policy update.
- Retention policy update.

Agent communication:

- `storage-sync`: sync object storage config to clusters that need the repo.
- `schedule-sync`: create/update Velero Schedule on source cluster.

DR Config status:

```text
active = storage-sync succeeded + schedule-sync succeeded + plan config valid
storage_failed = storage-sync failed
schedule_failed = schedule-sync failed
configuring = storage-sync or schedule-sync is running
disabled = plan disabled or source cluster not executable
```

If the target cluster also needs the same object storage, target-cluster
storage-sync status must be recorded separately from source-cluster
schedule-sync.

### 14.6 DR Stage 3: Sync, Recovery, and Resource Column

Page data:

- DR Config per namespace.
- Current active backup task.
- Latest available restore point.
- Sync status and progress.
- Resource summary.
- More menu actions: View Restore Point, Restore, Drill, Takeover, etc.

Data sources:

- Resource column comes from inventory summary, not full detail.
- Sync running state comes from tasks.
- Latest restore point comes from restore_points.
- Scheduled backup state comes from backup tasks created/updated by
  `agent.velero.event`.

Sync status priority:

1. Active backup task exists: show running/syncing/progress.
2. Latest backup task succeeded: show Sync complete and link latest restore
   point.
3. Latest backup task failed: show Sync failed and error.
4. No active task but available restore point exists: show Last snapshot.
5. No restore point: show No snapshot yet.

Latest restore point rule:

```text
latestRestorePoint =
  max(completedAt)
  where sourceClusterId = plan.sourceClusterId
    and sourceNamespace = selected namespace
    and status = available
```

View Restore Point jump:

- Single selected namespace: go to Restore Points and set namespace multi-select
  filter to that namespace.
- Multiple selected namespaces: set namespace multi-select filter to all
  selected namespaces.
- Do not show an extra banner; the filter itself expresses the condition.

Resource modal rules:

- Resource column shows only icon + number summary.
- Clicking any resource icon opens the same namespace resource detail modal.
- If detail cache is missing or stale, platform triggers
  `platform.inventory.request scope=namespaceResources`.

### 14.7 Restore Points Page

Page data:

- `available` restore points.
- Namespace multi-select filter.
- Source cluster filter.
- Size and sizeStatus.
- Restore/drill/takeover entry points.

Rules:

- Cluster filter uses `sourceClusterId`.
- Namespace filter uses `sourceNamespace` or included namespaces.
- Default list shows only `status=available`.
- failed, deleted, and clearing do not appear in the default list.
- Size main field shows `totalBytes`.
- Tooltip shows `metadataBytes`, `volumeBytes`, `uploadedBytes`,
  `uploadedVolumeBytes`, and accuracy.
- `sizeStatus=collecting/partial` must be visible.
- `uploadedVolumeBytes` is estimated in the first version and must be labeled
  estimated in tooltip/details.
- Stage 3 jump writes namespace condition into the page filter, including
  multi-select; do not use a text banner as a fake filter.

### 14.8 Backup and Restore Tasks Page

Page data:

- task type.
- task status: `running/succeeded/failed`.
- source namespace, target namespace.
- restore point display.
- repository/storageLocation.
- startedAt, completedAt, duration.
- errorCode, message.
- size and sizeStatus.

Default time window:

- Default fixed to recent `7d`.
- User can change time range.
- Do not query all history by default.

Suggested columns:

- Task Type.
- Namespace.
- Operation Time.
- Restore Point.
- Repository.
- Duration.
- Result/Error.

Running update rules:

- On page open, query data for current filters.
- Background task events only update visible task status, progress, error, and
  completion time.
- Do not reset pagination, filters, sorting, or selected rows.
- User refresh re-queries the list.

Restore point relation:

- Successful backup task links to restore point.
- Failed backup task does not require a restore point.
- If the restore point has been cleaned, Task History shows cleared/deleted but
  does not hide the task.

### 14.9 Resource Detail Modal

Request payload:

```json
{
  "requestId": "req_ns_detail_001",
  "scope": "namespaceResources",
  "namespace": "demo-mysql",
  "includeDetails": true,
  "reason": "resource_modal_open"
}
```

Returned groups:

- `workloads`: Deployment, StatefulSet, DaemonSet, Pod summary.
- `network`: Service, Ingress, EndpointSlice count.
- `storage`: PVC, PV reference, StorageClass, requestedBytes, bound status.
- `config`: ConfigMap, Secret name, type, key count.
- `rbac`: ServiceAccount, Role, RoleBinding.
- `policy`: NetworkPolicy, PDB, HPA.
- `jobs`: Job, CronJob.

PVC fields:

- name.
- status.
- storageClass.
- requestedBytes.
- capacityBytes.
- volumeName.
- accessModes.

Sensitive information rules:

- Secret values are forbidden.
- ConfigMap values are forbidden by default.
- Pod logs are forbidden.
- Full Events are forbidden.
- Endpoint address details are forbidden by default.

Cache rules:

- Suggested namespace detail cache is `5min`.
- If cache is fresh, modal uses cache directly.
- If cache is stale or missing, trigger on-demand request.
- If request fails, the modal may show old cache with collection failure and
  collection time.

### 14.10 Page-Level Platform API Contract

This section does not fix final HTTP route names. It defines capabilities that
must exist between the UI and the platform. The UI calls platform APIs only; the
platform decides whether to read database/cache, trigger agent requests, or wait
for background events.

#### 14.10.1 Cluster Registration and Unregister APIs

Cluster registration page needs:

- Create install token.
- Query install token status and expiry.
- Revoke install token.
- Query registered cluster list.
- Query single cluster detail.

Cluster list API response shape:

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

Cluster unregister page needs:

- Query unregister impact, such as related plan, active task, and restore point
  counts.
- Create unregister task.
- Query unregister task status.
- Perform offline soft unregister.

Rules:

- Online cleanup must create an `unregister` task and dispatch it.
- Offline soft unregister must not pretend cluster-side resources were cleaned.
- Unregister operation must return a traceable operation/task id.

#### 14.10.2 DR Stage 1 API

Stage 1 needs:

- Query source clusters.
- Query namespace summary for one source cluster.
- Query or create plan draft.
- Save protected object selection.
- Query namespace detail when opening Resource modal.

Namespace summary response shape:

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

Rules:

- `status=Active` is Kubernetes namespace status. It may be useful in Stage 1,
  but should not consume key table columns in Stage 2 or Stage 3.
- Resource summary comes from `resourceSummary/pvcSummary`, not per-namespace
  detail.
- Saving protected object selection only changes plan draft.

#### 14.10.3 DR Stage 2 API

Stage 2 needs:

- Query plan config detail.
- Query Storage Repository list.
- Create/update/delete Storage Repository.
- Test Storage Repository connection.
- Save schedule policy.
- Save retention policy.
- Dispatch or redispatch `storage-sync`.
- Dispatch or redispatch `schedule-sync`.
- Query DR Config aggregate status.

DR Config response shape:

```json
{
  "planId": "plan_001",
  "sourceClusterId": "source_cluster_id",
  "sourceNamespace": "demo-mysql",
  "status": "active",
  "storage": {
    "repositoryId": "repo_001",
    "name": "my-minio",
    "sourceSyncStatus": "succeeded",
    "targetSyncStatus": "succeeded",
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

Rules:

- `DR Config` is configuration effectiveness, not latest backup success.
- Source-cluster `schedule-sync` and target-cluster `storage-sync` are recorded
  separately.
- Empty repository in Task History is a data gap or missing association. The UI
  should display `Not set` or backfill from plan/repository relation instead of
  a blank cell.

#### 14.10.4 DR Stage 3 API

Stage 3 needs:

- Query sync status list for each plan/namespace.
- Start single or batch manual backup.
- Query latest available restore point.
- Query active backup task.
- Create namespace multi-select filter when jumping to Restore Points.
- Query namespace detail when opening Resource modal.

Stage 3 list response shape:

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

Rules:

- After Start Sync, page first shows the active task created by the platform. It
  must not instantly show success.
- `sync.status=succeeded` must come from task terminal state plus restore point
  upsert completion, not HTTP task creation or intermediate Velero phase.
- Latest restore point is selected only from source-cluster `available` restore
  points.
- If backup task succeeded but size is still collecting, sync may be successful
  while restore point size shows `collecting/partial`.
- View Restore Point only sets Restore Points filters. It does not display an
  extra text banner.

#### 14.10.5 Restore Points API

Restore Points page needs:

- Query restore point list.
- Query namespace filter options with multi-select support.
- Query restore point detail.
- Start restore, drill, takeover.
- Query related task history.

List response shape:

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

Rules:

- Default `status=available`.
- Size main column shows only `totalBytes`; details are in tooltip/details.
- `unknown` is only temporary abnormal display. Target implementation should use
  `sizeStatus=collecting/partial/failed` to explain why.
- Restore Points page does not show task running state; Task History does.

#### 14.10.6 Backup and Restore Tasks API

Task History page needs:

- Query task list.
- Query task detail.
- Query error detail.
- Query related restore point.
- Filter by time range.
- Filter by task type, task status, namespace, repository, etc.

Default query:

- Default time window is recent `7d`.
- User can switch to `24h`, `7d`, `30d`, or custom range.
- Do not query all history by default.

List response shape:

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

Rules:

- First column is `Task Type`, showing user-understandable task type and icon.
- `cluster` is hidden by default; source/target cluster can be in detail or
  filters.
- Do not show long technical ids under backup type.
- Failed tasks must show user-readable error; technical details go into detail
  modal.
- UI status uses `Running`, not `Accepted/Pending`, for normal users.
- UI task status collapses to `running/succeeded/failed`.

### 14.11 Modal and Drawer Coverage

All modals/drawers read data from platform APIs. Opening a modal may trigger
platform on-demand refresh, but the frontend must not wait on the agent
directly.

| Modal / drawer | Trigger page | Required data | Agent trigger | Rules |
| --- | --- | --- | --- | --- |
| Resource Detail | DR Stage 1, Stage 3 | namespace resources | maybe `platform.inventory.request scope=namespaceResources` | group as workloads/network/storage/config/rbac/policy/jobs |
| Restore Point Detail | Restore Points, Task History | restore point, size, Velero backup summary | no by default | source-cluster restore points only |
| Task Detail | Task History, Stage 3 active task | task events, errors, Velero objects | no by default | show state timeline and errors |
| Restore confirmation | Restore Points, Stage 3 | restore point, target cluster, target namespace | no | creates restore task |
| Drill confirmation | Restore Points, Stage 3 | restore point, drill target namespace | no | creates drill task, no new restore point |
| Takeover confirmation | Restore Points, Stage 3 | restore point, target cluster, risk confirmation | no | requires second confirmation |
| Storage Test | Stage 2 | repository connection config | platform executes or dispatches test | clear success/failure reason |
| Unregister confirmation | Cluster list | affected plans/tasks/restore points | online cleanup dispatches unregister | offline only soft unregister |

Resource Detail display:

- Header shows namespace, collection time, total resources, total PVC requested
  capacity.
- Body groups resources by resource group. Do not mix everything into one
  unreadable table.
- Each group table has few clear fields, such as name, kind, status, age, and
  key spec.
- ConfigMap/Secret show key count and references only, not content.
- PVC capacity belongs in the storage group. Resource summary tooltip may also
  show PVC requested capacity.

Task Detail display:

- Title uses user-understandable name, for example `Backup demo-mysql`.
- Subtitle shows startedAt, duration, repository, restore point displayName.
- Timeline shows accepted, running, completed/failed.
- Error details are collapsed by default and show errorCode, message, Velero
  phase, and raw details when expanded.

Restore Point Detail display:

- Title uses namespace plus completion time.
- Main fields: source cluster, namespace, repository, completedAt, size, status.
- Velero backup name is secondary technical information, not the title.
- Size breakdown is in tooltip/details: metadata, volume, uploaded, accuracy.

### 14.12 Protocol Coverage Assessment and Additional Rules

The current target protocol can cover the required pages, but implementation
must provide these platform-side capabilities:

1. Platform API layer must return page aggregates. The frontend must not join
   tasks, restore points, and inventory by itself.
2. Inventory summary/detail must be stored or cached with collectedAt/hash so
   pages can judge freshness.
3. Task History must have a task event table or equivalent event log for detail
   timelines.
4. Restore Point must store size breakdown, sizeStatus, repository,
   sourceNamespace, and sourceClusterId.
5. DR Stage 3 sync status must be generated by backend aggregation, not inferred
   by the frontend from multiple endpoints.
6. Platform must provide visible-row local update capability, such as SSE,
   WebSocket, or polling current row status. It must not auto-refresh the whole
   table.
7. Platform must ack terminal events only after persistence; otherwise the agent
   may believe the final result was reliably received when it was not.
8. On first scheduled backup event, platform must idempotently create task and
   return `taskId/commandId` in ack.
9. Target-cluster Backup CRs must be filtered at both agent and platform layers.
10. Restore/drill/takeover operations must use an `available` restore point as
    input, not raw Velero backup name.

If these capabilities are missing, typical symptoms are:

- Clicking sync instantly shows success, then later shows progress.
- Restore point first shows old data, then jumps to new data seconds later.
- Pagination returns to page 1 after a while.
- Target-cluster same-name Backup CR causes restore point reduction, disorder,
  or false clearing.
- Tasks stay pending/running/finalizing.
- Resource modal is clipped, missing fields, or confusing because it lacks a
  stable detail schema.

### 14.13 Page Refresh and Local Update Rules

- On page open, fetch current platform data.
- Pagination, filtering, and sorting operate on the current query condition and
  must not trigger global auto-refresh.
- Background events update only visible row state.
- Except user refresh, initial page load, and filter change, lists must not
  re-query the whole table.
- Agent inventory summary background updates must not reset page pagination or
  selection.
- Running task status should update promptly, but without refreshing the whole
  table.
- After reliable terminal event delivery, platform updates tasks and restore
  points; pages reflect it through local state update.

## 15. Implementation Checklist

Every new task type, state transition, page data source, or agent message must
update:

- `platform/backend/internal/protocol/messages.go`
- backend task status handling.
- agent executor and reporter.
- task event reason mapping.
- restore point creation rules.
- inventory summary/detail schema.
- frontend UI state mapping.
- platform API filtering and refresh rules.
- this document.
