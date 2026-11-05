---
title: DR drills
description: Verify in an isolated scope whether a restore point is usable, including restore cache preflight and verification points.
---

# DR drills

Entry point: **Drill**.

A drill **proves, without affecting production, that a restore point can recover a working application**.

A successful task only means the backup flow finished. A drill verifies whether the application can run after recovery.

## Drill and recovery

| | Drill | Recovery |
| --- | --- | --- |
| Purpose | Verify capability | Handle a real incident |
| Scope | Isolated environment | Business target environment |
| Risk | Low, can be repeated | High, may overwrite existing resources |
| Frequency | Regular | On demand |

## Drill prerequisites

- [ ] The cluster is online and the agent is healthy
- [ ] A usable restore point exists
- [ ] The target scope is isolated and will not touch production resources
- [ ] The restore cache preflight passes (see below)

## Restore cache preflight

Before the drill submits the restore, the agent reads the actual ConfigMap and StorageClass to run a preflight:

| Preflight result | Behavior |
| --- | --- |
| Cache not enabled | Records that temporary disk is used, and continues |
| Cache configuration valid | Records the StorageClass, threshold and cache limit, and continues |
| Enabled but configuration unusable | Returns `RESTORE_CACHE_PREFLIGHT_FAILED` before creating the Restore |

A preflight failure cannot be ignored: if Velero fails to create the cache PVC at runtime, it does **not** fall back to temporary disk automatically.

Situations where the preflight can fail: a missing StorageClass, no dynamic provisioner, a `reclaimPolicy` other than `Delete`, or an unparseable ConfigMap. See [Velero restore cache](/en/docs/operations/restore-cache).

## Verification points

Do not rely on the task status label alone. Check in this order:

1. **Workloads**: whether Deployments/StatefulSets are ready, and whether replica counts and image versions are as expected.
2. **PVCs**: whether they bound successfully, with the correct capacity and StorageClass.
3. **Application data**: connect to the application to inspect the data, and spot-check key records or files.
4. **External services**: whether Service/Ingress is usable (verified within the isolated scope).

::: warning Partial success is still a failure

A Velero restore can report "partially complete": most resources restored, a few failed. The task status may look normal, so check the resource inventory item by item.

:::

## Handling drill results

| Result | Meaning | Next step |
| --- | --- | --- |
| Everything passes | Recovery capability verified | Record the timestamp and repeat on a schedule |
| Workloads abnormal | Missing images, configuration or dependencies | Fill the gaps and drill again |
| Data incomplete | A problem with backup scope or consistency | Check the protection scope and storage |
| Preflight failed | Cluster storage capabilities are insufficient | Adjust the StorageClass or cache configuration |

Record the time, restore point and result of every drill. If an incident occurs, this record is the evidence of recovery capability.

## Related

- [Tasks and restore points](/en/docs/manual/tasks)
- [Velero restore cache](/en/docs/operations/restore-cache)
- [Limitations and security recommendations](/en/docs/reference/limitations)
