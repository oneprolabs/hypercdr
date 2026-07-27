# Agent WebSocket Protocol

The platform-agent protocol uses JSON messages in the first phase. Every message is wrapped in a stable envelope so the payload can later move to protobuf without changing routing semantics.

## Transport

- URL: `wss://<platform-host>/ws/agent`
- Agent connects outbound to the platform.
- Platform never initiates direct network access to the managed cluster.
- Agent reconnects with exponential backoff.
- Both sides support ping/pong or protocol heartbeat.

## Message Envelope

```json
{
  "version": "v1",
  "messageId": "018f4b67-df64-7a70-a0fb-c4b28a19c9a1",
  "type": "agent.heartbeat",
  "tenantId": "tenant-id",
  "clusterId": "cluster-id",
  "agentId": "agent-pod-uid",
  "timestamp": "2026-06-04T09:30:00Z",
  "correlationId": "task-or-command-id",
  "payload": {}
}
```

Field rules:

- `messageId` is unique per message.
- `correlationId` links task progress, events, and command acknowledgements.
- `clusterId` is empty only before registration is accepted.
- `agentId` is stable for the running agent instance.
- `payload` is typed by `type`.

## Agent to Platform Messages

### agent.register

Sent after the WebSocket connection is opened and before normal operations.

```json
{
  "type": "agent.register",
  "payload": {
    "installToken": "one-time-token",
    "cluster": {
      "fingerprint": "sha256-value",
      "name": "production-cluster-01",
      "kubeVersion": "v1.28.4",
      "nodeCount": 18,
      "namespaceCount": 42
    },
    "agent": {
      "version": "v0.1.0",
      "namespace": "hypercdr-agent",
      "podName": "comm-agent-xxxx"
    },
    "velero": {
      "installed": true,
      "version": "v1.17.1",
      "status": "healthy"
    }
  }
}
```

### agent.heartbeat

Heartbeat is intentionally lightweight. It carries cluster summary and an inventory hash. If the hash changes, the agent should send `agent.inventory.report` after the heartbeat or when requested by the platform.

```json
{
  "type": "agent.heartbeat",
  "payload": {
    "status": "healthy",
    "agentVersion": "v0.1.0",
    "kubeVersion": "v1.28.4",
    "veleroStatus": "healthy",
    "nodeCount": 18,
    "namespaceCount": 42,
    "applicationCount": 42,
    "activeTasks": 1,
    "inventoryHash": "sha256-inventory-summary",
    "inventoryChanged": false
  }
}
```

### agent.inventory.report

Sent after registration, when `inventoryHash` changes, or when the platform sends `platform.inventory.request`.

```json
{
  "type": "agent.inventory.report",
  "payload": {
    "full": true,
    "collectedAt": "2026-06-04T09:31:00Z",
    "cluster": {
      "kubeVersion": "v1.28.4",
      "nodeCount": 18,
      "namespaceCount": 42
    },
    "nodes": [
      {
        "name": "node-1",
        "role": "worker",
        "status": "ready",
        "kubeletVersion": "v1.28.4",
        "capacity": {
          "cpu": "8",
          "memory": "32Gi"
        }
      }
    ],
    "applications": [
      {
        "namespace": "frontend-service",
        "status": "running",
        "labels": {
          "app": "frontend"
        },
        "resources": {
          "deployments": 3,
          "statefulsets": 0,
          "daemonsets": 0,
          "services": 2,
          "ingresses": 1,
          "configmaps": 4,
          "secrets": 2,
          "pvcs": 1,
          "pvCapacityBytes": 8589934592
        }
      }
    ],
    "velero": {
      "backupStorageLocations": [],
      "volumeSnapshotLocations": [],
      "recentBackups": [],
      "recentRestores": []
    }
  }
}
```

### agent.task.accepted

```json
{
  "type": "agent.task.accepted",
  "correlationId": "command-id",
  "payload": {
    "taskId": "task-id",
    "commandId": "command-id",
    "acceptedAt": "2026-06-04T09:32:00Z"
  }
}
```

### agent.task.progress

```json
{
  "type": "agent.task.progress",
  "correlationId": "command-id",
  "payload": {
    "taskId": "task-id",
    "status": "running",
    "progress": 45,
    "message": "Velero backup is running",
    "velero": {
      "kind": "Backup",
      "name": "hcdr-prod-frontend-202606040932-ab12"
    }
  }
}
```

### agent.task.completed

```json
{
  "type": "agent.task.completed",
  "correlationId": "command-id",
  "payload": {
    "taskId": "task-id",
    "status": "succeeded",
    "progress": 100,
    "restorePoint": {
      "veleroBackupName": "hcdr-prod-frontend-202606040932-ab12",
      "type": "snapshot",
      "sizeBytes": 123456789,
      "completedAt": "2026-06-04T09:35:00Z",
      "expiresAt": "2026-07-04T09:35:00Z"
    }
  }
}
```

### agent.task.failed

```json
{
  "type": "agent.task.failed",
  "correlationId": "command-id",
  "payload": {
    "taskId": "task-id",
    "errorCode": "VELERO_BACKUP_FAILED",
    "message": "Backup phase is Failed",
    "details": {
      "veleroPhase": "Failed"
    }
  }
}
```

### agent.velero.event

```json
{
  "type": "agent.velero.event",
  "correlationId": "command-id",
  "payload": {
    "taskId": "task-id",
    "kind": "Backup",
    "name": "hcdr-prod-frontend-202606040932-ab12",
    "phase": "InProgress",
    "message": "Waiting for pod volume backup",
    "raw": {}
  }
}
```

## Platform to Agent Messages

### platform.register.accepted

```json
{
  "type": "platform.register.accepted",
  "payload": {
    "tenantId": "tenant-id",
    "clusterId": "cluster-id",
    "clusterName": "Production Cluster 01",
    "agentCredential": "opaque-token",
    "heartbeatIntervalSeconds": 30,
    "inventoryIntervalSeconds": 300
  }
}
```

### platform.register.rejected

```json
{
  "type": "platform.register.rejected",
  "payload": {
    "reason": "TOKEN_EXPIRED",
    "message": "Install token is expired"
  }
}
```

### platform.task.dispatch

```json
{
  "type": "platform.task.dispatch",
  "correlationId": "command-id",
  "payload": {
    "taskId": "task-id",
    "commandId": "command-id",
    "type": "backup",
    "deadline": "2026-06-04T10:00:00Z",
    "backup": {
      "appNamespace": "frontend-service",
      "scope": "namespace",
      "labelSelector": "",
      "storageRepo": "AWS-West-S3",
      "ttl": "720h",
      "includeClusterResources": false,
      "excludeResources": [],
      "hooks": {
        "pre": [],
        "post": []
      }
    }
  }
}
```

### platform.task.cancel

```json
{
  "type": "platform.task.cancel",
  "correlationId": "command-id",
  "payload": {
    "taskId": "task-id",
    "commandId": "command-id",
    "reason": "operator canceled"
  }
}
```

### platform.inventory.request

```json
{
  "type": "platform.inventory.request",
  "payload": {
    "full": true,
    "reason": "manual-refresh"
  }
}
```

## Backup Command Payload

```json
{
  "type": "backup",
  "backup": {
    "appNamespace": "frontend-service",
    "scope": "namespace",
    "labelSelector": "",
    "storageRepo": "AWS-West-S3",
    "ttl": "720h",
    "includeClusterResources": false,
    "excludeResources": [
      {
        "group": "apps",
        "resource": "deployments",
        "name": "debug-only",
        "version": "v1",
        "labels": "tier=debug"
      }
    ],
    "hooks": {
      "pre": [
        {
          "name": "pre-backup-hook.sh",
          "content": "#!/bin/sh\nset -e\n",
          "entry": true
        }
      ],
      "post": []
    }
  }
}
```

## Restore Command Payload

```json
{
  "type": "restore_drill",
    "restore": {
    "restorePointId": "restore-point-id",
    "veleroBackupName": "hcdr-prod-frontend-202606040932-ab12",
    "sourceNamespace": "frontend-service",
    "targetNamespace": "frontend-service-drill",
    "targetMode": "sandbox",
    "restoreMode": "full",
    "artifactMode": "all",
    "conflictPolicy": "skip",
    "includeClusterScoped": false,
    "useTransforms": true,
    "transformPreset": "drill",
    "storageProfileMode": "original",
    "alternateProfileId": ""
  }
}
```

For cross-cluster drill or takeover, both the source and target clusters must be able to access the selected backup storage repository. The platform should validate this before dispatching the restore command.

## Delivery Semantics

- Platform persists a task before dispatch.
- Platform generates one command per dispatch attempt.
- Agent persists or remembers accepted `commandId` values to avoid duplicate execution.
- Agent should send `agent.task.accepted` before creating Velero CRDs.
- If the WebSocket disconnects, the platform retries only tasks that are not terminal.
- Terminal task states are `succeeded`, `failed`, `canceled`, and `timeout`.

## Error Codes

Initial standard error codes:

- `AUTH_TOKEN_INVALID`
- `AUTH_TOKEN_EXPIRED`
- `CLUSTER_ALREADY_REGISTERED`
- `TASK_UNSUPPORTED`
- `KUBE_API_ERROR`
- `VELERO_NOT_READY`
- `VELERO_BACKUP_FAILED`
- `VELERO_RESTORE_FAILED`
- `STORAGE_LOCATION_INVALID`
- `COMMAND_TIMEOUT`
