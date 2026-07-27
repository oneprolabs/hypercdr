# HyperCDR Project Context

> Historical pre-restructure snapshot. Use the root README, `scripts/`, and
> `docs/deployment/` for current paths and workflows.

Last aligned: 2026-06-05

## Project Map

- `/data/hypercdr/hypercdr-hyperbdr-style`: the completed container disaster recovery page prototype, usually served on port `3001`.
- `/data/hypercdr/hypercdr-platform`: the real HyperCDR platform project generated from the prototype.
- `/data/hypercdr/backups`: manual tarball snapshots from previous development checkpoints.

The repository metadata is currently not usable: `/data/hypercdr/.git` exists but is empty. Use file contents and backups for recovery unless git is restored.

## Service Ports

- Prototype UI: `http://127.0.0.1:3001`
  - Directory: `/data/hypercdr/hypercdr-hyperbdr-style`
  - Command: `npm run dev`
  - Script starts Express + Vite through `tsx server.ts`.
- Platform frontend: `http://127.0.0.1:3002`
  - Directory: `/data/hypercdr/hypercdr-platform/platform/frontend`
  - Command: `npm run dev`
  - Vite proxies `/api`, `/install.sh`, `/healthz`, and `/readyz` to `http://127.0.0.1:18080`.
- Platform backend API: `http://127.0.0.1:18080`
  - Directory: `/data/hypercdr/hypercdr-platform/platform/backend`
  - Typical command:
    `env GOCACHE=/data/hypercdr/hypercdr-platform/.gocache HCDR_HTTP_ADDR=127.0.0.1:18080 go run -buildvcs=false ./cmd/platform-api`
- Current local backend should use PostgreSQL, not the in-memory repository. For source-cluster registration tests on this host, start it with:
  `env GOCACHE=/data/hypercdr/hypercdr-platform/.gocache HCDR_DATABASE_URL=postgres://hypercdr:hypercdr@127.0.0.1:5432/hypercdr?sslmode=disable HCDR_HTTP_ADDR=0.0.0.0:18080 HCDR_PUBLIC_BASE_URL=http://192.168.8.149:18080 HCDR_AGENT_WS_ENDPOINT=ws://192.168.8.149:18080/ws/agent HCDR_IMAGE_REGISTRY=192.168.8.149/hypercdr go run -buildvcs=false ./cmd/platform-api`
- Local PostgreSQL:
  - Current host service: PostgreSQL 12 cluster `12/main`, started with `pg_ctlcluster 12 main start`.
  - Database URL:
    `postgres://hypercdr:hypercdr@127.0.0.1:5432/hypercdr?sslmode=disable`
  - Migrations command:
    `env GOCACHE=/data/hypercdr/hypercdr-platform/.gocache HCDR_DATABASE_URL=postgres://hypercdr:hypercdr@127.0.0.1:5432/hypercdr?sslmode=disable go run -buildvcs=false ./cmd/platform-migrate`
  - Docker compose config remains available as an alternative: `/data/hypercdr/hypercdr-platform/deployments/docker/compose.yaml`
  - Docker compose command: `docker compose -f deployments/docker/compose.yaml up -d postgres`
  - Docker compose database URL:
    `postgres://hypercdr:hypercdr@127.0.0.1:15432/hypercdr?sslmode=disable`
- Local internal image registry:
  - Harbor is installed under `/data/harbor/setup/harbor`.
  - Harbor access address is `https://192.168.8.149`.
  - Project `hypercdr` exists and is public.
  - Current pushed images:
    - `192.168.8.149/hypercdr/comm-agent:dev`
    - `192.168.8.149/hypercdr/velero:v1.17.1-helperfix`
  - The Velero source is maintained in the sibling `hypercdr-velero` repository. The platform repository consumes the published Velero image and keeps only the runtime CRDs and image references.
  - Harbor TLS has been replaced with a HyperCDR internal CA signed certificate:
    - CA: `/data/harbor/cert/hypercdr-ca.crt`, valid until 2036-06-08.
    - Server cert: `/data/harbor/cert/public.crt`, valid until 2031-06-10.
    - SAN includes `IP:192.168.8.149`, `DNS:harbor.hypercdr.local`, and `DNS:192.168.8.149`.
  - The Harbor proxy actually loads `/data/harbor/secret/cert/server.crt` and `/data/harbor/secret/cert/server.key`; these were synced from the new server cert/key.
  - Platform host Docker trusts Harbor through `/etc/docker/certs.d/192.168.8.149/ca.crt`. It no longer needs `insecure-registries`.
  - Kubernetes nodes use containerd and must trust `/data/harbor/cert/hypercdr-ca.crt` under `/etc/containerd/certs.d/192.168.8.149/ca.crt` before pulling Harbor images.
  - `/assets/registry/ca.crt` serves the HyperCDR registry CA from the platform backend.
  - `/install.sh` now defaults `REGISTRY_CA_URL` to the platform CA endpoint, installs the CA on the current node, and can distribute the CA to all Kubernetes nodes when `--node-ssh-user` and `--node-ssh-key` are provided.
  - `/install.sh` performs image-pull preflight with temporary Pods before creating the final Velero/comm-agent workloads.

## Product Understanding

The `3001` project is the polished UI/product prototype for a Kubernetes/container disaster recovery platform. It is the source of product experience, page structure, workflow, and visual style. Its backend is only an in-memory Express mock.

The real platform project should turn that prototype into a working product. The platform manages multiple Kubernetes clusters, application protection policies, storage repositories, restore points, recovery drills, takeover workflows, operations history, alerts, tags, tenants, and settings.

## Architecture Consensus

- The platform does not directly connect to managed Kubernetes API servers.
- Each managed cluster runs an agent stack in an isolated namespace, recommended as `hypercdr-agent`.
- The cluster-side stack contains:
  - self-developed Go `comm-agent`
  - official Velero
- `comm-agent` connects outbound to the platform WebSocket server.
- The platform owns REST APIs, WebSocket server, PostgreSQL persistence, task engine, scheduler, and frontend.
- Velero performs backup and restore execution. HyperCDR should not modify Velero source code in the first phase.
- HyperCDR integrates with Velero by creating and watching CRDs such as `BackupStorageLocation`, `VolumeSnapshotLocation`, `Backup`, and `Restore`.
- First-phase application model is Kubernetes namespace. Label selectors can narrow protected resources inside a namespace.
- Platform-generated install tokens are one-time and time-limited.
- After successful registration, the agent receives and persists a cluster credential, then stops using the install token.
- Object storage secrets should not be collected from clusters. Store or reference secrets safely, preferably as Kubernetes Secrets in the agent namespace and references in PostgreSQL.

## Current Implementation Stage

The implementation plan in `docs/architecture/implementation-plan.md` lists Phase 0 through Phase 9. Actual code is ahead of the original documentation baseline.

Current practical stage: Phase 6/7 prototype closed loop, not production complete.

Completed or mostly completed:

- Phase 1 backend skeleton:
  - Go backend entrypoints exist.
  - Config loading and structured logging exist.
  - `/healthz` and `/readyz` exist.
  - PostgreSQL store and memory store exist.
  - SQL migrations exist.
- Phase 2 agent registration and heartbeat:
  - `comm-agent` Go module exists.
  - WebSocket client exists.
  - Platform registration handler exists.
  - Agent credential response and persistence path exist.
  - Heartbeat loop exists.
- Phase 3 inventory collection:
  - Agent inventory collector exists.
  - Platform inventory ingestion exists.
  - Cluster/application REST API can return reported inventory.
- Phase 4 frontend migration:
  - Prototype UI has been moved into `platform/frontend`.
  - Frontend starts on `3002`.
  - Several pages call `/api/v1/...`.
  - Fallback prototype visual data remains when backend/API data is unavailable.
- Phase 5 storage, policy, and protection plan:
  - Storage repository list/create APIs exist.
  - Policy list/create APIs exist.
  - Protection plan list/create APIs exist.
  - Six-step protection wizard submits protection plans.
- Phase 6 backup task closed loop:
  - Manual backup task API exists.
  - Platform dispatches backup task over WebSocket if agent is connected.
  - Agent can build and submit Velero Backup manifests in dry-run or kubernetes mode.
  - Platform records task events and creates a restore point after backup completion.
- Phase 7 drill/takeover early path:
  - Recovery wizard exists.
  - Restore/drill/takeover task APIs exist.
  - Agent can build and submit Velero Restore manifests in dry-run or kubernetes mode.

Known gaps:

- `platform/backend/task-engine` and `platform/backend/scheduler` are still placeholders.
- There is no background scheduler creating backup tasks from policies.
- Queued tasks for offline agents are not redispatched by a background task engine.
- Agent task progress is simulated only in dry-run/no-status-reader mode. In kubernetes executor mode, `comm-agent` now polls Velero `BackupStorageLocation`, `Backup`, and `Restore` status through the dynamic client and reports phase/errors/warnings/raw status to the platform.
- Agent command deduplication is not complete.
- Agent session tracking/offline timeout handling is not fully implemented.
- Frontend still contains substantial prototype state and mock fallback data.
- Installer, Kubernetes manifests, Helm charts, upgrade, and unregister flows are not production complete.
- Storage credentials are accepted by the platform and not returned by APIs, but at-rest encryption with `HCDR_SECRET_KEY` still needs implementation.
- Storage sync now has the basic platform-to-agent path for Kubernetes Secret plus Velero `BackupStorageLocation`; BSL status polling is implemented for kubernetes executor mode.
- Agent reconnect now performs a conservative redispatch of `queued` and `dispatched` tasks for the reconnecting cluster. It covers storage-sync, backup, restore, drill, and takeover task payload reconstruction. Accepted/running task reconciliation is still deferred until real Velero status polling and idempotency are stronger.
- Platform-to-agent task messages and agent-to-platform report messages are standardized in `docs/platform-agent-message-protocol.md`. New work should use the shared envelope and typed command payloads instead of ad hoc fields or unstructured command maps.
- Cluster unregister is task-based by default:
  - UI calls `POST /api/v1/clusters/{id}/unregister`.
  - Platform creates an `unregister` task and dispatches it to the cluster agent.
  - Agent acknowledges completion, then self-uninstalls cluster-side HyperCDR resources.
  - Platform deletes cluster registration data only after `agent.task.completed`.
  - `DELETE /api/v1/clusters/{id}?force=true` exists only as a platform-side fallback cleanup path.

## Important Code Pointers

- Platform backend router:
  `hypercdr-platform/platform/backend/internal/httpserver/router.go`
- Platform-agent message protocol:
  `hypercdr-platform/docs/platform-agent-message-protocol.md`
- Platform store interface:
  `hypercdr-platform/platform/backend/internal/store/types.go`
- PostgreSQL store:
  `hypercdr-platform/platform/backend/internal/store/postgres.go`
- SQL migrations:
  `hypercdr-platform/platform/backend/internal/migrations/sql`
  and `hypercdr-platform/platform/backend/migrations`
- Agent WebSocket client:
  `hypercdr-platform/agent/comm-agent/internal/wsclient/client.go`
- Agent executors:
  `hypercdr-platform/agent/comm-agent/internal/executor`
- Velero manifest builders:
  `hypercdr-platform/agent/comm-agent/internal/velero`
- Platform frontend:
  `hypercdr-platform/platform/frontend/src/App.tsx`
- Protection wizard:
  `hypercdr-platform/platform/frontend/src/protect-wizard-modal.tsx`
- Recovery wizard:
  `hypercdr-platform/platform/frontend/src/recovery-wizard-modal.tsx`

## Verification Commands

Backend tests:

```bash
cd /data/hypercdr/hypercdr-platform/platform/backend
env GOCACHE=/data/hypercdr/hypercdr-platform/.gocache go test ./...
```

Agent tests:

```bash
cd /data/hypercdr/hypercdr-platform/agent/comm-agent
env GOCACHE=/data/hypercdr/hypercdr-platform/.gocache go test ./...
```

Frontend build:

```bash
cd /data/hypercdr/hypercdr-platform/platform/frontend
npm run build
```

Prototype/platform visual comparison after browser installation:

```bash
cd /data/hypercdr/hypercdr-platform/platform/frontend
npm run visual:compare
```

This expects:

- Prototype UI running at `http://127.0.0.1:3001`.
- Platform frontend running at `http://127.0.0.1:3002`.
- Playwright Chromium installed under `/data/software/ms-playwright`.
- Screenshots are written under `/tmp/hypercdr-visual-diffs/` by default. Override with `VISUAL_COMPARE_DIR` if needed.
- The npm script clears `DISPLAY`, `WAYLAND_DISPLAY`, and `XAUTHORITY` so Chrome runs headless and does not trigger Xmanager dialogs.

If automatic Chromium download is too slow, download this file on another host:

```text
https://cdn.playwright.dev/builds/cft/148.0.7778.96/linux64/chrome-linux64.zip
```

Then place it on this machine, for example:

```text
/data/software/chrome-linux64.zip
```

Expected extracted executable path:

```text
/data/software/ms-playwright/chromium-1223/chrome-linux64/chrome
```

The visual comparison script now checks that path before falling back to Playwright's default browser discovery.

Last known verification result:

- Backend tests passed with project-local `GOCACHE` on 2026-06-11.
- Agent tests passed with project-local `GOCACHE` on 2026-06-11.
- Frontend build passed on 2026-06-11. Vite reported only a large bundle warning.
- Visual comparison is operational. Chromium is installed under `/data/software/ms-playwright`, and the script successfully generated prototype/platform screenshots on 2026-06-11.

## Current Alignment

The agreed understanding is:

1. Use the `3001` prototype as the product and visual reference.
2. Keep the real platform in `hypercdr-platform`.
3. Preserve the architecture where agents connect outbound to the platform and use Velero for backup/restore.
4. Treat current code as a working prototype closed loop, not a finished production platform.
5. Before major new work, align whether the next priority is:
   - real backup/restore execution and Velero status watching,
   - background task engine and scheduler,
   - frontend full API migration,
   - installer/Helm/deployment packaging.

## Phase 1 Technical Alignment

These are hard requirements for the first production-oriented phase.

### Platform UI And Data

- The central platform UI should follow the current `3002` page design and interaction model.
- Platform pages must use real backend data, not demo or prototype-only state.
- The central platform owns its own database. PostgreSQL is the first-phase database.
- Frontend fallback demo data may exist only as a development safety net, but first-phase acceptance must run against real API/database data.
- The platform database is the source of truth for clusters, default cluster settings, storage repositories, policies, protection plans, tasks, task events, restore points, and operation history.

### Cluster-Side Agent Composition

- The cluster-side agent stack has two parts:
  - Official open-source Velero.
  - A HyperCDR-developed WebSocket client module, currently `comm-agent`.
- Velero owns actual backup and restore execution.
- `comm-agent` owns communication with the central platform and Kubernetes API operations needed to drive Velero.
- HyperCDR should not modify Velero source code in this phase.
- Velero should be installed automatically by the HyperCDR installer in phase 1. The user should not have to pre-install Velero manually.
- Phase 1 should pin Velero to a fixed stable version instead of pulling `latest`.
- Current phase-1 baseline is Velero `v1.17.1`. The full Velero source is maintained outside this repository in the sibling `hypercdr-velero` repository; this platform repository keeps only the CRDs and image references needed at runtime.
- `comm-agent` must be built and packaged as a container image by this project.
- Current packaging foundation:
  - `deployments/docker/comm-agent.Dockerfile` builds the Go `comm-agent` binary and packages it into a distroless runtime image.
  - `tools/build_comm_agent_image.sh` builds `registry.local:5000/hypercdr/comm-agent:dev` by default, or uses `HCDR_IMAGE_REGISTRY`, `HCDR_AGENT_IMAGE_TAG`, and `HCDR_AGENT_IMAGE`.
  - Set `HCDR_PUSH_IMAGE=true` to push after build.
- Installer foundation:
  - The backend embeds Velero `v1.17.1` CRDs under `platform/backend/internal/veleroassets/crds`.
  - `GET /assets/velero/v1.17.1/crds.yaml` serves the bundled CRDs so managed clusters do not need internet access for CRD installation.
  - `/install.sh` applies the bundled CRDs, installs Velero Deployment, installs Velero node-agent DaemonSet, installs the AWS/S3 plugin initContainer, and then deploys `comm-agent`.
  - Default images come from `HCDR_IMAGE_REGISTRY`: `velero:v1.17.1-helperfix`, `velero-plugin-for-aws:v1.13.0`, and `comm-agent:dev`.
- The target deployment model assumes an internal/private image registry. Required runtime images, including `comm-agent`, Velero, and any required Velero helper/node-agent images, should be mirrored or pushed into that registry so cluster installation does not depend on external network access.
- Internal registry address is configurable. Use a professional default such as `registry.local:5000/hypercdr` in docs/examples, but expect the real deployment to receive a host/IP, username, and password from the user.

### Platform-Agent Communication

- The central platform exposes a WebSocket server for agent communication.
- The cluster-side `comm-agent` initiates and maintains the outbound WebSocket connection to the platform.
- Cluster-side uploads include registration, heartbeat, inventory, task accepted/progress/completed/failed, Velero events, and relevant execution result metadata.
- Platform-side executable commands are standardized as tasks. Current task types include storage sync, backup, restore, drill, takeover, and unregister. Inventory request and future upgrade/config commands should follow the same message-envelope rules in `docs/platform-agent-message-protocol.md`.

### Velero CRD Control Path

- The central platform does not directly call managed cluster Kubernetes APIs.
- The platform sends commands over WebSocket to `comm-agent`.
- After receiving a command, `comm-agent` uses Kubernetes APIs inside the managed cluster to create or update Velero CRDs.
- Phase 1 object storage support is S3-compatible storage, including MinIO.
- Phase 1 does not need cloud/local block snapshot integration through `VolumeSnapshotLocation`; defer snapshot-location support to a later phase.
- Phase 1 must support PVC data backup and migration for stateful applications. The initial implementation should use Velero file-system backup/node-agent style data movement to object storage, not local/cloud volume snapshots.
- For object storage setup, `comm-agent` should create/update the required Kubernetes Secret and Velero `BackupStorageLocation` resources.
- Current implementation stores submitted S3 credentials in the storage repository secret payload, hides them from JSON API responses, dispatches them over the agent WebSocket storage-sync command, and `comm-agent` writes a Velero-compatible `Secret` with key `cloud` before applying the `BackupStorageLocation`.
- The `3002` storage page can create S3/S3-compatible repositories through the real API and can dispatch selected repositories to all known clusters through `/api/v1/storage-repositories/{id}/sync`.
- Storage repository bucket selection belongs to the user. The UI lets the user create a storage repository with any desired bucket, and backup/protection workflows select one of the configured repositories. Do not force a platform-generated bucket layout in phase 1.
- For backup, `comm-agent` should create a Velero `Backup` CR with the namespace, label selector, storage location, TTL, include/exclude resources, and hooks derived from the platform command/protection plan.
- Current Backup manifest generation explicitly sets `spec.defaultVolumesToFsBackup: true` and `spec.snapshotVolumes: false` so phase-1 PVC data uses Velero file-system backup/node-agent instead of VolumeSnapshotLocation.
- Phase 1 backup/restore scope is namespace-level application protection.
- For drill or takeover restore, `comm-agent` should create a Velero `Restore` CR from the selected restore point/backup name, with namespace mapping and conflict policy derived from the platform command.
- Phase 1 restore should support both:
  - restore into a new namespace on the target cluster, for example `<source-namespace>-drill`;
  - restore into the original namespace name on the target cluster.
- Cross-namespace application topology discovery is out of scope for phase 1.

### Phase 1 Operational Decisions

- Platform endpoint should be generated from the platform public base URL and exposed to agents as a WebSocket endpoint. First-phase local/lab default can be `ws://<platform-host>:18080/ws/agent`; production packaging should support `wss://` behind TLS.
- `/install.sh` now defaults runtime images to the internal registry through backend config and uses configurable `HCDR_AGENT_NAMESPACE`.
- The 3002 cluster registration page now uses the real phase-1 flow:
  - Calls `POST /api/v1/agent-tokens` to create a one-time install token.
  - Shows a copyable install command that includes `--executor-mode kubernetes` and the configured agent namespace.
  - The user runs the copied command in the Kubernetes cluster.
  - The dialog waits for the agent to register by polling platform APIs and closes when a new cluster appears.
  - Demo wording such as "Simulate Connection" has been removed from the registration path.
- The 3002 cluster unregister action now calls the real task-based `POST /api/v1/clusters/{id}/unregister` API.
  - The backend creates an `unregister` task and dispatches it to the cluster agent when connected.
  - If the agent is offline, the task remains queued and is redispatched on reconnect.
  - The agent reports success, then removes the HyperCDR namespace and cluster-level RBAC.
  - The platform removes cluster registration data only after the agent reports `agent.task.completed`.
  - The old direct platform delete behavior is restricted to `DELETE /api/v1/clusters/{id}?force=true` as an operational fallback.
  - The installer defaults `--reset-agent-credential true`, deletes the old `hypercdr-agent-credential` Secret, and restarts the comm-agent Deployment so rerunning a fresh install command can register the same Kubernetes cluster again with a new token.
- MinIO/S3 credentials should be handled professionally:
  - Platform stores credentials encrypted at rest or behind a secrets-table/secret-backend abstraction.
  - API responses must never return secret values after creation.
  - `comm-agent` writes object storage credentials into Kubernetes Secret in `hypercdr-agent`.
  - Velero references that Secret from `BackupStorageLocation`.
- Restore defaults:
  - Drill defaults to a new namespace on the target cluster, such as `<source-namespace>-drill`.
  - Takeover defaults to the original namespace name on the target cluster.
  - The UI/API should allow either original namespace or a custom/new namespace for phase 1.
- Conflict policy:
  - Drill defaults to a new namespace to avoid conflicts.
  - Restore to original namespace defaults to skip/no-overwrite.
  - Takeover may allow overwrite only when explicitly selected and warned in the UI.
- Agent namespace:
  - Default managed-cluster namespace is `hypercdr-agent`.
  - Installer should support overriding the namespace later, but phase 1 can default to `hypercdr-agent`.
- Velero CR namespace:
  - Velero `Backup`, `Restore`, and `BackupStorageLocation` resources are created in the Velero/agent namespace, default `hypercdr-agent`.
- PVC backup:
  - Phase 1 should enable Velero file-system backup/node-agent by default for PVC data backup to the selected object storage repository.
- Platform secret encryption:
  - Use a platform-level `HCDR_SECRET_KEY`.
  - Development can warn and use a generated/in-memory key if needed, but production mode must require a configured key.
- Agent reconnect handling:
  - On reconnect, the platform should redispatch or reconcile non-terminal tasks (`queued`, `dispatched`, `accepted`, `running`) that do not have a terminal result.
- Velero status tracking:
  - Phase 1 uses simple polling instead of Kubernetes watch.
  - `comm-agent` polls Velero `BackupStorageLocation`, `Backup`, and `Restore` status every 5 seconds in kubernetes mode.
  - Successful terminal phases: BSL `Available`, Backup `Completed`, Restore `Completed`.
  - Failure phases: BSL `Unavailable`, Backup/Restore `Failed` or `PartiallyFailed`.
  - Failure events include Velero payload details under task event payload.
- E2E tooling:
  - Add small scripts or tools as needed, such as `tools/phase1_e2e.sh`, to automate setup checks and acceptance verification.
- Test application selection must not be hard-coded. The user selects a real namespace application from the source cluster application list.
- Phase 1 is single-tenant. Multi-tenant/RBAC expansion is deferred.
- Final acceptance uses both UI and technical verification:
  - UI completes the main flow.
  - API/database checks verify platform truth.
  - `kubectl` checks verify Velero CRs, BackupStorageLocation, Backup, Restore, namespaces, and restored resources in the clusters.

### Velero Status Watch And Result Upload

- `comm-agent` must watch Velero CR status instead of only simulating progress.
- For Backup tasks, `comm-agent` must watch the Velero `Backup` phase, validation errors, warnings/errors, timestamps, included resources when available, and completion state.
- For Restore tasks, `comm-agent` must watch the Velero `Restore` phase, validation errors, warnings/errors, namespace mapping result, timestamps, and completion state.
- `comm-agent` must send monotonic task progress and terminal results back to the platform through WebSocket.
- The platform must persist task status, task events, Velero status details, and create restore points after successful backups.
- The platform UI must show real task progress, terminal state, restore points, and drill/takeover results from platform API data.
- Failure handling should expose at least Velero phase, warning/error counts, the last meaningful error message, related Velero CR name, and task event timeline.

## Suggested Next Priorities

Recommended next engineering order:

1. Validate generated installer against a real test cluster and adjust Velero RBAC/security context as needed.
2. Expand task reconciliation from queued/dispatched redispatch to accepted/running reconciliation using Velero CR labels/status.
3. Implement scheduler that creates backup tasks from active protection policies.
4. Continue replacing frontend mock state with REST API data for the phase-1 acceptance path.

## Local Test Clusters (current snapshot, 2026-06-15)

These are the on-host kubeconfigs available for the source/target clusters used in the
phase-1 manual sync closed loop.

- Source cluster: `192.168.7.136` (control-plane node `k8s-136`).
  - kubeconfig: `/data/hypercdr/kubeconfigs/config-136`
  - 3 nodes, 11 namespaces.
  - `hypercdr-agent` and `velero` namespaces are installed and healthy.
  - Velero `BackupStorageLocation` named `default` (provider `aws`, bucket from
    `velero-repo-credentials` Secret) is `Available`.
  - This cluster corresponds to platform cluster `99db62af-d41b-4080-88bf-8a59a77fb899`
    (role=both, isDefault=true).
- Target cluster: `192.168.8.158` (control-plane node `k8s-master`).
  - kubeconfig: `/data/hypercdr/kubeconfigs/config-158`
  - 2 nodes.
  - `hypercdr-agent` and `velero` namespaces are installed and healthy.
  - `velero` namespace has no `BackupStorageLocation` yet. Storage sync has never
    been dispatched to this cluster.
  - This cluster corresponds to platform cluster `a2da8194-b370-40ba-bc85-3ca1cc49be10`
    (role=both).
- Out-of-scope here: the `dr-target-cluster` on `192.168.8.149` (4 nodes, platform cluster
  `00000000-0000-0000-0000-00000000bbbb`).

## Current Manual Sync Closed-Loop Stage (2026-06-15)

Stage target: complete DR configuration in the `3002` UI, click `Start Sync`, the platform
dispatches the backup task to the cluster's `comm-agent`, the agent creates a Velero
`Backup` CR, progress is reported back, the platform creates a restore point on success,
and the UI can launch a drill from that restore point.

Known break point observed against `99db62af-...` (source) with the existing `my-minio`
storage repository:

- The platform has a `StorageRepository` named `my-minio`, but the source cluster's
  Velero namespace only has a `BackupStorageLocation` named `default`.
- `POST /api/v1/tasks/backup` issued against `kasten-io` / `kasten-io-mc` produced
  Velero `Backup` CRs with `spec.storageLocation: my-minio`, but Velero returned
  `FailedValidation` with `BackupStorageLocation "my-minio" not found` and the task
  eventually timed out after 30 minutes (`Backup_STATUS_TIMEOUT`).
- The fix path is to ensure a `storage-sync` to the source cluster before issuing
  the backup, or to align the storage repository name with the cluster's actual
  Velero `BackupStorageLocation` (`default`).

Next engineering order for this stage (replacing the previous "Suggested Next
Priorities" while the manual closed loop is not yet green):

1. Make `Start Sync` ensure the selected storage repository is synced to the source
   cluster (auto-`storage-sync` when needed) and surface a clear error otherwise.
2. Add a quick precheck to `dispatchBackupTaskImpl` that rejects the dispatch with a
   `STORAGE_NOT_SYNCED` task error when the source cluster has no matching BSL.
3. Re-run the closed loop with a small namespace (`default` or `dev-test`) and verify:
   page progress, task `succeeded`, restore point created, drill task dispatches and
   creates a Velero `Restore` CR in the target cluster.
4. Only then move to the original priorities 2-4 in `## Suggested Next Priorities`.

## Manual Sync Closed-Loop Status (2026-06-15, end of session)

The manual sync closed loop (UI Start Sync -> platform -> comm-agent -> Velero Backup
-> progress events -> restore point -> drill on target cluster) is now green for a
light namespace (`192.168.7.136` / `dev-test` / `my-minio`).

Real run on 2026-06-15T02:30Z..02:47Z:

- 136 cluster backup of namespace `dev-test`:
  - `hcdr-dev-test-20260615024348-c9278d0c` reached Velero `Completed`,
  - platform task `c9278d0c-2fc5-4590-94bd-6702a8cc157e` reached `succeeded`,
  - `/api/v1/restore-points` returned restore point `242df29c-...` with
    `sourceClusterId=99db62af-...`, `sourceNamespace=dev-test`,
    `veleroBackupName=hcdr-dev-test-20260615024348-c9278d0c`,
    `backupStorageName=my-minio`, `status=available`.
- 158 cluster drill from that restore point:
  - `hcdr-restore-dev-test-5e7b22f2` reached Velero `Completed`,
  - platform task `5e7b22f2-...` reached `succeeded`,
  - namespace `dev-test-drill` was created on `192.168.8.158` with pod
    `busybox-test 1/1 Running`.

Fixes applied during this session:

- `agent/comm-agent/internal/velero/storage.go`: BackupStorageLocation
  `spec.config` is now sanitised:
  - `urlStyle=path` is translated to `s3ForcePathStyle=true`,
  - `urlStyle=virtualHost` is also translated,
  - `prefix` is consumed into `objectStorage.prefix` and dropped from `config`,
  - `bucket` and `caCert` are also dropped (they are not AWS-plugin config keys),
  - `s3Url` automatically receives `http://` / `https://` scheme based on
    `TLSEnabled`,
  - any other unknown key is dropped to avoid Velero validation errors.
  - New unit test `TestBuildBackupStorageLocationManifestMapsPathStyleConfig`
    locks the behaviour.
- `platform/backend/internal/httpserver/router.go`:
  - `POST /api/v1/tasks/backup` now de-duplicates the protection-plan app set
    so a single app on a plan is dispatched once (the legacy `app_id` plus the
    many-to-many `protection_plan_apps` table were producing two tasks per
    app).
  - The `install.sh` template now ships Velero with a writable
    `/udmrepo` `emptyDir` volume plus `HOME=/udmrepo` and
    `XDG_CACHE_HOME=/udmrepo/.cache` environment variables. Without these
    the Velero v1.17.1 Kopia uploader fails with
    `mkdir /udmrepo: permission denied` / `mkdir /.cache: permission denied`
    on the distroless Velero image.
- `platform/backend/internal/store/postgres.go`: `ListTasks` now copies
  `cluster_id` back into the returned `Task` struct so `/api/v1/tasks` and
  the page-side task list show the correct cluster.

Known follow-up (not blocking the manual closed loop):

- Heavy namespaces such as `kasten-io` (14 workloads, 4 PVCs, ~60 GB) are
  slow but do make progress; in the same session the backup
  `2ffbcb91-...` reached the Kopia upload stage and only the in-pod Velero
  `Backup` controller hanging on its first reconcile blocked a clean
  terminal. A `kubectl delete pod` on the Velero pod on `192.168.7.136` and
  re-running the backup was enough to unblock the controller. The Velero
  community tracks similar issues; the cluster already has the `/udmrepo`
  volume mounted.
- `comm-agent` and the platform are still running pre-existing image
  binaries. The `urlStyle`/`scheme` fix and the install-script `/udmrepo`
  fix are in source but the next agent image build will roll them out.
- The next engineering priority after validating the heavy `kasten-io`
  backup end-to-end is to add a "Storage BSL not synced" precheck in
  `dispatchBackupTaskImpl` so a fresh start-sync on a cluster that has not
  yet received a `storage-sync` for the selected storage repository fails
  immediately instead of waiting 30 minutes for Velero.

## Session Addendum — 2026-06-15 03:42Z (kasten-io recovery work)

Picked up from the v2 stage backup. The `dev-test` closed loop is already
green (task `c9278d0c` succeeded on 136; task `5e7b22f2` drill succeeded
on 158; namespace `dev-test-drill` running `busybox-test 1/1`). The new
target is the **heavy `kasten-io` namespace** (14 workloads, 4 PVCs,
~60 GB).

### Observed this session

- The dev-test fully manual closed loop remains green and is the
  reference for "HyperCDR works end to end".
- Kasten-io backup keeps hitting a Velero 1.17.1 backup-controller bug
  on first reconcile: `Backup` CR stays at `phase=<none>`, `items=<none>`,
  no `PodVolumeBackup` is ever created. Workaround used so far has been
  `kubectl delete pod -l app.kubernetes.io/name=velero -n hypercdr-agent
  --grace-period=0 --force` on 136. After the restart, the new Backup CR
  is reconciled and an old InProgress one is marked `Failed` with
  reason `found a backup with status "InProgress" during the server
  starting, mark it as "Failed"`.
- To reduce the controller's surface I `kubectl patch`ed the latest
  kasten-io `Backup` CR with an `excludedResources` block listing the
  kasten CRDs (actions.kio.kasten.io, config.kio.kasten.io, reports.kio,
  snapshot.storage.k8s.io) plus the volume-snapshot CRDs. After Velero
  restart, the controller is now reconciling properly.

### Current state at 2026-06-15 03:42Z

- `task a6ee2f91-5f45-437e-a430-8a1fb2e6a2db`: status `running`, progress
  `85%`, `Backup CR phase: InProgress`, `Total Items: 231`, start
  `2026-06-15T03:37:48Z`. Two `PodVolumeBackup` records are already
  present (kopia uploader, no `BYTES DONE` yet). Item collection is
  clearly progressing in the velero pod log.
- `dev-test` reference: task `c9278d0c-...` succeeded; restore point
  `242df29c-6dd2-4056-9bc2-8ad24c4dd450` is available.
- `comm-agent` and `velero` images running on both clusters are still
  the pre-fix binaries; the source-side fixes from the v2 session are
  in tree but not yet built and rolled.

### Engineering follow-up needed before this loop is product-ready

1. `agent/comm-agent/internal/velero/backup.go` does NOT yet expose
   `ExcludedResources` on `BackupManifestSpec`. The protocol layer
   (`agent/comm-agent/pkg/protocol/messages.go` and the matching
   `platform/backend/internal/protocol/messages.go`) already has
   `BackupCommand.ExcludeResources []ExcludeRule`, but it is set to an
   empty slice in `dispatchBackupTaskImpl`. Add the field, wire the
   conversion (`ExcludeRule` → `Backup.Spec.ExcludedResources`), and
   decide where the kasten CRD list is sourced from. The simplest source
   is a per-app annotation on the `Application` record (or a
   protection-plan-level extension) that the wizard can populate
   automatically for known large apps.
2. Rebuild the `comm-agent` image and roll it out to 136 and 158
   (Harbor push, `kubectl rollout restart deploy/hypercdr-comm-agent`).
3. After (1) and (2), re-run a kasten-io backup through the page so
   the round-trip is exercised end-to-end (no manual `kubectl patch`).
4. Once the kasten-io restore point is available, drive a drill from
   158 and confirm pods come up.
5. Add a `BackupStorageLocation unavailable` precheck in
   `dispatchBackupTaskImpl` so a start-sync on a cluster that has not
   yet received a `storage-sync` fails fast (this avoids the 30-minute
   Velero wait).

## Session Addendum — 2026-06-15 04:30Z (kasten-io engineering push + comm-agent image fix)

### What changed in the code

Agent side (`agent/comm-agent/internal/velero/backup.go`):
- Added `ExcludedResources []string` field to `BackupManifestSpec`.
- Added `convertExcludeRules([]protocol.ExcludeRule) []string` helper that
  translates protocol-level `ExcludeRule` entries into Velero's
  `group/resource` form, normalises `core` to empty, de-duplicates, and
  drops empty resource names. Returns nil so the JSON marshaler omits the
  field when there are no rules.
- New unit tests:
  `TestBuildBackupManifestPopulatesExcludedResourcesFromCommand`,
  `TestBuildBackupManifestOmitsExcludedResourcesWhenEmpty`,
  `TestBuildBackupManifestRejectsEmptyAppNamespace`.

Platform side (`platform/backend/internal/httpserver/`):
- New file `exclude_defaults.go` with
  `defaultExcludedResources(appNamespace string) []protocol.ExcludeRule`
  and `kastenDefaultExcludedResources()`. The kasten list covers
  actions.kio.kasten.io (6), config.kio.kasten.io (6),
  reports.kio.kasten.io (3), and snapshot.storage.k8s.io (2).
- `dispatchBackupTaskImpl` and `buildStoredTaskDispatch` now call
  `defaultExcludedResources(...)` to fill the `ExcludeResources` field
  in the dispatch message.
- New unit tests: `TestDefaultExcludedResourcesForKasten`,
  `TestDefaultExcludedResourcesUnknownNamespace`,
  `TestKastenDefaultListIsStable`.

Both modules: `go test -buildvcs=false ./...` is green.

### Image rebuild notes (important for the next host)

The previous `192.168.8.149/hypercdr/comm-agent:dev` was effectively a
scratch-style image with only the `comm-agent` binary — no
`/etc/passwd`, `/etc/group`, `/etc/shadow`. Older containerd allowed
that, but the new containerd on the k8s 1.29 nodes refuses to start a
container with `USER nonroot:nonroot` unless those files are present
(the error is "open /var/lib/containerd/tmpmounts/.../etc/passwd: no
such file or directory"). The new image is built with this minimal
context:

```
/tmp/comm-agent-rebuild.Dockerfile
  FROM 192.168.8.149/hypercdr/comm-agent:base-old
  COPY comm-agent-rebuild /comm-agent
  COPY etc/passwd /etc/passwd
  COPY etc/group  /etc/group
  COPY etc/shadow /etc/shadow
  USER nonroot:nonroot
  ENTRYPOINT ["/comm-agent"]
```

`/tmp/etc/passwd` minimal content:
```
root:x:0:0:root:/root:/sbin/nologin
nonroot:x:65532:65532:nonroot:/home/nonroot:/sbin/nologin
```
`/tmp/etc/group` minimal content:
```
root:x:0:
nonroot:x:65532:
tty:x:5:
```
`/tmp/etc/shadow` minimal content:
```
root:!::0:::::
nonroot:!::0:::::
```

Build host: build the new comm-agent binary with
```
cd /data/hypercdr/hypercdr-platform/agent/comm-agent
env GOCACHE=/tmp/gocache CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -buildvcs=false -trimpath -ldflags="-s -w" \
  -o /tmp/comm-agent-rebuild ./cmd/comm-agent
```
then build the image with the Dockerfile above, push to Harbor and
`kubectl rollout restart deployment/hypercdr-comm-agent -n hypercdr-agent`
on both 136 and 158.

### Stage-1 closed loop status at 04:30Z

- dev-test: backup + cross-cluster drill in 158, all green.
  Restore point `242df29c-6dd2-4056-9bc2-8ad24c4dd450` (available).
  A second drill (task `9bf91dcd-...`) into `dev-test-drill-v2`
  succeeded at 04:28:25Z — proves the drill path still works after the
  new comm-agent rollout.
- kasten-io: Backup CRs are now being created with the
  `excludedResources` block populated by the platform code (verified
  on `hcdr-kasten-io-20260615041933-2f2ad515` and
  `hcdr-kasten-io-20260615042415-5b9f8734`), but the velero 1.17.1
  backup controller still hangs after metadata collection on this
  large namespace with 4 PVCs. The hang is reproducible and
  independent of HyperCDR code; it survives velero pod restarts.
  Two task records (`2f2ad515-...` and `5b9f8734-...`) are marked
  failed in the platform DB with the reason captured.
- The next concrete step to unblock the kasten-io closed loop is
  to either downgrade velero to 1.14.x or to disable fs-backup
  (`DefaultVolumesToFsBackup: false`) for kasten-io-style namespaces
  and rely on a CSI snapshot strategy. Both options are deferred
  until the next session.

### Out-of-scope (still placeholder)

- `internal/scheduler` package is still empty placeholder.
- `task-engine` package is still empty placeholder.
- The 000009 migration adds `protection_plan_schedules` but the API
  surface and the dispatcher hook for cron-style automatic execution
  are not yet implemented.
