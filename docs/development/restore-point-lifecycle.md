# Restore point retention and quota replacement

## Scope and acceptance criteria

The protection-plan policy's `retentionCount` is a hard maximum, not an
unprotected-point allowance. Manual, policy and latest-successful-drill retention
all count toward it. Multiple reasons on one point consume one slot. Storage-byte
quotas are not part of this feature.

Acceptance requires isolated PostgreSQL tests covering rolling calendar rules,
replacement rollback/publication, concurrent reservations, retained-point deletion
guards, tenant isolation, and successful-drill protection transfer; frontend
tests, lint, build and browser checks cover optional rules and snapshot actions.
Do not validate by triggering a production drill or deleting live backup data.

## Optional policy rules

`retentionRules` defaults to an empty array. With no rules, oldest eligible points
are replaced when the count quota is full. Each rule specifies `period` (`daily`,
`weekly`, `monthly`, `yearly`), positive `ordinal`, and positive `keep`.

Periods use the persisted IANA `retentionTimezone`; weeks begin Monday. The current
period counts toward the rolling window. Daily point #1 / keep 3 on October 26
selects October 24, 25 and 26, not three additional days after October 26.
Candidates are successful points ordered by backup-task start time, with ID as a
deterministic tie-breaker. Missing periods are not backfilled. Deleted successful
point tombstones retain their ordinal, so deleting the first point does not
silently promote the second to first. Rule results form a union and expire
independently; expiration removes a reason rather than immediately deleting data.

## Retention reasons and mutations

`manualRetained` is explicitly set/released through
`PATCH /api/v1/restore-points/{id}/retention`. `drillRetained` moves atomically
within a protection plan only when another drill succeeds. Failed/canceled
drills and successful backups do not move it. Replayed terminal notifications
must not move protection back to an older task. Other reasons remain intact.

Automatic cleanup respects all reasons and active recovery references. A user may
explicitly confirm deletion of a policy-selected point, but must release manual
retention first. Drill protection and active references cannot be bypassed by the
manual delete endpoint. Replacement points cannot be newly retained, deleted or
used for another recovery task. Source protection cleanup also validates retained
points before destructive dispatch.

Namespace details expose the restore-point tab in all application stages. The
tab shows active restore points, retention reasons and replacement state, and
offers confirmed retain/release/delete actions. If there is no current plan,
the lookup is scoped to the application's ID and cluster, never an unscoped list.

## Durable quota replacement

Replacement publication takes the latest final size metrics from the parent
backup task, not only the earlier Velero completion snapshot. Inventory updates
without metrics must not overwrite existing measurements. Migration 000046
repairs missing measurements from persisted backup-task results.

`restore_point_reservations` persists the association between a backup task and
either a free slot or a replacement point. Transactions lock the protection plan
to serialize quota changes, task admission, recovery references and publication.
A unique partial index prevents two backups reserving the same old point.

1. Free quota is reserved before dispatch. At full quota, the oldest eligible
   point is marked `replacementTaskId`; it still exists and counts toward quota.
2. If all points are retained/in use, a failed backup task with
   `RESTORE_POINT_QUOTA_EXHAUSTED` is recorded without dispatching data transfer.
3. Transfer failure/cancellation while `reserved` releases the old point's marker.
4. Successful transfer persists the complete new point input as `pending_point`
   and moves to `data_ready`. It does not yet publish a usable point.
5. The scheduler creates a deletion task and atomically records its ownership
   before agent dispatch. Retention is rechecked at deletion-task creation.
6. Only confirmed deletion changes the old point to `deleted`. Its freed slot
   remains reserved for the waiting publication, including across process restart.
7. Publishing the new point converts the reservation to `published` atomically.
   The scheduler retries task finalization after a crash between publication and
   completion notification. Publication is idempotent by source cluster and
   Velero backup name.

Deletion failures keep the new data and old-point association. Retry delay grows
from 60 seconds to a maximum of 32 minutes, using durable cleanup-attempt counts.
Agent-offline tasks stay queued. No deletion request is treated as confirmed
deletion, and no uncertain deletion is rolled back to an unlocked usable point.
The parent remains finalizing with an explanatory quota-cleanup warning. Once
data is ready, ordinary cancellation is rejected rather than interrupting an
irreversible replacement. Temporary data may require additional physical storage;
this does not represent an extra published restore point or a capacity quota.

## History and migration

The additive migration supplies empty rules / UTC for existing policies, adds
reservation persistence, backfills recovery-task restore-point timestamps and
marks available points referenced by the latest successful drill. Historical
recovery labels resolve against all loaded points, not only currently available
ones, and fall back to the task's persisted timestamp, then its point ID.

## Velero ownership and rollout

Velero TTL garbage collection must not override platform retention. The bundled
Velero GC reconciler skips backups carrying both
`hypercdr.io/managed-by=hypercdr` and a nonempty `hypercdr.io/plan-id`.
Non-HyperCDR backups retain upstream TTL behavior; explicit DeleteBackupRequests
remain effective. This works for existing managed backup labels as well as newly
created backups.

Rollout therefore requires the matching API, frontend and patched Velero image,
not just a frontend refresh. Verify backup labels and the Velero image before
enabling long-duration retention in an environment. External object-store
lifecycle rules or manual Kubernetes/storage deletion remain outside this logical
protection and must not expire managed backup objects. This is not WORM storage.

Build and validate outside the source tree. Do not apply migrations to the live
platform database, change cluster images, or deploy without explicit approval.
