---
title: Velero restore cache
description: Cache policy during the restore phase, default thresholds, configuration change requirements, and how to handle a failed preflight.
---

# Velero restore cache

HyperCDR configures the Velero 1.18.2 Data Mover restore cache automatically when a cluster is registered. The cache is used **only** in the **restore phase** of CSI Snapshot Data Movement and File System Backup, never for backup uploads.

## Default policy

- You do not choose PVCs, paths or StorageClasses on the DR Config or Drill pages.
- The installer prefers the default StorageClass determined at registration, but enables the cache **only when a dynamic provisioner exists and `reclaimPolicy=Delete`**.
- When those conditions are not met, no `cachePVC` is configured and Velero uses the ephemeral disk of the Data Mover Pod.
- `residentThresholdInMB` defaults to `1024`; restores smaller than that do not create a cache PVC.
- Kopia `cacheLimitMB` defaults to `5120`. Velero adds 20% and rounds up, so it typically requests a 6 GiB ephemeral PVC.
- Velero creates the PVC dynamically for each `DataDownload` or `PodVolumeRestore` and cleans it up in the terminal state; HyperCDR does not pre-create a shared PVC.

## Two configurations the Node Agent reads

The Node Agent reads both:

- `--node-agent-configmap=node-agent-config`
- `--backup-repository-configmap=backup-repository-config`

::: warning Roll the Node Agent after a configuration change

After a configuration change you **must roll the Node Agent**. Velero 1.18.2 does not hot-reload the cache configuration read at controller start-up.

:::

## Preflight before a drill

Before submitting the Velero Restore, the agent reads the actual ConfigMap and StorageClass:

| Situation | Behavior |
| --- | --- |
| Cache not enabled | Record that ephemeral disk is used and continue |
| Cache configuration valid | Record the StorageClass, threshold and cache limit and continue |
| Enabled but the StorageClass is missing | Return `RESTORE_CACHE_PREFLIGHT_FAILED` before creating the Restore |
| Enabled but there is no dynamic provisioner | Same as above |
| Enabled but `reclaimPolicy` is not `Delete` | Same as above |
| Enabled but the ConfigMap cannot be parsed | Same as above |

::: danger A failed preflight cannot be ignored

Velero **does not fall back to ephemeral disk** when it fails to create the cache PVC at runtime. A preflight failure must therefore be handled explicitly.

:::

Cluster advanced settings can adjust the StorageClass and the cache limit later; the restore action in the business UI does not offer a cache option, because the cache is a cluster-level setting and cannot be changed per restore.

## Related

- [DR drills](/en/docs/manual/drill)
- [Cluster registration and management](/en/docs/manual/clusters)
- [Troubleshooting](/en/docs/operations/troubleshooting)
