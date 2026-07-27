# HyperCDR Architecture Overview

## Goal

HyperCDR is a centralized Kubernetes disaster recovery platform. It connects many Kubernetes clusters, manages application protection policies, creates restore points, and drives drill or takeover recovery workflows.

The platform does not access managed Kubernetes API servers directly. Each managed cluster runs a cluster-side agent in an independent namespace. The agent keeps an outbound WebSocket connection to the platform, reports cluster inventory and health, receives tasks, and drives Velero through Kubernetes CRDs.

## First Phase Scope

- Platform UI and REST API.
- Platform WebSocket server for agent connections.
- PostgreSQL as the first persistent database.
- Task engine and scheduler for backup and recovery orchestration.
- Cluster-side `comm-agent`.
- Official Velero deployed with the agent.
- Application model based on Kubernetes namespace. Label selectors can narrow the protected resources inside a namespace.
- Backup restore points created by Velero backup tasks.
- Recovery drill into sandbox namespaces.
- Takeover into a target cluster or target namespace.

Out of scope for the first phase:

- Direct platform access to managed Kubernetes API servers.
- Modifying Velero source code.
- Multi-cluster active-active replication.
- Full application topology discovery beyond namespace and labels.
- Enterprise SSO and complex RBAC beyond tenant/admin/operator roles.

## System Components

```text
Browser
  -> Platform Frontend
  -> Platform REST API
  -> PostgreSQL

Platform Scheduler
  -> PostgreSQL tasks
  -> Task Engine
  -> WebSocket Server
  -> Agent session

Managed Kubernetes Cluster
  -> hypercdr-agent namespace
      -> comm-agent
          -> Kubernetes API
          -> Velero CRDs
      -> Velero
          -> BackupStorageLocation
          -> Object storage / snapshots
```

## Platform Modules

### Frontend

The frontend should start from the existing prototype behavior:

- Dashboard.
- Clusters.
- Application DR.
- Storage repositories.
- Policies.
- Restore points.
- Operations.
- Tags.
- Alerts.
- Settings and tenants.

The real frontend should not keep business state only in React memory. It must use REST APIs and subscribe to task or cluster updates through either WebSocket/SSE or polling.

### REST API

The REST API owns user-facing configuration and query flows:

- Cluster list, detail, unregister, upgrade command creation.
- Agent install token and install command creation.
- Application inventory query.
- Storage repository CRUD and validation.
- Policy CRUD.
- Protection plan CRUD.
- Backup, drill, takeover task creation.
- Restore point query.
- Operation and audit query.

### WebSocket Server

The WebSocket server is only for platform-agent communication:

- Agent registration.
- Authentication and session tracking.
- Heartbeat.
- Inventory report ingestion.
- Task dispatch.
- Task progress and event ingestion.
- Agent upgrade or config update commands.

### Task Engine

The task engine owns task state transitions and command generation.

It reads queued tasks from PostgreSQL, validates required resources, builds agent commands, dispatches them through the WebSocket server, and records task events. It must be idempotent because network connections can drop after a command is accepted.

### Scheduler

The scheduler converts enabled protection policies into backup tasks. It should not execute Velero operations directly. It only creates task records and lets the task engine dispatch them.

### PostgreSQL

PostgreSQL is the source of truth for:

- Tenants and users.
- Clusters and agent sessions.
- Application inventory snapshots.
- Storage repositories.
- Policies and protection plans.
- Tasks and task events.
- Restore points.
- Audit logs.

## Agent Modules

### comm-agent

The `comm-agent` runs in each managed cluster. It is responsible for:

- Connecting to the platform WebSocket server.
- Registering with an install token.
- Sending heartbeat messages.
- Collecting cluster and namespace inventory.
- Receiving task commands.
- Creating and watching Velero CRDs.
- Reporting task status, events, logs, and restore point metadata.

### Velero

Velero is deployed as the official stable release. HyperCDR does not modify Velero source code. The agent integrates with Velero by creating and watching CRDs such as:

- `BackupStorageLocation`.
- `VolumeSnapshotLocation`.
- `Backup`.
- `Restore`.
- `Schedule` only if needed later; first phase scheduling should stay in the platform.

### Installer

The installer is generated from the platform cluster registration flow. It can run on any node or management machine that has enough Kubernetes permissions through `kubectl` or kubeconfig. It does not have to run on a master node.

A typical command:

```bash
curl -sSL https://platform.example.com/install.sh | bash -s -- \
  --token <install-token> \
  --endpoint wss://platform.example.com/ws/agent
```

The installer creates:

- `hypercdr-agent` namespace.
- ServiceAccount, Role/ClusterRole, and bindings.
- Secret containing bootstrap token and platform endpoint.
- Velero deployment and CRDs.
- comm-agent deployment.

## Core Workflows

### Cluster Registration

1. Operator clicks "Add Cluster" in the platform.
2. Platform creates a one-time install token.
3. UI shows an install command.
4. Operator runs the command on a node with Kubernetes admin access.
5. Installer deploys Velero and comm-agent.
6. comm-agent connects to the platform and sends `agent.register`.
7. Platform validates the token and creates or updates the cluster record.
8. Agent sends full inventory.
9. UI shows the cluster card as connected.
10. After registration succeeds, the agent persists the returned cluster credential and does not use the one-time install token again.

### Inventory and Heartbeat

1. Agent sends heartbeat every 15-30 seconds.
2. Heartbeat contains only summary information, such as agent status, Velero status, Kubernetes version, node count, namespace count, active task count, and inventory hash.
3. Agent sends full inventory after registration.
4. Agent sends a new full inventory when the inventory hash changes or when the platform explicitly requests it.
5. Platform updates cluster health, application list, Velero health, and timestamps.

### Backup

1. User creates a protection plan for a namespace application.
2. Scheduler creates a backup task according to the policy.
3. Task engine dispatches the task to the source cluster agent.
4. Agent creates a Velero `Backup` CR.
5. Agent watches backup status and reports progress.
6. Platform marks the task succeeded or failed.
7. Successful backup creates a restore point.

### Drill

1. User selects a restore point and starts a drill.
2. Platform creates a `restore_drill` task.
3. Agent creates a Velero `Restore` CR with namespace mapping to a sandbox namespace.
4. Agent reports progress.
5. Platform records operation history and validation state.

### Takeover

1. User selects a restore point and starts takeover.
2. Platform validates target cluster and target namespace.
3. Platform validates that the source and target clusters can both access the selected object storage repository.
4. Target cluster agent creates a Velero `Restore` CR.
5. Platform tracks progress and records takeover result.
6. Network routing or DNS cutover remains an integration point for later phases.

## Reliability Rules

- Every command has a `commandId` and `taskId`.
- Agent must deduplicate commands by `commandId`.
- Platform must tolerate reconnects and dispatch retries.
- Task state transitions must be monotonic.
- Agent should report Velero events, not hide them.
- Platform should mark a cluster offline if no heartbeat is received within a configured timeout.

## Security Rules

- Install tokens are one-time and time-limited.
- Agent receives a long-lived cluster credential only after successful registration.
- Agent credential rotation should be supported by protocol even if implemented later.
- Object storage secrets should be stored as Kubernetes Secrets in the agent namespace and encrypted or referenced in PostgreSQL on the platform side.
- Secret contents from managed clusters must not be collected in inventory.
