# Restore points in backup results

Successful backup publication stores an optional `restorePointChanges` snapshot
in the task payload, atomically with publication and task completion:

- `created`: the published restore point's `id` and UTC `taskCreatedAt`.
- `deleted`: the quota-replacement restore points confirmed as `deleted`, with
  the same fields. An empty array means no old restore point was deleted.

The final-result panel displays the created point and renders deletion rows only
when this snapshot contains confirmed deletions. Display timestamps use the
selected user timezone. Pending or failed deletion is not reported as successful.
Replay of an accepted result preserves the original snapshot. Older tasks can
display their created point from the task's existing association; deletion is
not inferred from the current restore-point list. No schema migration or agent
protocol change is required.
