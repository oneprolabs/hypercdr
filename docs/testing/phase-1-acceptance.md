# Phase 1 Acceptance

This phase proves the first real HyperCDR workflow against two Kubernetes clusters and one MinIO/S3-compatible object storage repository.

## Scope

- Single tenant.
- Two registered clusters:
  - one source cluster;
  - one target cluster.
- Default cluster can be set and persists in the platform database.
- Applications are Kubernetes namespaces.
- Storage is S3-compatible object storage, including MinIO.
- PVC data must be backed up through Velero file-system backup/node-agent to object storage.
- `VolumeSnapshotLocation` and cloud/local block snapshots are out of scope.
- Drill restores default to a new namespace.
- Takeover restores default to the original namespace name.
- Restore to original namespace defaults to skip/no-overwrite unless explicitly changed.

## Required Inputs

The final acceptance run needs:

- Platform host/IP reachable by both clusters.
- Internal image registry host/IP, username, and password.
- Source cluster kubeconfig or admin access for installer execution.
- Target cluster kubeconfig or admin access for installer execution.
- MinIO/S3-compatible endpoint, bucket, access key, secret key, and TLS mode.
- A source cluster namespace selected from the real application list.

## Acceptance Workflow

1. Start platform backend, PostgreSQL, and frontend.
2. Push required images to the internal registry:
   - `comm-agent`;
   - Velero fixed version image;
   - Velero node-agent/helper images if separate.
3. In the platform UI, create install command for source cluster.
4. Run installer against source cluster.
5. In the platform UI, create install command for target cluster.
6. Run installer against target cluster.
7. Verify both clusters are online in the platform.
8. Set one cluster as default.
9. Create MinIO/S3-compatible storage repository in the platform.
10. Sync the storage repository to both clusters.
11. Verify Velero `BackupStorageLocation` is available in both clusters.
12. Create a synchronization policy.
13. Select a source namespace application from the real application list.
14. Create a protection plan with source cluster, target cluster, storage repository, and policy.
15. Start synchronization.
16. Verify source cluster has a Velero `Backup` CR.
17. Verify backup data is written to MinIO.
18. Verify platform task status reaches succeeded and a restore point is created.
19. Start a drill from the restore point.
20. Verify target cluster has a Velero `Restore` CR.
21. Verify resources are restored to the selected target namespace.
22. Verify platform operations show drill terminal result.

## API Checks

- `GET /api/v1/clusters` returns both clusters with correct role/default fields.
- `GET /api/v1/applications?clusterId=<source>` returns real namespaces.
- `GET /api/v1/storage-repositories` returns the MinIO repository without secret values.
- `GET /api/v1/policies` returns the created policy.
- `GET /api/v1/protection-plans` returns the application protection plan.
- `GET /api/v1/tasks` shows backup and drill tasks with real terminal states.
- `GET /api/v1/restore-points` returns a restore point after backup success.

## Kubernetes Checks

Run against both clusters as applicable:

```bash
kubectl get ns hypercdr-agent
kubectl get deploy -n hypercdr-agent
kubectl get backupstoragelocations.velero.io -n hypercdr-agent
kubectl get backups.velero.io -n hypercdr-agent
kubectl get restores.velero.io -n hypercdr-agent
```

The target cluster must contain the restored namespace and expected workload resources.

## UI Checks

- The `3002` platform UI is the design baseline.
- Pages use real API/database data for the acceptance workflow.
- Visual comparison against the `3001` prototype can be run with:

```bash
cd /data/hypercdr/frontend
npm run visual:compare
```

Screenshots are written to `/tmp/hypercdr-visual-diffs`.
