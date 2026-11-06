# Plan capacity collection

The control plane schedules one `plan-capacity` task per eligible source cluster
per UTC calendar day. The daily window starts at 00:00 UTC; a stable cluster hash
spreads dispatch over the first five minutes. Offline clusters catch up once when
they reconnect, not once per missed day. Clusters without plans
with a configured storage repository are skipped. Manual refresh collects only
the selected plan and does not change the daily schedule.

The protocol reuses `platform.task.dispatch` and `agent.task.completed` with typed
`planCapacity` command/report fields. Agents advertise `plan-capacity-v1` during
registration. Older agents are not dispatched this command. Upgrade the agent
to stop legacy per-backup plan scans; upgrading the API alone cannot stop scans
performed by old agents.

Each target contains a plan ID, repository identity, backup storage location and
namespaces. The agent serializes capacity tasks and scans namespaces sequentially,
reusing successful repository measurements within the batch. Backup metadata is
filtered by plan backup-name prefix. Kopia measurements include all repository
objects, including indexes, metadata, and data awaiting repository maintenance.
Node-local caches are excluded. Namespace ownership must remain exclusive to a
plan for namespace repository occupancy to represent that plan's storage usage.

The agent checkpoints its most recent completed capacity report in its existing
persistent state directory before sending it through the reliable event outbox.
A redispatch of that task reuses the checkpoint instead of rescanning. Partial
failures are returned per plan. The platform retains previous successful values
on failure and records `status`, `errorMessage`, `lastAttemptAt`, `taskId` and
`requestedAt` in `planStorageSize`; successful results also update `collectedAt`
and the three byte counts. Requests older than or equal to the persisted request
timestamp cannot overwrite newer results. Changed plan repository/namespace
assignments are checked before accepting a result.

Database indexes enforce at most one active capacity task per cluster and one
scheduled task per cluster/day. A running daily task is reused by manual refresh
when it contains the selected plan. An unrelated active capacity task returns a
conflict rather than starting an overlapping scan. Stale tasks expire using the
existing maintenance timeout. Failed daily tasks are not automatically rescanned
on every scheduler tick; manual retry is available.

API:

- `GET /api/v1/protection-plans/{id}/capacity`: persisted size and most recent task.
- `POST /api/v1/protection-plans/{id}/capacity/refresh`: start/reuse a manual task.

Both endpoints are tenant-scoped. Capacity refresh is available in the plan's
namespace detail drawer next to repository usage. UTC timestamps use the shared
display-timezone formatter in the UI. These are daily observations, not live
quotas or cloud-provider billing values. Backup completion still calculates the
individual restore point size, but no longer scans the entire plan repository.
## Initial capacity after the first successful backup

After a successful backup, plans without a valid nonzero capacity measurement
receive one automatic `first-backup` capacity task. A previous zero measurement
of an empty repository does not suppress this collection. Subsequent syncs of
plans with valid capacity do not trigger a scan.

The existing task and reliable report channels are reused. An active collection
containing the plan is reused; offline clusters and busy clusters are retried by
the scheduler. At most one initial task is created per plan, including failed
attempts. Failures retain the previous value and can be retried by daily or manual
collection instead of repeatedly scanning. The UTC 00:00 daily schedule is unchanged.
