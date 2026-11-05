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

## Progress and events {#task-events}

Task details carry two kinds of information: **progress** (percentage, duration, start and end times) and the **event timeline** (what happened at each step). Diagnose from the timeline — the percentage is only an overview.

![Progress, duration and start/end times in task details](/images/user-guide/task-summary.png)

The progress in this screenshot comes from an already completed task, so it is not a live example. Three things are easy to misread:

- **Cluster is the cluster that actually ran the task**, which may differ from the source cluster selected on the page — especially during cross-cluster recovery;
- **Started / Completed are shown in your selected timezone**, which may not match the cluster node's local time;
- When recording a problem, keep the task ID, type and time together, and do not splice events from different tasks.

### Reading the events

![The event timeline of a completed drill task](/images/user-guide/drill-events.png)

A recovery task roughly emits events in this order:

```text
storage preflight → dispatched to the agent → agent accepted → waiting for recovery → workload readiness check → completed
```

| Event | Meaning | Usable as proof of success |
| --- | --- | --- |
| `accepted` | The agent received the task; recovery has not started | No |
| `workload readiness changed` | The application readiness check changed | No — judge it together with pod and replica counts |
| `completed` | The task reports resources restored and the application ready | Only for this restore point and this target |

Historical events do not disappear when a task eventually succeeds. Seeing `ContainersNotReady` followed by Ready is a normal progression. Judge from later events and the final result, never from a single old waiting message.

### What each of the four detail views shows

| Entry | What to read |
| --- | --- |
| **Recovery Details** (DR → Application DR → Recovery Status) | The full recovery stage timeline; expanding a workload event shows the pod, node and waiting reason |
| **Sync Details** (Sync Status on the same row) | Sync stages, the latest restore point and the next schedule |
| **DR Configuration Details** | Storage preparation failures and retries |
| **Task Details → Task events** | Platform, agent, Kubernetes and backend events |

::: tip Task events are not raw component logs

These events stop at "which step failed and what it reported". For resource-level troubleshooting, read the comm-agent, Velero or node-agent output in [diagnostic logs](/en/docs/operations/logging). `Technical payload` is technical data — not an operating procedure, and not something to paste without redaction.

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

![Timestamps, repositories and sizes in the restore point list](/images/user-guide/restore-points.png)

| Column | How to use it |
| --- | --- |
| Namespace | The namespace of the protected application; check it against the protected target before recovering |
| Restore Point | Display name; timestamps use your selected timezone |
| Repository | The storage repository holding the backup; it must still be reachable at recovery time |
| Original Size | The original data size the page obtained — not the actual object-storage consumption |
| Storage Size | Shows `Unknown` when the increment is unavailable; `Unknown` is not zero and does not by itself mean the backup failed |
| Recovery Task | The associated recovery task and its state; having no recovery task does not make the restore point unusable |

The screenshot shows part of the list — scroll horizontally for the columns further right. Before selecting a restore point, check the source cluster, namespace, time and repository; do not judge from the most recent row alone.

## Deleting restore points

Entry point: **DR → Restore Points → More → Delete Restore Points**.

Deleting sends a Velero backup deletion request to the source cluster; it is not just hiding a row in the platform. Once deleted, that point in time can no longer be recovered.

### When deletion is acceptable

- Retention, audit and business recovery requirements have been confirmed and deletion is authorised;
- The source cluster's agent and storage are reachable;
- The restore point is no longer needed for current recovery or drill plans.

### Steps

1. Open **DR → Restore Points**, confirm the source cluster and select the restore points to delete.
2. Open **More → Delete Restore Points** and check the selected count, restore point names, backup identifiers and timestamps in the confirmation window.
3. If you are not deleting, select **Cancel**. Select **Delete from Velero** only once authorisation and scope are confirmed.
4. **Velero deletion is running** means the request is still executing — wait for the result and do not click repeatedly.
5. Check the task result and refresh the list to confirm the expected restore points are gone. On failure, keep the error and task identifier before investigating.

![The restore point deletion confirmation window](/images/user-guide/restore-point-delete.png)

### Fields in the confirmation window

| Item | Meaning |
| --- | --- |
| Selected Restore Points | The restore points in this batch; check each one before a bulk delete |
| Velero backup identifier | The identity of the associated backup on the source cluster, used for checking and troubleshooting. Long identifiers are truncated in the list — compare them together with the restore point timestamp, not just the first few characters |
| Delete from Velero | Actually submits the deletion request; it is not clearing filters or deselecting |
| Submitted / running / completed | Deletion is asynchronous: a successful submission does not mean the backup data is already gone |

::: danger Do not substitute client-side cleanup for platform deletion

If deletion fails, do not delete objects from the bucket by hand as a workaround. Platform records and backup data will then disagree, and later troubleshooting, drills and audits will all be misleading.

:::

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
- [Formal takeover](/en/docs/manual/takeover)
- [Troubleshooting](/en/docs/operations/troubleshooting)
