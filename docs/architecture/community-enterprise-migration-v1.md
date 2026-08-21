# Community to Enterprise migration protocol v1

## Product contract

Enterprise initiates migration from an already installed, licensed control
plane. Community grants a single-use, 30-minute authorization. The source and
target must share the exact Community baseline and the Agent must advertise the
handover capability. Migration never installs a second Agent.

Community remains the sole writer until handover starts. Enterprise remains
read-only for imported resources until a system administrator explicitly
commits. Commit is the irreversible boundary. Community is retained read-only
for seven days and is never automatically deleted.

## State machine

```text
draft -> prechecking -> backing-up -> ready -> frozen
      -> exporting -> importing -> reconciling
      -> handover-preparing -> handover-connecting -> verifying
      -> awaiting-tenant-admin -> ready-to-commit -> committed

Any pre-commit failure -> rolling-back -> rolled-back
Rollback failure       -> rollback-failed (manual recovery required)
```

Only one migration may be active on either control plane. Every transition is
persisted with an event, actor, UTC timestamp, error code, retryability, source
and target instance IDs, and an idempotency key.

## Authorization and transport

- Community stores only the SHA-256 hash of the single-use authorization.
- Establishing a session consumes the authorization and binds it to the target
  Enterprise instance, tenant draft, protocol version, and target ephemeral
  encryption key.
- All calls use TLS. Sensitive settings are additionally envelope-encrypted:
  Community decrypts its at-rest value in memory, encrypts it to the target
  migration key, and Enterprise immediately re-encrypts it with its own
  application secret.
- Passwords, session tokens, password-reset tokens, Agent credentials, install
  tokens, and Enterprise global settings are never exported.

## Data scope

The target creates one new tenant and one new user with Tenant Administrator
and DR Administrator roles. The source `admin` remains only as historical actor
metadata. Imported tenant data includes clusters, nodes, applications, tags,
storage repositories and encrypted credentials, bindings, policies, protection
plans and schedules, tasks and events, restore points and content indexes,
resource scopes, and tenant-relevant audit history. UUIDs are preserved unless
a precheck detects a target collision, in which case migration stops.

SMTP and other global settings are comparison-only and require an explicit
system-administrator selection; License, global security, roles, existing
users, image sources, and target settings are never overwritten.

## Freeze, backup, and reconciliation

Migration is blocked while backup, restore, drill, delete, cleanup, upgrade, or
repository-maintenance work is active. Community then disables scheduling and
business mutations, creates a mandatory database/configuration backup, and
records table counts, relationship hashes, cluster UID, namespace UID, PVC UID,
BSL, repository, and Agent ownership evidence.

Enterprise imports into migration-scoped staging. Existing tenants remain
available. Reconciliation compares exact counts, stable IDs, foreign-key
relationships, credential decryptability, Agent/data-plane state, and sampled
historical object metadata. Staging is atomically published only at commit.

## Agent handover and rollback

The source sends a `control-plane-handover/prepare` task to the existing Agent.
The Agent persists an encrypted Kubernetes Secret containing the previous
endpoint, cluster ID, credential, target endpoint/token, migration ID, status,
and rollback deadline. It updates only the bootstrap Secret and restarts the
same comm-agent Deployment in `hypercdr-agent`.

Velero, node-agent, Kopia repository, BSL/VSL, object-storage prefix, PVCs,
RBAC, historical backups, and namespace are unchanged. The target accepts the
preserved cluster identity, verifies the imported relationships and data plane,
and sends `confirm`. Before commit, `rollback` restores the old endpoint and
credential. An unconfirmed handover automatically rolls back after ten minutes
(configurable from five to thirty minutes, never disabled). `commit` erases the
rollback snapshot and makes Enterprise the sole writer.

## Disaster handover

Disaster handover is a separate audited workflow for a permanently unavailable
Community control plane. It requires a short-lived Enterprise recovery token
and cluster-admin execution in the managed cluster. It preserves all data-plane
resources but can recover only platform data available from surviving cluster
and object-storage metadata. The standard installer never exposes a force or
allow-existing-Agent switch.
