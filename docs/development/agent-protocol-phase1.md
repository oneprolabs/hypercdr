# Agent protocol consolidation: phase 1

This implements the first increment of the V2 design while retaining the V1 wire
format. It does not implement V2 attempts/fencing, generated schemas, or a global
message inbox. Those remain follow-up work, not guarantees of this release.

## Ownership

`agent.task.completed` for a backup is adapted by `acceptBackupCompletion` into a
validated result. `PublishBackupResult` is the only live backup publication
boundary. Velero completion notifications and inventory only persist a separate
`backupObservation`; they cannot publish points, complete a backup, reserve more
quota, or overwrite a pending result. Unassociated inventory does not silently
create successful historical tasks. Historical adoption needs a separate
authorized import flow.

An accepted result is persisted as `tasks.payload.backupResult`. Its recovery-point
input and measurements are immutable. Conflicting measurements or backup identity
are rejected. The result is saved before quota cleanup starts. Replacement retries
use that snapshot, not a later inventory or mutable task metric sample. Pending
rows produced by the previous version may use persisted task metrics once when
entering the new publication boundary; this is compatibility for unfinished work,
not a backfill of published points.

## Atomic publication

Under the protection-plan lock, the publisher validates tenant/cluster/task
references, accepts the result, and checks the existing quota reservation. If old
data still exists, it commits the accepted result and remains finalizing. Once
deletion is confirmed, the restore point, task restore-point reference, reservation
publication, completion timestamp, and completion event commit together. A failed
commit leaves none of these partial effects. Concurrent/repeated completion
messages return the same point. Late retries do not resurrect a deleted point.

The generic `CreateRestorePoint` storage primitive remains for existing test and
historical storage callers. Live WebSocket/inventory paths must not call it. This
is enforced by the production call structure and black-box regression tests.

## Measurements and V1 compatibility

The result requires schema V2 logical and new-data measurements with explicit
known flags, a positive logical total, and nonnegative new data. Known zero remains
zero. Legacy V1 completion payloads with explicit final `uploadedBytes`, logical
size, and complete incremental counts are normalized at this one boundary.

Missing/unknown measurements do not produce an available point. The task remains
finalizing with `BACKUP_RESULT_INCOMPLETE` and the Agent receives a retryable error,
so its reliable completion remains in the outbox. This is deliberately not a
silent fallback to zero or success. There is no new repair migration and no
automatic backfill of existing published points. A persistently incomplete legacy
result needs diagnosis; a general result remeasurement/query protocol and bounded
collection policy belong to the next protocol increment.

## ACK and progress

Nonterminal observation receipts omit task/command terminal context. This prevents
already-installed V1 Agents from treating a Velero observation ACK as a terminal
task-result ACK. Updated Agents additionally match the stored outbox message,
message type, task, command, and persisted receipt before acknowledging a result.
An unknown, mismatched, or nonpersisted receipt cannot acknowledge the task ledger.

Terminal states in progress messages are ignored. Late progress/acceptance cannot
regress an accepted or published backup result. Envelope cluster/version checks
and failure-event task/command ownership checks apply before task mutation.
This is not yet the general per-operation V2 transition engine.

## Validation

- Black-box: authenticated loopback WebSocket registration and V1 messages, all
  six completion/Velero/inventory orderings, repeated results, exact retransmission,
  invalid terminal progress, real authenticated task/restore-point HTTP reads.
- White-box: immutable quota replacement across coordinator recreation, complete
  result validation before deletion, eight concurrent publications, result
  conflict, rollback fault injection, and no resurrection after deletion.
- Agent black-box: observation/result receipts over a real WebSocket, read-loop
  barriers, retained outbox after observation ACK and reopening persisted state.
- The same ordering regression was run against checkpoint `3f14ea9` in an external
  runtime checkout: the four observation-first permutations fail because the
  observation publishes a point before the execution result.
- Run `make verify`, targeted race checks, and the protocol regression suite.

Runtime evidence belongs outside the repository under
`/data/hypercdr-runtime/validation/protocol-phase1`. Tests use isolated PostgreSQL
databases and simulated Agents; they do not delete or trigger live backups. A live
cluster rollout or destructive DR rehearsal is not implied by passing these tests.
