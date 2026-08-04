# Codex Desktop Handoff — 2026-08-04

## Read this first

- Development host: `192.168.8.149` (Ubuntu).
- Authoritative repository: `/data/hypercdr-main`.
- Authoritative branch: `main`.
- Do all future source edits in `/data/hypercdr-main`. Do **not** edit `/data/hypercdr`.
- Follow `/data/hypercdr-main/AGENTS.md`.
- This file intentionally contains no passwords, tokens, secret keys, or database URLs.

## Git state

- Current HEAD: `92f46e2 Decompose the app.tsx file and optimize the code structure`.
- The Drill/Restore work below is present as **uncommitted changes** in the `main` working tree.
- Do not discard or overwrite the working tree. Inspect `git status --short` before editing.
- `frontend/node_modules` is a development symlink created by `scripts/dev/start-dev.sh` and points to dependencies outside the repository. Do not include it in a source commit.

## Why this handoff exists

Some Drill/Restore changes were mistakenly developed in the old `/data/hypercdr` worktree on `master`. They have now been migrated and adapted to the decomposed frontend architecture in `/data/hypercdr-main/main`. The old worktree must be treated as read-only historical reference.

The following unrelated changes remain only in the old worktree and were deliberately not mixed into the migration:

- release image-digest publishing scripts;
- component-release version uniqueness/store changes;
- migration `000026_component_release_version_identity.sql`.

## Drill/Restore changes now in main

### Frontend

- `frontend/src/recovery-wizard-modal.tsx`
  - Advanced options drawer.
  - Restore Content catalog loaded from the selected restore point.
  - Resource include/exclude selection.
  - StorageClass mapping.
  - Container image mapping.
  - Workload wait, validation, and force-start options.
- `frontend/src/features/applications/application-dr-page.tsx`
  - Supplies defaults, Restore Content loader, cluster StorageClasses, and advanced recovery payload.
- `frontend/src/features/restore-points/restore-point-page.tsx`
  - Same integration for the Restore Points entry path.
  - Failed recovery retry action.
- `frontend/src/features/recovery/task-ui.tsx`
  - Recovery-stage presentation, evidence, improved failure solution text, retry UI support.
- `frontend/src/styles.css`
  - Advanced recovery and recovery-stage styles.

### Backend

- `backend/internal/httpserver/router.go`
  - `GET /api/v1/restore-points/{id}/contents`.
  - Backup-content request/response coordination with Comm Agent.
  - Restore Point `contentIndex` background generation, persistence, retry, concurrency limiting, and compatibility loading.
  - Advanced recovery fields and recovery retry API.
- `backend/internal/protocol/messages.go`
  - Backup-content protocol messages and advanced RestoreCommand fields.
- New tests:
  - `backend/internal/httpserver/recovery_dispatch_test.go`
  - `backend/internal/httpserver/recovery_stage_payload_test.go`
  - `backend/internal/httpserver/restore_point_content_index_test.go`

### Comm Agent

- New BackupContents reader:
  - `agent/comm-agent/internal/kube/backup_contents.go`
  - `agent/comm-agent/internal/kube/backup_contents_test.go`
- Protocol handling and recovery-stage reporting:
  - `agent/comm-agent/internal/wsclient/client.go`
  - `agent/comm-agent/internal/wsclient/recovery_stages_test.go`
  - `agent/comm-agent/pkg/protocol/messages.go`
- Restore improvements across `internal/executor`, `internal/kube`, and `internal/velero`:
  - included/excluded resources;
  - StorageClass/image mappings through Velero ResourceModifier;
  - ResourceModifier propagation barrier;
  - readiness/application-validation stages;
  - detailed image-pull failure events.
- Audit document: `docs/drill-recovery-implementation-audit.md`.

## Validation already completed

- `go test ./agent/comm-agent/...`: passed.
- `go test ./backend/internal/httpserver`: passed outside the sandbox because `httptest` needs local listener permission.
- Frontend TypeScript check: passed.
- Frontend Vite production build: passed using dependencies copied to `/tmp`.
- Bootstrap registry tests: passed.
- Shell syntax checks: passed.
- `git diff --check`: passed.
- `make verify` completed backend and Agent tests; its in-place frontend step initially failed only because `frontend/node_modules` was absent at that moment. A later full frontend build passed, and deployment recreated the external dependency symlink.

## Current development deployment

- Started from `/data/hypercdr-main/scripts/dev/start-dev.sh`.
- Frontend URL: `https://192.168.8.149:3002`.
- Frontend process working directory: `/data/hypercdr-main/frontend`.
- Backend binary: `/data/hypercdr-runtime/dev/bin/platform-api`.
- Backend internal endpoint: `http://127.0.0.1:18080`.
- Runtime/logs: `/data/hypercdr-runtime/dev`.
- Services:
  - `hypercdr-dev-api.service`: active.
  - `hypercdr-dev-frontend.service`: active.
- Both `/healthz` endpoints returned `status: ok` after deployment.
- The served frontend source was checked and contains `Advanced options`, `Restore content`, and `loadContents`.

Useful commands:

```bash
cd /data/hypercdr-main
./scripts/dev/status-dev.sh
tail -120 /data/hypercdr-runtime/dev/logs/platform-api.log
tail -120 /data/hypercdr-runtime/dev/logs/platform-frontend.log
```

Restart using the repository scripts rather than the old `/data/hypercdr` scripts.

## Formal/containerized deployment on host 149

The standard/formal deployment is Docker Compose based. It is separate from the
current Vite/systemd development deployment.

- Host deployment directory: `/var/lib/hypercdr`.
- Compose file: `/var/lib/hypercdr/docker-compose.yaml`.
- Environment file: `/var/lib/hypercdr/.env` (contains secrets; never print or
  copy its values into chat or documentation).
- Persistent database data: `/var/lib/hypercdr/data/postgres`.
- TLS material: `/var/lib/hypercdr/tls`.
- Formal services:
  - `hypercdr-platform-api`, host port 18080;
  - `hypercdr-platform-frontend`, host port 3002;
  - `hypercdr-platform-upgrader`;
  - `hypercdr-postgres`.
- Installed formal platform version currently represented by the API/frontend/
  upgrader containers: `v20260729.11`.
- Those three platform containers are currently **stopped** because ports 3002
  and 18080 are being used by the development deployment.
- `hypercdr-postgres` remains running and healthy; the development backend is
  intentionally reusing this database.
- Do not start the formal frontend/API while the development transient services
  are active, or the host ports will conflict.

Formal deployment inspection commands:

```bash
cd /var/lib/hypercdr
docker compose --env-file .env -f docker-compose.yaml ps
docker compose --env-file .env -f docker-compose.yaml logs --tail=200
```

Switching from development back to formal deployment should be treated as an
explicit operational action:

1. stop development with
   `/data/hypercdr-main/scripts/dev/stop-dev.sh`;
2. from `/var/lib/hypercdr`, run the existing Compose deployment with its
   protected `.env`;
3. verify ports 3002/18080, health endpoints, API/WS routing, and container
   images.

The normal installation entry point is
`bootstrap/install-platform.sh docker --public-base-url ... --execute`.
The installer writes the resolved formal deployment to `/var/lib/hypercdr`.
Do not regenerate or overwrite that directory during ordinary development.

### Bootstrap portal

- Container: `hypercdr-bootstrap-portal`.
- Current status: running.
- Host port: `8080`.
- URL: `http://192.168.8.149:8080`.
- Current Compose source:
  `/data/hypercdr-main/bootstrap/portal/portal-compose.yaml`.
- It serves installer/release artifacts and is independent of the development
  frontend on port 3002.

## Container registries and release publishing

Registry profiles are declared without credentials in
`config/registries.conf`.

### Alibaba Cloud ACR

- Profile: `aliyun_acr`.
- This is the current active/default registry profile.
- Provider: Alibaba Cloud ACR Personal Edition.
- Server:
  `crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com`.
- HyperCDR prefix:
  `crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr`.
- Visibility is configured as public; trust uses the system CA store.
- Credentials are not stored in the repository. Authenticate interactively or
  through approved CI secrets:

```bash
cd /data/hypercdr-main
./scripts/registry-login.sh --profile aliyun_acr
```

The formal `v20260729.11` API/frontend/upgrader images currently present on
host 149 came from this ACR prefix. Multiple Comm Agent builds are also present;
the newest locally observed ACR Comm Agent tag is `v20260803.7`. Do not infer
that a locally cached image is the currently registered upgrade target; query
the platform release catalog before publishing or upgrading.

### Local Harbor

- Profile: `harbor_149`.
- Prefix: `192.168.8.149:5001/hypercdr`.
- Private registry using a private CA.
- CA file: `/data/harbor/cert/hypercdr-ca.crt`.
- Harbor operational details are in the next section.

### Release flow

The repository-wide release entry point is:

```bash
cd /data/hypercdr-main
./scripts/release/release-all.sh vYYYYMMDD.N
```

It builds/tests and publishes:

- `platform-api`;
- `platform-frontend`;
- `platform-upgrader`;
- `comm-agent`;
- PostgreSQL, customized Velero, and supported object-storage plugins as
  required by the release workflow.

Useful variants:

```bash
# Publish to the local Harbor profile
./scripts/release/release-all.sh vYYYYMMDD.N --registry-profile harbor_149

# Initial publication without platform release registration
./scripts/release/release-all.sh vYYYYMMDD.N --skip-register

# Resume an interrupted immutable-tag release
./scripts/release/release-all.sh vYYYYMMDD.N --skip-register --resume
```

Release rules:

- version tags are intended to be immutable;
- use the profile configuration rather than hardcoding a registry;
- never write registry credentials into `config/registries.conf`;
- customized Velero must be built from the pinned source workflow, not replaced
  with an unrelated upstream binary;
- generated build/release artifacts belong under `/data/hypercdr-runtime`;
- publishing or registering a release changes external state and requires
  explicit user authorization.

## Harbor on host 149

- Harbor Compose directory: `/data/harbor`.
- Compose file: `/data/harbor/docker-compose.yml`.
- URL/registry: `https://192.168.8.149:5001`.
- Harbor was restarted and all components reported healthy.
- `https://127.0.0.1:5001/api/v2.0/health` returned overall `healthy`.
- `/v2/` returned the expected authenticated Registry `401` response.

Harbor startup incident: after Docker restarted, Harbor components attempted to initialize before `harbor-log` was accepting syslog on `127.0.0.1:1514`, so they exited with code 128. Once `harbor-log` was healthy, this recovered Harbor:

```bash
cd /data/harbor
docker compose -f /data/harbor/docker-compose.yml up -d
```

## Latest Drill failure diagnosis

Latest failed task:

- Task ID: `c26f00a4-0c12-4e22-8112-05a1951b6c54`.
- Target cluster ID/name: `f78b50ec-8874-4f8b-81c3-99939243517e` / `131`.
- Restore point: `cf14546a-1e3c-42a5-850e-c481a1dc69d8`.
- Source namespace: `demo-mysql-csi`.
- Target namespace: `demo-mysql-csi-drill`.
- Failure stage: `Restoring Persistent Data`, around 20%.
- Error code: `RESTORE_VOLUME_DEPENDENCY_MISSING`.
- Failed PVR: `hcdr-restore-demo-mysql-csi-c26f00a4-qm7wl`.
- Exact underlying error: Kopia restored `ibdata1`, then `chown` failed with `read-only file system`.

Evidence from cluster 131:

- PVC `demo-mysql-data` was bound to Longhorn volume `pvc-e82f5082-f4c8-4e4b-9913-d7d66f7fef02`.
- The workload did not request a read-only mount.
- During the restore, Longhorn manager health checks timed out; CSI provisioner/attacher/resizer restarted; a replica stopped and rebuilt.
- The Longhorn volume later returned to `attached/healthy`, but the restore was incomplete: about 43 MB of 210 MB.
- The replacement MySQL Pod is in `CrashLoopBackOff`; MySQL reports `ibdata1` open error 71 because the restored dataset is incomplete/corrupt.
- Harbor was already healthy for the latest attempt; the web image pulled and ran successfully. This latest failure is storage-related, not an image-pull failure.

Kubeconfigs are outside the main repository:

- `/data/hypercdr/kubeconfigs/config-131`
- `/data/hypercdr/kubeconfigs/config-136`

No cleanup or retry was performed during diagnosis. The recommended next operational step is to remove the failed Drill namespace/PVC/PV only after explicit user authorization, verify Longhorn CSI stability, then retry the Drill on a clean target. Resource deletion is destructive and must not be assumed.

## Recommended next engineering steps

1. Review `git diff` in `/data/hypercdr-main` and commit the migrated Drill/Restore work in coherent commits.
2. Do not commit the `frontend/node_modules` symlink/runtime artifact.
3. Decide whether to harden Drill preflight against Longhorn/CSI instability and provide a more accurate error code than `RESTORE_VOLUME_DEPENDENCY_MISSING` for a read-only filesystem.
4. Fix recovery-stage evidence currently rendering placeholder values such as `<nil>` when fields are absent.
5. With explicit approval, clean the failed `demo-mysql-csi-drill` resources and retest.
6. Keep all generated/runtime state under `/data/hypercdr-runtime`, never inside the repository.

## Safety and operating rules

- Ask for elevated permission when sandbox restrictions block systemd, Docker, Kubernetes, local listener tests, or protected filesystem access.
- Diagnose first; do not delete namespaces, PVCs, PVs, Harbor data, or retry recovery tasks without explicit user authorization.
- Preserve tenant scoping, UTC persistence, rolling-agent compatibility, and shared frontend design tokens per `AGENTS.md`.
