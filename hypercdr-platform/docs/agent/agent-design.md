# Agent Design

The cluster-side agent consists of the official Velero deployment and a self-developed `comm-agent`. The agent is deployed into an independent namespace, recommended as `hypercdr-agent`.

## Responsibilities

The `comm-agent` owns:

- Platform WebSocket connection.
- Registration with install token.
- Heartbeat.
- Cluster inventory collection.
- Task command receiving and deduplication.
- Velero CRD creation and watching.
- Task progress, event, and log reporting.

Velero owns:

- Backup execution.
- Restore execution.
- Backup repository interaction.
- Snapshot and pod volume backup mechanisms.

## Recommended Implementation Language

Use Go for `comm-agent`.

Reasons:

- Kubernetes client-go is mature.
- Velero types can be imported when compatible.
- Single static binary is easy to ship in a container.
- Agent code and Kubernetes controllers are naturally Go-oriented.

## Directory Layout

```text
agent/comm-agent/
  go.mod
  cmd/comm-agent/main.go
  internal/config/
  internal/wsclient/
  internal/auth/
  internal/inventory/
  internal/executor/
  internal/velero/
  internal/kube/
  internal/store/
  internal/logging/
  internal/metrics/
  pkg/protocol/
```

## Configuration

The agent reads configuration from environment variables and mounted Secret/ConfigMap:

- `HCDR_PLATFORM_ENDPOINT`
- `HCDR_INSTALL_TOKEN`
- `HCDR_CLUSTER_ID`
- `HCDR_AGENT_ID`
- `HCDR_AGENT_NAMESPACE`
- `HCDR_HEARTBEAT_INTERVAL`
- `HCDR_INVENTORY_INTERVAL`
- `HCDR_LOG_LEVEL`

Before registration, `HCDR_INSTALL_TOKEN` is required. After registration, the platform returns a cluster credential. That credential should be stored in a Kubernetes Secret and used for subsequent reconnects. The one-time install token must not be reused after successful registration.

## Registration Flow

1. Agent starts.
2. Agent loads kube in-cluster config.
3. Agent computes cluster fingerprint.
4. Agent connects to WebSocket endpoint.
5. Agent sends `agent.register`.
6. Platform validates install token.
7. Platform replies with `platform.register.accepted`.
8. Agent stores `clusterId` and credential.
9. Agent sends full inventory.

Cluster fingerprint should be stable enough to detect duplicate installs. Recommended inputs:

- Kubernetes namespace UID for `kube-system`.
- API server version.
- Optional platform-generated install token ID.

## Inventory Collector

First phase inventory treats Kubernetes namespaces as applications. A protection plan may optionally use a Kubernetes label selector to narrow which resources inside the namespace are included.

Collect:

- Cluster version.
- Nodes and readiness.
- Namespaces.
- Deployments.
- StatefulSets.
- DaemonSets.
- Jobs and CronJobs.
- Services.
- Ingresses.
- ConfigMaps count only.
- Secrets count only.
- PVC/PV count and capacity.
- StorageClasses.
- Velero BackupStorageLocations.
- Velero VolumeSnapshotLocations.
- Recent Velero Backups and Restores.

Do not collect:

- Secret data.
- ConfigMap content by default.
- Pod logs by default.
- Arbitrary CR content outside the HyperCDR and Velero integration scope.

## Task Executor

The executor receives platform commands and dispatches to task-specific handlers:

- `inventory_scan`
- `backup`
- `restore_drill`
- `takeover`
- `validate_storage`
- `agent_upgrade`
- `unregister`

Each handler must:

1. Check `commandId` deduplication.
2. Send `agent.task.accepted`.
3. Execute Kubernetes/Velero operation.
4. Watch status.
5. Send progress and Velero events.
6. Send completed or failed.

## Velero Backup Mapping

For a namespace protection plan, create a Velero `Backup` CR:

```yaml
apiVersion: velero.io/v1
kind: Backup
metadata:
  name: hcdr-prod-frontend-202606040932-ab12
  namespace: hypercdr-agent
  labels:
    hypercdr.io/task-id: task-id
    hypercdr.io/protection-plan-id: plan-id
spec:
  includedNamespaces:
    - frontend-service
  storageLocation: AWS-West-S3
  ttl: 720h
  includeClusterResources: false
```

For label scope, set `labelSelector`.

For stateless scope, exclude persistent volume resources or disable volume backup behavior according to the chosen Velero mode.

Hooks from the platform should be translated to Velero-supported hooks. If hooks are complex scripts, the first phase can store them as ConfigMaps and mount them to hook execution containers only after the execution model is validated.

## Velero Restore Mapping

For drill, create a Velero `Restore` CR with namespace mapping:

```yaml
apiVersion: velero.io/v1
kind: Restore
metadata:
  name: hcdr-drill-frontend-202606040945-cd34
  namespace: hypercdr-agent
  labels:
    hypercdr.io/task-id: task-id
spec:
  backupName: hcdr-prod-frontend-202606040932-ab12
  namespaceMapping:
    frontend-service: frontend-service-drill
  existingResourcePolicy: none
```

For takeover, the restore target may be a different cluster. In that case the task must be dispatched to the target cluster agent. The source and target clusters must both be able to access the selected backup storage repository.

## Local Deduplication Store

The agent should remember recently accepted command IDs and reliable terminal
events. The state store must survive process restart and normal pod replacement
so task terminal events can be resent after reconnect.

Recommended first implementation:

- Local JSON files under `HCDR_AGENT_STATE_DIR`, mounted from the
  `hypercdr-agent-state` PVC.
- In-memory cache only as an optimization.
- Do not use `emptyDir` for reliable outbox or task ledger data, because it is
  lost when the pod is replaced.

## RBAC

The agent and Velero need enough permissions to:

- Read cluster inventory.
- Create and watch Velero resources.
- Create restore resources during recovery.
- Read PVC/PV and storage classes.

Use least privilege where possible, but first phase may need broad read access and Velero-required restore privileges. Separate ServiceAccounts are recommended:

- `hypercdr-comm-agent`
- `velero`

## Health and Metrics

Expose:

- `/healthz`
- `/readyz`
- `/metrics`

Metrics:

- WebSocket connected state.
- Last heartbeat timestamp.
- Inventory collection duration.
- Active task count.
- Task success/failure counters.
- Velero operation duration.

## Installer Design

Installer responsibilities:

1. Validate `kubectl` and cluster access.
2. Create namespace.
3. Install Velero CRDs and deployment.
4. Create comm-agent Secret and Deployment.
5. Wait for comm-agent pod readiness.
6. Print registration status hints.

The installer must be idempotent. Re-running the script should update existing manifests instead of creating duplicates.

The installer can run on any node or management machine with sufficient Kubernetes permissions. It does not need to run on a Kubernetes master node.

## Failure Handling

- If platform is unreachable, agent keeps retrying.
- If registration is rejected, agent exits or backs off with clear logs.
- If Velero is not ready, backup/restore tasks fail with `VELERO_NOT_READY`.
- If Kubernetes API returns conflict, agent retries with bounded backoff.
- If a Velero Backup or Restore enters Failed phase, agent reports `VELERO_BACKUP_FAILED` or `VELERO_RESTORE_FAILED`.
