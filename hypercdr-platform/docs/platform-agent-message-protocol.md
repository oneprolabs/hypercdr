# Platform-Agent Message Protocol

Last updated: 2026-06-11

This document defines the first-phase message contract between the HyperCDR platform and the cluster-side `comm-agent`.

## Design Rules

- Every WebSocket frame uses the same envelope.
- Platform-to-agent commands are modeled as tasks.
- Agent-to-platform messages are modeled as reports, acknowledgements, progress, terminal results, or events.
- Every task has one `taskId`, one `commandId`, one `type`, one `deadline`, and exactly one typed command object.
- Do not add ad hoc command fields to the envelope. Add a typed command under `payload` instead.
- Do not use unstructured maps for primary command intent when a typed command exists.
- `messageId` identifies one WebSocket message. `correlationId` is the related `commandId` for task messages.
- Task terminal states are `succeeded` and `failed`. A terminal task must not be redispatched.

## Envelope

```json
{
  "version": "v1",
  "messageId": "msg_public_id",
  "type": "platform.task.dispatch",
  "tenantId": "00000000-0000-0000-0000-000000000001",
  "clusterId": "cluster_public_id",
  "agentId": "agent_public_id",
  "timestamp": "2026-06-11T00:00:00Z",
  "correlationId": "command_public_id",
  "payload": {}
}
```

Required fields:

- `version`
- `messageId`
- `type`
- `timestamp`
- `payload`

Task-related messages must include `clusterId` and `correlationId`.

## Platform To Agent

### `platform.task.dispatch`

The platform uses this message for all executable work.

```json
{
  "taskId": "task_public_id",
  "commandId": "command_public_id",
  "type": "backup",
  "deadline": "2026-06-11T00:30:00Z",
  "backup": {}
}
```

Supported task types:

- `storage-sync`: write object storage credentials/configuration and apply Velero `BackupStorageLocation`.
- `backup`: create a Velero `Backup`.
- `restore`: create a Velero `Restore`.
- `drill`: create a Velero `Restore` for recovery drill validation.
- `takeover`: create a Velero `Restore` for takeover flow.
- `unregister`: agent self-uninstall and platform cleanup after success.

Exactly one of these command fields must be present:

- `storageSync`
- `backup`
- `restore`
- `unregister`

For `drill` and `takeover`, use the `restore` command body with the task `type` carrying the workflow intent.

### `platform.task.cancel`

Reserved for later. First phase does not depend on cancellation.

### `platform.inventory.request`

Reserved for on-demand inventory refresh. First phase primarily relies on periodic agent inventory reports.

## Agent To Platform

### `agent.register`

Sent when the agent starts or reconnects.

Initial registration uses `installToken`. Reconnect uses `agentCredential`.

Payload contains:

- `cluster`: cluster fingerprint/name/version/count summary.
- `agent`: agent version, namespace, pod name.
- `velero`: installed/version/status summary.

### `agent.heartbeat`

Sent periodically by the agent.

Payload contains:

- agent and cluster health fields.
- `nodeCount`, `namespaceCount`, `applicationCount`.
- `activeTasks`.
- `inventoryHash` and `inventoryChanged`.

### `agent.inventory.report`

Sent after registration and whenever inventory changes.

Payload contains:

- `cluster` summary.
- `nodes`.
- namespace-level `applications`.
- Velero inventory: `BackupStorageLocation`, `VolumeSnapshotLocation`, recent backups, recent restores.

### Task Lifecycle Messages

After receiving `platform.task.dispatch`, the agent reports lifecycle messages in this order:

1. `agent.task.accepted`
2. zero or more `agent.task.progress`
3. exactly one terminal message:
   - `agent.task.completed`
   - `agent.task.failed`

`agent.task.accepted` payload:

```json
{
  "taskId": "task_public_id",
  "commandId": "command_public_id",
  "acceptedAt": "2026-06-11T00:00:01Z"
}
```

`agent.task.progress` payload:

```json
{
  "taskId": "task_public_id",
  "status": "running",
  "progress": 50,
  "message": "velero backup is running",
  "velero": {
    "kind": "Backup",
    "name": "backup-name",
    "phase": "InProgress"
  }
}
```

`agent.task.completed` payload:

```json
{
  "taskId": "task_public_id",
  "status": "succeeded",
  "progress": 100,
  "message": "velero backup completed",
  "velero": {
    "kind": "Backup",
    "name": "backup-name",
    "phase": "Completed"
  }
}
```

`agent.task.failed` payload:

```json
{
  "taskId": "task_public_id",
  "status": "failed",
  "errorCode": "VELERO_BACKUP_FAILED",
  "message": "velero backup failed",
  "details": {
    "phase": "Failed",
    "errors": 1
  }
}
```

### `agent.velero.event`

Reserved for asynchronous Velero events that are not tied to a single progress update.

## Command Templates

### Storage Sync

```json
{
  "taskId": "task_id",
  "commandId": "command_id",
  "type": "storage-sync",
  "deadline": "2026-06-11T00:10:00Z",
  "storageSync": {
    "repositoryId": "repo_id",
    "name": "minio-primary",
    "type": "S3",
    "endpoint": "http://minio.example:9000",
    "bucket": "hypercdr",
    "region": "us-east-1",
    "tlsEnabled": false,
    "secretRef": "hypercdr-storage-minio-primary",
    "credentials": {
      "accessKey": "provided only in dispatch",
      "secretKey": "provided only in dispatch"
    },
    "config": {}
  }
}
```

### Backup

```json
{
  "taskId": "task_id",
  "commandId": "command_id",
  "type": "backup",
  "deadline": "2026-06-11T00:30:00Z",
  "backup": {
    "appNamespace": "demo",
    "scope": "namespace",
    "labelSelector": "",
    "storageRepo": "minio-primary",
    "ttl": "720h",
    "includeClusterResources": false,
    "excludeResources": [],
    "hooks": {
      "pre": [],
      "post": []
    }
  }
}
```

First-phase backup must enable Velero filesystem backup for PVC data and avoid local snapshot dependency.

### Restore / Drill / Takeover

```json
{
  "taskId": "task_id",
  "commandId": "command_id",
  "type": "restore",
  "deadline": "2026-06-11T00:30:00Z",
  "restore": {
    "restorePointId": "rp_id",
    "veleroBackupName": "backup-name",
    "sourceNamespace": "demo",
    "targetNamespace": "demo-drill",
    "targetMode": "new-namespace",
    "restoreMode": "full",
    "artifactMode": "reuse",
    "conflictPolicy": "skip-existing",
    "includeClusterScoped": false,
    "useTransforms": true,
    "transformPreset": "namespace-remap",
    "storageProfileMode": "same",
    "alternateProfileId": ""
  }
}
```

### Unregister

```json
{
  "taskId": "task_id",
  "commandId": "command_id",
  "type": "unregister",
  "deadline": "2026-06-11T00:10:00Z",
  "unregister": {
    "clusterId": "cluster_id",
    "namespace": "hypercdr-agent",
    "deleteVelero": true,
    "deleteNamespace": true,
    "reason": "requested from platform cluster page"
  }
}
```

Normal unregister flow:

1. User requests unregister in the platform UI.
2. Platform creates an `unregister` task.
3. Platform dispatches the task to the connected agent, or keeps it queued if the agent is offline.
4. Agent accepts the task.
5. Agent reports completion after acknowledging self-uninstall intent.
6. Agent deletes the HyperCDR namespace and cluster-level RBAC.
7. Platform removes the cluster record only after `agent.task.completed`.

`DELETE /api/v1/clusters/{id}?force=true` is platform-only cleanup and must be treated as an operational fallback, not the default UI path.
