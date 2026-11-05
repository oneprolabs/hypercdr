---
title: Tasks and restore points
description: Check the status of backup and restore tasks, diagnose failures, and verify usable restore points under Restore Points.
---

# Tasks and restore points

Entry point: **Backup & Recovery Tasks** (execution records) and **Restore Points** (recoverable points in time).

## Relationship between tasks and restore points

```text
policy schedule → task created → task succeeds → restore point produced
                                             ↘ task fails → no restore point (or partial)
```

A task records the outcome of one run; a restore point marks a point in time you can recover to. The two are not interchangeable.

## Task fields

| Field | Purpose |
| --- | --- |
| Status | Distinguishes success, failure and in progress; for a failure, check the specific stage |
| Time | Compare with the policy schedule time to see whether it is delayed |
| Details | Error message and execution stage, used to locate storage, the agent or resource mapping |
| Logs | The concrete error stack, for deeper troubleshooting |

::: tip Check the task details before the agent logs

Task details usually identify whether the problem is storage access, PVC/StorageClass or resource mapping. Reading the agent logs first tends to bury the cause in unrelated output.

:::

## Common failure types

| Error signature | Meaning | What to do |
| --- | --- | --- |
| Storage access denied | Credentials expired, insufficient bucket permissions, endpoint unreachable | [Object storage](/en/docs/manual/storage) |
| PVC / StorageClass related | The target cluster lacks the corresponding storage class, or dynamic provisioning failed | Check the target cluster's storage capabilities |
| Resource mapping conflict | The target namespace already has resources with the same name | Adjust the recovery target or the mapping |
| `RESTORE_CACHE_PREFLIGHT_FAILED` | The restore cache preflight before the drill did not pass | See [Velero restore cache](/en/docs/operations/restore-cache) |
| Agent timeout | Cluster-side network or load problem | Check the cluster status and the agent logs |

## Restore points

Confirm in **Restore Points**:

- The restore point timestamps correspond to the task records;
- The storage and application associated with the restore point are correct;
- The retention rule has not removed it.

When the restore point count stops growing, first determine whether **tasks are failing** or **the retention rule is removing old points**.

## Recovery prerequisites

::: danger A recovery overwrites the target

Before a recovery, check the **target and resource mapping** to avoid overwriting existing workloads. A recovery changes the target environment.

:::

Recommended order:

1. First verify in a [drill](/en/docs/manual/drill) that the same restore point works;
2. Confirm the target cluster, target namespace and storage class mapping;
3. Confirm that resources with the same name on the target side may be overwritten;
4. Run the recovery and follow the task progress;
5. After recovery, verify the workloads, PVCs and application data.

## Post-recovery checks

A successful task does not mean the application is usable. At minimum check:

- [ ] Workloads (Deployment/StatefulSet and so on) are ready, with the expected replica count
- [ ] PVCs are bound and the data is readable
- [ ] The application can serve traffic
- [ ] The data content matches the restore point time

## Related

- [DR drills](/en/docs/manual/drill)
- [Troubleshooting](/en/docs/operations/troubleshooting)
