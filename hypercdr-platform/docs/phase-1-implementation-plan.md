# Phase 1 Implementation Plan

This plan implements the accepted phase-1 workflow incrementally, with tests after each slice.

## Milestone 1: Platform Data Foundations

- Persist cluster role and default cluster in PostgreSQL.
- Add cluster update/default APIs.
- Remove acceptance-path dependency on frontend demo data.
- Add platform config for public endpoint, internal image registry, agent namespace, Velero version, and secret encryption key.

Verification:

- Backend tests pass.
- Cluster role/default survive restart.
- Frontend can set and read default cluster through API.

## Milestone 2: Installer And Image Packaging

- Add `comm-agent` Dockerfile.
- Add image build/push scripts for internal registry.
- Generate installer script that installs:
  - `hypercdr-agent` namespace;
  - Velero CRDs and deployment;
  - Velero node-agent/file-system backup support;
  - comm-agent Secret/Deployment/RBAC.
- Installer must accept registry, endpoint, token, namespace, and image tags.

Verification:

- Installer can install into a clean test cluster.
- comm-agent registers and persists platform credential.

## Milestone 3: Real Storage Repository Sync

- Extend storage repository model for S3-compatible credentials.
- Store secrets without API readback.
- Dispatch storage sync to selected clusters.
- comm-agent writes Kubernetes Secret and Velero BackupStorageLocation.
- comm-agent polls BackupStorageLocation state and reports result.

Verification:

- MinIO repository can be created in UI.
- BSL becomes Available in source and target clusters.

## Milestone 4: Policy And Protection Plan Completion

- Ensure policy CRUD and protection plan CRUD persist real records.
- Bind namespace app, source cluster, target cluster, storage repository, and policy.
- Frontend flow must refresh from real API data.

Verification:

- Selected namespace becomes protected after refresh.
- API returns complete protection plan relationship.

## Milestone 5: Backup Task Closed Loop

- Implement task engine dispatch/re-dispatch for non-terminal tasks.
- comm-agent creates Velero Backup CR with file-system/PVC backup support.
- comm-agent polls Backup status every 3-5 seconds.
- Platform persists task events and creates restore point on success.

Verification:

- Backup CR succeeds.
- MinIO contains backup data.
- Platform creates restore point.

## Milestone 6: Drill Restore Closed Loop

- Create drill task from restore point.
- Dispatch restore to target cluster agent.
- comm-agent creates Velero Restore CR.
- Support original namespace and new namespace modes.
- comm-agent polls Restore status and reports terminal result.

Verification:

- Restore CR succeeds.
- Target namespace contains restored resources.
- UI operations show drill result.

## Milestone 7: Acceptance Automation

- Add `tools/phase1_e2e.sh` or equivalent.
- Automate API checks and kubectl checks where inputs are available.
- Run frontend build, backend tests, agent tests, and visual comparison.

Verification:

- Full phase-1 acceptance workflow can be repeated with minimal manual steps.

