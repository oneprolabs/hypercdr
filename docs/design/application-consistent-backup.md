# HyperCDR application-consistent backup

> Superseded: the user explicitly removed application stop/freeze coordination.
> The current implementation scope is CSI crash-consistent snapshot data
> movement, documented in `csi-crash-consistent-backup.md`. The proposal below
> is historical and must not be used to deploy application coordination.

## Status and scope

Design approved for implementation by the requested goal on 2026-11-25.
This document is a design, not a statement that these guarantees are deployed.
Initial qualification targets isolated Linux filesystem PVC applications on
the test environment. No production workloads or host 47 are in scope.

### Accepted goal refinement: backup selection and user-visible guarantees

The original application-consistency objective remains required in full. The
user additionally approved the following user-facing acceptance criteria:

1. Keep the five-step Protect applications wizard. Name step one Protection
   (formerly Scope), with the other step names unchanged. In this first step, show
   backup method and application consistency settings immediately after source
   scope, so unsupported protection is visible early. Step three Storage remains
   dedicated to the backup repository. This supersedes the earlier step-three
   placement proposal.
2. Default new plans to CSI snapshot data movement. Detect support for all source
   volumes, show loading/unsupported/error states truthfully, and provide specific
   prerequisite/configuration guidance. Unknown capability is not supported.
3. Never install shared snapshot infrastructure automatically or downgrade on
   preflight/execution failure. When CSI cannot be supported, allow an explicit
   live-filesystem selection only with a visible consistency warning and recorded
   user acknowledgement. Keep existing plans and legacy FSB restores compatible.
4. Distinguish the backup method from the consistency guarantee. CSI alone is
   snapshot/crash consistency. Application consistency requires a qualified,
   explicitly configured coordinator, complete volume/workload scope and passing
   execution evidence. Show the service-pause implications before confirmation.
5. Persist and round-trip settings through create/edit/detail/review and immutable
   task snapshots. Recheck at execution without asking for silent downgrade.
   Tasks and restore points display the actual method, requested and achieved
   guarantee, and relevant failure/recovery evidence; no unsupported success label.
6. Verify these UI flows in the real 149 browser, including supported CSI,
   unsupported/unknown capability, configuration guidance, acknowledged FSB,
   consistency enabled/disabled and edit preservation. Require affected backend,
   Agent and frontend tests/builds plus the original isolated backup/restore,
   cross-volume and failure/timeout/disconnect safe-thaw acceptance.

The runtime goal remains active. Goal-tool status changes are not used to replace
or prematurely complete the original objective. Missing prerequisites or failed
acceptance remain explicitly incomplete. Do not operate host 47, modify existing
business workloads, or automatically commit/tag/publish.

## Findings

Current manual backups and schedules set `snapshotVolumes=false` and
`defaultVolumesToFsBackup=true`. The platform saves script fields but dispatches
an empty HookSet. Current backups therefore do not establish an application
consistency boundary. Existing Pod/PVC readiness checks are not integrity tests.

Velero 1.18 supplies CSI snapshot data movement, using Kopia to read temporary
volumes created from snapshots. This is distinct from live filesystem backup.
Backup hooks execute around ItemBlocks, not around an arbitrary application
spanning namespaces. A successful hook command alone is insufficient evidence
of a persistent database lock: session-scoped locks can disappear when the
command exits. VolumeGroupSnapshots group compatible PVCs in the same namespace
and CSI driver; they provide storage crash consistency, not application semantics.

References checked against the vendored source and official 1.18 documentation:

- https://velero.io/docs/v1.18/backup-hooks/
- https://velero.io/docs/v1.18/csi-snapshot-data-movement/
- https://velero.io/docs/v1.18/csi/
- https://velero.io/docs/v1.18/volume-group-snapshots/
- `third_party/velero/pkg/backup/backup.go`, `backupItemBlock`
- `third_party/velero/pkg/backup/actions/csi/pvc_action.go`

## Guarantee levels

Store requested and achieved levels separately on every restore point:

| Level | Guarantee |
| --- | --- |
| live-filesystem | Compatibility path; no shared point-in-time guarantee |
| snapshot-crash-consistent | Stable snapshots; independent volumes may have different capture times |
| application-consistent | Qualified application strategy holds all writers coordinated until every required snapshot is secured |

Restore validation is a separate result: not-run, passed, or failed. A backup
must never obtain an application-consistent label merely because it completed,
used snapshots, executed a command, or subsequently started Pods.

## Chosen architecture

### User-facing selection and explicit fallback

New-plan UI should recommend CSI snapshot data movement by default and show the
selected data path separately from the requested consistency guarantee. Existing
FSB plans must not be silently migrated. Before accepting a new CSI plan, inspect
snapshot APIs/controller, the matching CSI driver and VolumeSnapshotClass, and
every selected PVC. Show actionable missing prerequisites rather than silently
switching to live filesystem backup. Installing shared snapshot infrastructure
requires cluster-owner approval; Agent installation alone must not take it over.

If CSI cannot be supported, the user may explicitly select live-filesystem
compatibility mode and acknowledge that reading changing files does not guarantee
application data consistency. CSI without qualified application coordination is
only snapshot/crash consistency, not application consistency. A requested
application-consistent run must fail closed on unsupported volumes, quiesce or
snapshot errors. Task and restore-point views must show the actual data path,
requested/achieved guarantee and retained evidence. This UI policy is a design
requirement, not a claim that the current frontend implements it.

Place these controls in the existing Protect applications wizard's first step,
Scope: resource selection first, backup method and source-volume capability
results second, application coordination configuration third, with advanced
settings expanded on demand. Repository selection stays in step three Storage;
schedule and retention stay in step four. Review
must summarize the selected method, requested guarantee and pause budget. Persist
the selection on the plan, recheck capabilities before execution, and do not ask
for an execution-time downgrade or silently change existing plans.

### Coordination

Use Velero's built-in CSI snapshot data mover, not a custom Kopia repository
implementation. Add a durable cluster-local consistency coordinator. Its work
must survive platform websocket loss and Agent restart; a websocket observer
must not own a database lock or the only thaw mechanism.

The unit of coordination is an explicit application consistency group, carrying
namespace-qualified PVC and workload identities. Independent groups can run
separately. A multi-namespace application uses one application-level write
barrier, even when its underlying snapshots are independent.

Prefer qualified application-native online backup adapters where available.
The initial general-purpose strategy is controlled offline capture: explicitly
quiesce all writers, gracefully stop selected controllers, verify their Pods
have terminated and no unapproved Pod mounts the required volumes, snapshot,
then restore the original replica counts. This has a measurable service pause
and is opt-in, not a universal zero-downtime solution. Operator-managed resources,
external writers, unsupported controllers and shared RWX writers fail preflight
unless a qualified adapter accounts for them. Do not silently scale arbitrary
business workloads to zero.

App-specific quiesce/resume steps execute against explicit namespace, workload,
container and argument arrays, with bounded timeouts. Credentials are secret
references, not script content or task-log values. Arbitrary legacy scripts
remain custom hooks and do not qualify as a tested consistency adapter.

## Required conditions

- Working CSI v1 snapshot APIs, snapshot controller and matching driver/class.
- Successful probe of snapshot creation, readiness and temporary PVC provisioning.
- Velero CSI feature and node-agent; object-store plugin and repository access.
- Available temporary storage, scheduling resources, mount propagation and
  permitted data-mover privileges. Do not stress the previously overloaded130.
- Every required PVC bound and accounted for; no implicit exclusions or FSB
  annotations/policies that can silently change the selected data path.
- Verified adapter support for application/version and all write producers.
- Original workload state recorded before mutation, namespace-scoped permissions,
  durable lease, recovery worker and maximum pause deadline.
- VGS is optional, never assumed from single-volume snapshot support. Probe its
  CRDs, controller and actual driver support separately. It does not solve
  cross-namespace or cross-driver atomicity.

## State machine

The application write pause covers quiescence and securing every required
snapshot, not the full transfer. Once the entire group's snapshots are safely
readable and their identities are verified, restore writes immediately. Kopia
then transfers immutable snapshot data while the application runs. In the initial
offline test strategy this restoration includes restarting the stopped Pod;
qualified online adapters should release their write barrier without a Pod
restart. Show application pause/recovery time separately from backup duration.

The snapshot barrier accepts an owned temporary mover PVC in Pending or Bound
state only after the corresponding rebound VolumeSnapshot and Content are both
ready with matching identities, driver, class and immutable handle. Binding the
temporary PVC is transfer preparation, not a prerequisite for releasing source
writes. Missing ownership, an unknown/lost PVC state, unready snapshots or invalid
identities still fail the barrier. Final success still requires completed
DataUploads and durable Kopia snapshots for all volumes. Unit tests cover Pending
with ready snapshots as well as unready snapshots, invalid ownership/source,
unknown and Lost temporary-PVC states. Live behavior is recorded below; no
meaningful latency improvement is claimed from one comparison.

`Preflight -> AcquireLease -> RecordOriginalState -> Quiesce -> VerifyBarrier ->
CreateBackup -> CaptureSnapshots -> VerifySnapshotBarrier -> Resume ->
UploadSnapshots -> VerifyBackup -> PublishRestorePoint -> Cleanup`

1. Persist run identity, policy revision and exact PVC UID set before mutation.
2. Prevent overlapping runs or conflicting mutations of the same group.
3. Ensure the independent recovery worker is healthy before permitting quiesce.
4. Quiesce all writers in dependency order; prove the barrier, not just exit code.
5. Use `snapshotVolumes=true`, `snapshotMoveData=true`,
   `defaultVolumesToFsBackup=false`. Reject any required volume that resolves to
   a skipped snapshot or live filesystem path.
6. Match every expected PVC UID to this run's snapshot and secured data-movement
   source. Snapshot request creation or a snapshot handle alone is not enough.
   Conservatively require ready-to-use source evidence and retention until the
   mover has safely taken ownership. Handle Velero snapshot object lifecycle
   changes without losing the recorded evidence.
7. Resume once all required snapshots are secured; never wait for object-store
   upload merely to resume business writes. Upload may continue for hours.
8. Publish only after all required DataUploads and the Velero backup succeed,
   with full inventory and consistency evidence. Any partial volume failure
   prevents an application-consistent restore point from being published.

Captured resource metadata must represent the original operational workload,
not temporary zero replicas or maintenance mode. If offline capture alters live
resources before Velero archives them, preserve authoritative original specs
in the backup evidence and restore them using a qualified restore transform.
Do not publish a backup until that recovery path is implemented and tested.

## Failure and recovery

Default maximum write pause is configurable and short (initial default120s),
with a separate bounded snapshot timeout. Slow providers require explicit
qualification, not an unlimited pause. No silent downgrade to FSB.

On timeout, cancellation or failed snapshot, invalidate the attempt, prohibit
late capture from becoming a consistent point, and resume the application.
The independent recovery worker uses persisted original state and idempotent
steps to recover after process loss. Retry resume and alert until restoration
is verified; do not call the run successful while resume is unresolved.
Protect against stale workers using fencing/run IDs and compare-and-set
ownership. Recovery must never overwrite legitimate later user changes.

On total cluster outage, automatic resume is not guaranteed; recovery reconciles
on return. State this boundary explicitly. Test controller/API loss independently
from websocket loss. Preserve minimum recovery RBAC even after task cancellation.

## Platform integration

Add versioned protection-plan configuration, task protocol and restore-point
evidence. Reject application-consistent dispatch to Agents lacking an explicit
capability; old Agents must not silently ignore a new optional field. Manual and
scheduled backups use the same policy and coordinator. Legacy schedules must not
bypass coordination; strict mode cannot use an unattended Velero schedule unless
it invokes the qualified coordinator. Keep old restore points readable.

UI shows supported/unsupported reason, selected strategy, expected service pause,
all PVCs and writers, requested/achieved guarantee, snapshot capture window,
resume result and separate integrity validation. Changing existing plans is
explicit; do not silently reinterpret historical backups as consistent.

## Qualification and acceptance

All component tests and builds must pass. Isolated integration evidence must
demonstrate:

1. Continuous write workload with transaction IDs/checksums across two volumes;
   recovered data satisfies the application invariant at the captured boundary.
2. Writes resume before a deliberately slow upload finishes; restored data does
   not contain later writes. No live PVB path for required PVCs.
3. Two namespaces coordinated as one application; namespace-qualified ownership
   and expected PVC UIDs all match.
4. Failed quiesce, missing snapshot class, one-of-many snapshot failure, expired
   pause lease, cancellation, websocket disconnect and coordinator restart never
   publish a false consistent point or leave writers paused indefinitely.
5. Restore original replica counts and remove temporary maintenance state; check
   actual application integrity, not only Pod readiness.
6. Restore a pre-existing FSB backup without migration or format breakage.

Until these pass, implementation remains experimental and disabled by default.
Do not advertise general application consistency or deploy it to business plans.

## Implementation checkpoint

Implemented foundations, not yet an operational end-to-end feature:

- Protocol mode field shared between backend and standalone Agent.
- Manual and scheduled manifest builders preserve legacy FSB defaults and can
  explicitly generate CSI snapshot data movement settings.
- Uncoordinated application-consistent requests fail closed.
- Read-only PVC UID, bound PV ownership and CSI snapshot-class driver preflight.
- Coordinator core orders preflight/recovery arming/quiesce/capture/resume/upload
  and rejects incomplete snapshot inventories. Cancellation does not cancel
  the bounded recovery context.
- ConfigMap-backed journal persists records in the cluster with resource-version
  checks. A second writer cannot create the same run, and a stale loaded writer
  cannot overwrite a newer record. This is record-level optimistic concurrency,
  not yet a cross-run application-group lease or action-level fencing.
- Expired-run recovery invalidates capture and resumes without retrying capture
  or upload; live pause leases are not preempted. The concrete journal and
  recovery scan are implemented. Corrupt journals are reported without skipping
  other runs. Records with missing deadlines are rejected.
- The `comm-agent consistency-recover --namespace <agent-namespace>` command
  runs independently of platform authentication/websockets, retries scans after
  API errors and supports `--once` for explicit diagnostics. It is not deployed
  or armed automatically yet.
- Offline recovery supports explicitly recorded Deployment/StatefulSet replicas
  with the `offline-v1` strategy, UID and run-ownership checks plus resource-version
  tested JSON patches. It refuses external replica edits, retries pending
  readiness, and continues restoring other workloads if one fails. It does not
  establish quiescence; the capture-side adapter and original-state recording
  still need implementation and live qualification.
- Group and volume ConfigMap locks prevent overlapping groups and cross-group
  access to the same PVC identity. Locks are not automatically stolen at a
  deadline. Release requires verified resume, checks run ownership and uses
  object UID/resource-version delete preconditions. Failed partial acquisition
  attempts roll back their acquired locks; API failures remain fail-closed.
- Recovery scans release resumed runs' locks and persist that cleanup outcome;
  missing locks are safe to retry. Application-group locks still need wiring
  into the capture-side adapter before any workload is paused.
- Optional recovery heartbeat publication (`--heartbeat-name`) occurs only after
  a successful scan. Capture-side checks reject missing, stale, incompatible or
  invalid timestamps and refuse unrelated ConfigMaps. Empty one-shot scans
  without that option remain read-only. The heartbeat is a readiness gate, not
  a claim that the worker cannot fail after arming.
- Snapshot barrier reader verifies backup UID and strict CSI settings, current
  source PVC UID, unique DataUpload ownership and source namespace, built-in
  mover readiness, relocated snapshot/content binding, matching driver/handle,
  and the bound temporary PVC's DataUpload owner and snapshot data source.
  Velero's exposer replaces the original source-namespace snapshot objects with
  objects named after the DataUpload in the Agent namespace; the reader follows
  that actual lifecycle rather than waiting on deleted original references.
  Completed uploads require a durable Kopia repository snapshot ID and do not
  depend on already-cleaned temporary snapshot objects. These are read-only
  evidence checks, not yet wired to a live capture adapter.
- Focused and race tests cover these foundations; they do not substitute for
  live backup/restore qualification.
- Read-only offline preparation now records actual replicas, resource version
  and generation for explicitly opted-in Deployment/StatefulSet identities.
  It rejects replacement/deleting objects, existing consistency owners,
  operator-owned controllers, HPA targets and duplicate inventory entries.
  This is not yet a complete writer inventory or proof of quiescence.
- Partial-stop recovery accepts an untouched controller only when its recorded
  UID, original generation and replicas still match and no run owns it; it
  fences it with a run-specific resumed annotation without changing replicas.
  That annotation and resource-version-tested pause patches prevent a delayed
  pause from winning after recovery has finished. Changed or missing generations remain
  fail-closed, and other workloads are still resumed independently.
- Read-only Pod writer discovery now checks every Pod referencing a selected
  PVC in its namespace, including terminating and completed Pods. It resolves
  Deployment ownership through the current ReplicaSet UID and rejects standalone
  Pods, Jobs and controllers absent from the explicit workload inventory.
  Direct StatefulSet ownership is also supported. Owner namespace, API version,
  kind, name and UID must match. This check is not action fencing: it must be
  repeated around quiescence and cannot establish absence of external storage
  clients, future Pods or unmanaged writes outside the Kubernetes API.
- The concrete offline pause operation now checks the exact persisted quiescing
  checkpoint, current exclusive locks, independent recovery heartbeat and a
  remaining deadline of at most 120 seconds before each controller mutation.
  It repeats opt-in/original-state and Pod writer checks, then atomically records
  run ownership and scales to zero using UID, resource version, generation and
  annotation JSON tests. Scaling alone is not proof of graceful termination or
  absence of writers. Drain verification now requires observed zero replicas
  and absence of every Pod referencing selected PVCs, including terminating
  and completed Pods. This still does not prove graceful application shutdown.
  The capture adapter remains pending.
- The user approved snapshot component installation in test cluster 123 and
  pauses of at most 120 seconds only for newly isolated test applications.
  No existing application or cluster 47 is authorized for mutation. Snapshot
  CRDs and an isolated controller deployment/class were installed; the controller
  image pull initially timed out. Two isolated 256Mi test PVCs are Bound.
- The snapshot controller is now Running using a digest-pinned mirror image.
  A real CSI snapshot of the isolated ledger PVC became ready and a new PVC
  created from it became Bound. This proves this storage path's snapshot/clone
  capability, not application consistency or repository backup integrity.
- A separately deployed recovery worker is active with permissions scoped to
  the isolated test namespace. Its test binary was checksum verified at load.
  The opt-in live test paused only the fixture writer, observed all volume user
  Pods gone and allowed the independent worker to recover the expired run.
  It restored the original replica count and readiness, released locks and kept
  the interrupted capture Failed. The test passed in 40.78 seconds. Writer logs
  verified graceful stop at sequence 938 and restart validation at sequence 938.
  Evidence: `live-expired-recovery.log`, `live-writer-graceful-stop.log` under the
  runtime research directory. This is not yet a live backup/restore test.
- Upload now returns durable evidence to the coordinator instead of only an
  error. Completion requires unique per-volume Kopia repository snapshot IDs
  tied to the same DataUpload identities observed at capture. Missing volumes,
  CSI-only evidence and changed upload identities cannot publish completion.
  Upload failure checkpoints use a fresh bounded context after long transfers.
- Restore manifest construction accepts an optional verified completed offline
  record. It preserves existing restore modifiers and adds original-replica
  rules scoped to source namespace, exact controller name and run ownership.
  The rule uses annotations rather than the UID that Velero strips before
  modifiers, and clears stale run ownership while preserving other metadata.
  The Agent-generated fixture was exercised by the actual pinned Velero modifier
  implementation, including numeric-looking run IDs and nonmatching identities.
  Dispatch/persisted backup evidence lookup is not integrated yet; this is not
  an enabled restore path for users.

- The first coordinated live backup failed closed at its 70-second pause
  deadline; the independent worker restored the source writer and it validated
  sequence 5757 on restart. Ready snapshots and Bound temporary PVCs were
  already available, but waiting for DataUpload Prepared unnecessarily coupled
  the pause to data-mover Pod attachment. The barrier now also permits Accepted
  only after the same full snapshot/content binding, driver, source identity,
  and owned Bound temporary-PVC checks. Both phases run all negative barrier
  tests. Upload still requires durable repository evidence before completion.
  This correction alone does not prove successful live backup and restore.

- The second coordinated live run `isolated-csi-1795573786591056654` passed
  the opt-in backup/restore test in 407.97 seconds. The fixture gracefully
  stopped with both volumes validated at sequence 6489, the coordinator saved
  stable snapshot evidence and resumed the source before upload completed,
  and both DataUploads produced durable Kopia IDs. The isolated restored writer
  validated both complete checksummed histories at exactly sequence 6489 and
  became Running. Restore finished Completed with two warnings about source
  PVs excluded from the namespace-only fixture backup; dynamic restored volumes
  were Bound and both data-movement operations completed.
  This was an assisted infrastructure run, not unattended acceptance: restore
  informer read permissions were supplemented, then patch/update permission
  was granted only for the two newly allocated test PV names. A failed rebind
  callback was recovered through restart of the isolated node-agent, without
  editing task or DataDownload status. Production RBAC/preflight and repeated
  unattended verification remain required. Evidence: live-csi-backup-restore-second.log,
  live-backup-record.json, live-completed-journal.json, live-completed-backup.json,
  live-completed-restore.json and live-restored-writer.log in the runtime directory.

- The authoritative backend protocol and mirrored Agent protocol now carry an
  optional explicit consistency group for manual and scheduled commands:
  group ID, offline strategy, application profile, maximum pause, controller
  identities and PVC identities with explicit snapshot classes. Structural
  validation rejects incomplete, duplicate or out-of-namespace inventories and
  pauses outside 1–120 seconds. A profile name alone is not qualification:
  dispatch must eventually resolve it against supported runtime adapters.
  Ordinary Agent manifest builders reject any explicit group rather than
  silently generating an uncoordinated Backup or native Velero Schedule.
  Until persistence/capability-gated dispatch integration is complete, backend
  stored-task dispatch explicitly rejects nonlegacy modes or explicit groups
  instead of dropping their fields. This is a temporary fail-closed gate, not
  an enabled user feature; legacy live-filesystem dispatch remains compatible.
  Focused backend dispatch/protocol tests, Agent suite, Agent build, API build,
  protocol synchronization checks and protocol/Velero race tests passed.
  Repository `make verify` stopped at the formatting gate for the unchanged
  `backend/internal/httpserver/cluster_contract.go`; do not report full verify
  success or reformat that unrelated file as part of this work. Full isolated
  database suites are separate acceptance evidence and must be checked to
  completion before claiming they passed.

- Plan persistence now has an additive `data_protection` JSONB migration with
  live-filesystem defaults for legacy rows and database shape constraints.
  Plan create/update validates application membership within the tenant and
  source cluster, then validates group namespaces against those applications.
  Legacy item hooks cannot be mixed with an application-consistent group.
  Create/update, get/list and delete/cleanup responses retain configuration;
  malformed persisted settings fail closed. Updates from clients that omit
  the new field preserve the previous configuration rather than resetting it.
  API contract responses include the configuration and return validation
  failures as HTTP 400. Stored-task dispatch independently checks the persisted
  plan, so missing strict fields in a task payload cannot bypass the temporary
  coordinated-dispatch gate. This is persistence, not working runtime dispatch.
  Isolated PostgreSQL persistence, constraints, legacy-update, persisted-plan
  dispatch and API lifecycle/contract tests passed. Earlier full `make test`
  completed successfully before this persistence change. The full isolated
  PostgreSQL suites after the persistence change also passed: store in 199.233
  seconds and httpserver in 282.151 seconds. Evidence:
  `persistence-full-backend-tests.log` in the runtime research directory.

- Concrete verifier-boundary fault injection now covers verifier failure,
  bounded verifier timeout and task-context cancellation after the isolated
  volume users drain. The first run found a test-observation race: Kubernetes
  Pod readiness can precede the application's startup-validation log. The test
  now waits for actual paired-data validation instead of treating readiness
  alone as application proof. The revised run passed all cases in 86.77 seconds.
  No case created a Backup, recorded snapshot evidence or became Completed.
  The coordinator now immediately releases failed-run locks only after verified
  resume, using the independent recovery context even when the task context is
  canceled. Resume failure retains locks for independent recovery; cleanup
  failure retains both original and cleanup errors and cannot publish success.
  Unit and race tests cover these branches. A second live run with immediate
  cleanup required in the returned result passed all three cases in 56.05
  seconds. The source writer is again one replica and Ready. Evidence:
  `live-verifier-fault-cleanup-tests.log` and the three Failed journal records
  under `fault-cleanup/`. Agent full tests and affected race/vet checks passed.
  These are concrete driver/verifier tests, not end-to-end user HookScript,
  websocket-disconnect, scheduler or multi-namespace acceptance.

- Offline captures now require a structured quiescence proof, not only a nil
  verifier error. The proof binds profile, run/group IDs, exact controller/PVC
  UIDs, UTC observation time within the run's pause window and at most 16 KiB
  of nonsecret JSON verification evidence with a SHA-256 digest. The coordinator
  persists it before capture and verifies it again before completion. The
  Backup carries the same proof annotation; polling rejects absent/changed
  archived proof, and explicit application-consistent restore requires it.
  SHA-256 detects changes; it is not a signature or application qualification.
  Adapter resolution and trusted qualification are still required, and the
  paired-ledger test profile is not a generic database adapter. Legacy FSB
  restore construction remains unchanged.
  Unit tests reject stale, missing, changed, incomplete and oversized proofs,
  validate persisted JSON round trips, prevent capture without proof and
  exercise unchanged backup-annotation and restore requirements.

- The first proof-enabled live run `isolated-csi-1795575968496221681` failed
  closed after a stale independent-recovery heartbeat and API timeouts during
  snapshot reconciliation. It did not become a verified restore point. The
  source restarted and validated sequence 12209. The old test recovery binary
  also omitted new proof fields when rewriting the Failed journal. It has been
  replaced by a checksum-verified current recovery worker. The recovery scanner
  now skips already-resumed/released history and unexpired runs directly from
  its list result, avoiding redundant per-record reads; a regression test
  requires exactly one LIST for terminal history and live leases. This addresses
  request amplification, not a proven diagnosis of every observed CSI/API delay.
  Failed-run evidence remains in `proof-run-failed-{backup,journal}.json` and
  `live-proof-backup-restore.log`; no failed status was changed to success.

- Proof-enabled live retry `isolated-csi-1795576336418444479` passed actual
  backup/restore in 201.28 seconds. It stopped the paired writer at sequence
  12912, persisted proof, secured both snapshots, resumed the source before
  long upload, completed two durable Kopia snapshots and restored into a new
  labelled isolated namespace without deleting the earlier restored fixture.
  Both recovered histories validated at exactly sequence 12912. The completed
  journal equals the result record and the Backup annotation equals its proof.
  Restore completed with two source-PV exclusion warnings and both data-movement
  operations completed. Test-only provisioning granted patch/update only for
  the two restore PV names after checking DataDownload/Restore ownership and
  claim identities; this is not production installer/RBAC acceptance.
  Evidence: `live-proof-backup-restore-second.log`, `live-proof-backup-record.json`,
  `proof-run-completed-{backup,journal,restore}.json`, `proof-restored-writer.log`.
  A verifier returning success without proof also failed closed in a live
  19.50-second test, restoring the source and releasing locks without a Backup:
  `live-missing-proof-test.log` and `proof-faults/missing-proof.json`.
  Agent suite, affected race/vet tests and the pinned upstream restore-modifier
  test passed. These checks do not enable user-facing application consistency.

The reusable `executor.CoordinatedBackupRunner` now performs read-only command,
profile, workload identity and CSI preflight before opening a verifier session.
It reserves independent recovery time inside the requested maximum pause and
uses the durable coordinator rather than ordinary Backup submission. No default
application profile is qualified or advertised. Websocket backup execution now
has a distinct injected coordinator branch, with no production runner configured;
existing fail-closed dispatch gates remain necessary. This branch requires the
Kubernetes executor, ledger, outbox and recovery provider, rejects OADP and
unconfigured coordination before storage mutation, persists a ConsistencyRun
ledger entry before acceptance and never calls ordinary Backup submission.
Runner errors or invalid returned success consult durable recovery rather than
inventing a terminal success. The concrete runner now classifies errors before
coordinator entry, or during its read-only preflight with no phase assigned, as
`CoordinatedPreparationError`. These failures are reliably published with
`pauseAttempted=false` without waiting for a nonexistent journal. Errors at or
after a prepared checkpoint are not classified this way, since a checkpoint
write may have an uncertain API outcome. Production profile provisioning and
live branch acceptance remain pending. Local
websocket integration tests verify that ledger write or acceptance failure never
starts the runner; the runner sees its persisted ConsistencyRun and delivered
acceptance before execution. Runner errors and unproven success consult recovery
without publishing success. These tests use injected execution/recovery stubs,
not real cluster backup or process-disconnection acceptance.
Ledger reconnection now diverts commands carrying application-consistent mode
or any explicit group away from ordinary Backup polling. Its dedicated recovery
interface requires exact dispatched-group validation, quiescence proof, resumed
workloads, released locks and durable Kopia evidence before publishing success.
No production recovery provider is configured yet: an unavailable or unproven
result remains pending and is logged, rather than becoming native Backup success.
A read-only `ConsistencyRecoveryObserver` now polls the explicitly configured
journal namespace within a bounded observation timeout. It never repeats pause,
snapshot or upload operations. Exact group identity is checked on every read;
successful results require full evidence, while failed results are publishable
only after verified resume and lock release. Unknown phases, missing journals
and invalid evidence fail observation without synthesizing success. Nonterminal
or not-yet-resumed failures wait until cancellation or the observation bound.
The websocket recovery branch can publish these safe failed results through its
reliable outbox. `HCDR_CONSISTENCY_JOURNAL_NAMESPACE` explicitly opts a Kubernetes
executor into this read-only provider (2-second polling, 2-minute observation
bound); it has no default namespace and is disabled in dry-run mode or without
a concrete dynamic applier. This option neither installs recovery infrastructure
nor enables coordinated execution or advertises an application profile.
The websocket observer retries its per-attempt timeout and transient Kubernetes
API throttling/unavailability/timeouts after a 2-second delay, bounded by the
task/session context. Retries only read evidence and never repeat backup actions.
Malformed evidence and other permanent errors are not retried automatically.
Installer configuration and end-to-end reconnection acceptance remain pending.
Explicit per-driver snapshot-class selection is propagated to the Backup and
checked against DataUpload and snapshot evidence, including completed uploads.
Tests reject conflicting classes for a single driver and preserve missing-volume
coverage. Agent full unit tests pass; this is not live acceptance of the runner.

Outstanding: full capture-side adapter integration and final action fencing, production recovery worker deployment/arming, qualified
application drivers and backend publication of audited quiescence/backup evidence,
dispatch capability gates, strict runtime data-path verification, UI, installer
capability provisioning, and live failure/integrity acceptance tests.

The isolated paired-ledger shutdown verifier is now reusable rather than an
inline live-test parser: it requires a deadline, closes blocked streams on
cancellation, caps log input at 1 MiB and rejects malformed/truncated events,
missing shutdown validation, nonpositive committed sequences and output after
shutdown validation. The live backup fixture uses this verifier; unit and race
tests cover its parsing and deadline behavior. This log parser alone does not
qualify an application: a future profile must fence the actual Pod/container,
validate the deployed fixture/script and bind the opened stream to those exact
identities before any pause. No generic database guarantee is inferred.

`executor.IsolatedPairedLedgerProfile` now implements this preparation for the
fixed `hcdr-consistency-test` namespace, single opted-in writer Deployment and
two exact fixture PVCs. It checks namespace/PVC safety labels and UIDs, pins the
writer script digest and image, requires an immutable script ConfigMap predating
the Pod, validates container command/mounts and rejects restarts, sidecars,
environment overrides and unqualified lifecycle/probes. Pod ownership must lead
through the exact ReplicaSet UID to the fenced Deployment. It opens a
current-session container log stream before pause, rechecks Pod/container
identity and binds verification to the prepared workload/volume inventory.
Close and timeout cancel the session. The adapter is not enabled by default or
advertised. Unit tests cover Pod
guards, pinned repository script and mutable-script rejection, not successful
live stream preparation. A local HTTP Kubernetes API fixture now exercises the
real typed client and streaming transport: successful stop proof, replacement
Pod rejection after opening the stream, cancellation closure and idempotent
session cleanup. It asserts read-only API use and current-session/container log
parameters. This is not an actual cluster acceptance test. This is a test-fixture adapter, not a production
database adapter or completion of application-profile acceptance.

On 123, the isolated writer script content matched the pinned digest. After
checking the isolated namespace label, an RV-fenced patch made only that fixture
ConfigMap immutable; no Pod restart or pause was performed. Live read-only
profile preparation then passed in 0.22 seconds: it opened and closed the real
container log stream, while Pod UID
`769d8fe1-f4ef-435c-843e-e89c02cdcedc`, ready/restart state and Deployment
generation 33 remained unchanged. Evidence: `paired-ledger-script-before-immutable.json`,
`paired-ledger-script-immutable.json`, `live-profile-preparation.log` in the runtime
research directory. This is preparation acceptance only, not coordinated backup
or restore acceptance. Kubernetes injects a service-account projected mount;
the profile permits only its read-only standard mount path in addition to the
three fixture mounts and rejects an extra mount targeting the script directory.

First full runner live attempt `isolated-runner-1795578834887422341` failed
closed in 37.54 seconds: the typed client's 15-second HTTP timeout also applied
to its followed log response, so shutdown evidence was unavailable. The run
reported Failed with Resumed=true; the source subsequently verified 1/1 Ready.
Evidence is retained in `live-coordinated-runner-first-failed{.log,-record.json}`.
The profile now accepts a separate log client; the live harness retains bounded
API requests and uses a log transport bounded by the task/session context, not
the ordinary per-request timeout. This fix has not yet passed a full live retry.

The subsequent full runner retry `isolated-runner-1795578936707048631` passed
in 154.05 seconds using the separate log transport. The actual qualified fixture
profile observed graceful paired-volume shutdown at sequence 19179. Both CSI
snapshots were secured, the source returned to 1/1 Ready before long upload,
and two durable Kopia snapshots completed. The test checked the exact original
task/group against the successful result and reloaded the journal to prove it
equals that result. Evidence: `live-coordinated-runner-second.log` and
`live-coordinated-runner-second-record.json`. This validates the library runner
and profile against 123, not websocket/backend dispatch, restore of this run,
multi-namespace coordination or actual process/disconnection recovery.

Restore-only acceptance for this runner backup now reuses the tested restore
modifier/integrity harness, checks the persisted record against the actual
journal with compact JSON comparison (pretty JSON evidence whitespace is not
an identity change) and refuses a nonempty target. The first submitted restore
`hcdr-restore-hcdr-consistency-test-6374aae6` into newly created isolated target
`hcdr-consistency-restored-runner` encountered four namespace RBAC errors:
the isolated Velero SA lacked creation rights for two PVCs, the script ConfigMap
and the writer Deployment. Its native state is preserved in
`runner-restore-rbac-failed-state.json`; this is not successful restore acceptance.
The test now performs explicit SubjectAccessReviews for target resource access
before submission to expose this provisioning gap early. The running failed
restore/observer must be inspected to terminal state before retry; do not erase
it or report its two pending DataDownloads as complete.

The test target now has an explicit namespaced Role/RoleBinding from
`deploy/testing/application-consistency/restore-target-rbac.yaml`, applied only
after exact123 and isolation-label checks. All 18 required access checks for
PVCs, ConfigMaps and Deployments passed; evidence is in
`runner-restore-target-permission-check.log`. This grants no cluster-wide
wildcards or PV permissions, and does not repair the already-failed restore.
Its original plugin operations and observers remain under observation; a new
restore must not overwrite that evidence.

The original restore defaulted `itemOperationTimeout` to four hours. Upstream
DataDownload reconciliation waits for a target PVC before accepting a download,
so the two absent PVCs left downloads without a phase. After recording exact
Restore ownership, backup and target identities, the two isolated downloads
were canceled using their supported `spec.cancel` field with UID tests, not
status edits. Both reached Canceled, the Restore reached PartiallyFailed, and
its observer failed in 346.51 seconds. Terminal evidence is preserved in
`runner-restore-rbac-failed-terminal.json` and
`live-runner-restore-rbac-failed.log`. The obsolete PV authorization watcher was
terminated without granting any PV permissions. Test-only restore names now
include target namespace in their digest, and test restores explicitly bound
plugin operations to eight minutes rather than the four-hour server default.
No restore retry has yet passed.

The independent restore retry `hcdr-restore-hcdr-consistency-test-89c91bc9`
passed in 102.57 seconds after namespace permissions were preflighted. It
restored the runner backup into the previously empty labelled isolated target;
the restored writer's startup checksum/history validation reported exactly
sequence 19179, matching the quiescence proof across both volumes. Restore is
Completed with two source-PV exclusion warnings and no errors, both
DataDownloads are Completed, both target PVCs are Bound and the writer is 1/1
Running. Source writer remains 1/1 Ready. The test-only PV authorization verified
ownership and granted patch/update only for
`pvc-574fbd01-7570-4986-83a5-9389e7afbdae` and
`pvc-6d2d031f-0171-405b-b48c-b6307845bb72`. Old PartiallyFailed Restore and canceled
downloads were retained. Evidence: `live-runner-restore-second.log`,
`runner-restore-completed.json`, `runner-restore-completed-downloads.json`,
`runner-restored-writer.log`, `runner-restore-second-pv-authorization.log`.
This proves the current library runner/profile backup-to-restore data path,
not production installer permissions, backend/UI integration, multi-namespace
acceptance or real websocket/process-disconnect recovery.

Real coordinator-process termination was exercised in run
`isolated-runner-exit-1795579955142829492`: a child test process ran the actual
runner/profile, was killed after a durable Quiescing checkpoint and replica=0,
and could not execute deferred cleanup. The parent performed no Resume call.
The independent deployed worker recovered the source, released locks and
persisted Failed rather than success. The source's startup validation verified
its complete paired-volume history at sequence 21823. The test passed in
106.30 seconds, within the 120-second authorized bound. Evidence:
`live-runner-process-exit.log`, the run's `-child.log` and `-recovered.json`.
Additional explicit SIGKILL exit-status and pause-budget assertions were added
after this initial run and are compiled/unit-checked, but have not themselves
been live rerun. Websocket loss, terminal outbox delivery and backend/UI
publication remain separate unverified acceptance items.

Read-only recovery replay acceptance now loads that actual123 process-exit
journal through `ConsistencyRecoveryObserver`, attempts failure delivery over
a closed local websocket, reopens the on-disk outbox, replays over a replacement
websocket and acknowledges the same event. It passed in 0.02 seconds and checked
that Failed, verified resume, released locks and the original failure message
survive; no success event is substituted. Evidence:
`live-recovered-failure-reconnect.log` and `recovery-outbox-3626383229`.
The peer and acknowledgement are local test fixtures, not the149 backend, and
the transport is interrupted after independent recovery, not during pause.
Therefore this proves reliable terminal replay against real durable evidence,
not full agent-session disconnect-during-pause or backend persistence acceptance.

The durable evidence wire types now live in the backend-authoritative protocol
(`ConsistencyRecord`, workload/volume identities, snapshots and quiescence proof)
and are mirrored through the protocol generator. Agent coordinator types are
aliases of these contracts, not independent duplicate structs. A shared wire
round-trip test preserves the existing journal JSON shape, including proof and
DataUpload identities; the Agent full suite and backend protocol suite pass.
This is schema consolidation for backend ingestion, not backend evidence
validation or restore-point publication. Dispatch remains fail-closed while
those trust/capability and persistence paths are incomplete.

Quiescence proof verification is now also backend-authoritative:
`protocol.VerifyConsistencyQuiescenceProof` checks run/group/profile binding,
observation window, bounded JSON-object evidence, compact-JSON SHA256 and exact
unique controller/PVC identities. The Agent delegates to the same generated
implementation. Shared negative tests cover substituted identities, duplicate
identities, missing/future/out-of-window proof, digest changes and malformed or
oversized evidence; pretty persisted JSON remains valid. This verifier detects
structural/evidence mismatches, not cryptographic authenticity or application
qualification. Completion admission must additionally bind the stored task,
qualified capability and durable snapshots before publishing a restore point.

Task identity, complete result and snapshot-inventory validation now also use
the backend-authoritative protocol implementation, with Agent wrappers rather
than divergent copies. A successful result requires the exact dispatched
workload/PVC identities, Completed phase, verified resume, released locks, no
failure, valid quiescence proof and one secured durable Kopia snapshot per
required volume with nonempty unique DataUpload UIDs. Shared tests reject
substituted resources, unresumed/locked/failed/incomplete runs, CSI-only evidence,
missing snapshots and duplicate uploads. HTTP admission and tenant-scoped
restore-point persistence are not yet connected to this shared validator.

Backend backup-completion admission and the central restore-point publication
entry now invoke a stored-task consistency validator before legacy processing.
Unrequested application-consistency claims, unsupported modes, omitted groups,
native Completed without strict evidence and incomplete records are rejected
as backup-result conflicts. Explicit application-consistent results must match
the stored task group and both requested/achieved mode claims, then pass the
shared exact-identity/proof/durable-snapshot validator. Legacy unclaimed FSB
completion retains its path. Negative tests exercise both entry points without
a store to prove rejection occurs before publication side effects. Immutable
task configuration creation, persisted-plan omission defenses, qualified-agent
capability admission, evidence persistence and actual strict publication remain
incomplete; dispatch gates remain in place.

Backup and schedule-sync task creation now snapshots the plan's persisted data
protection mode/group under the existing plan row lock after tenant/reference
validation, and records snapshot version 1. Caller-provided mode/group claims
are replaced by the authoritative plan configuration, on a detached payload
copy. Database tests verify caller-map isolation, both task types, persisted
historical settings after tasks end and the plan is edited, and rejection of
caller attempts to substitute an application group for a live-filesystem plan.
The existing active-work guard still prevents editing a busy plan. This is
creation-time capture, not yet complete immutability: status-payload update
paths must additionally preserve these fields before dispatch can be enabled.

Migration 000059 now enforces version-1 snapshot immutability at the database
boundary: updates cannot change or remove the version, mode, or group, including
the distinction between an explicit null group and a missing key. Both merged
status payloads and direct SQL are covered, and violations roll back the whole
update. Ordinary progress updates and identical snapshot fields remain allowed.
This guard does not backfill legacy tasks or enable coordinated dispatch.
Validation passed against isolated PostgreSQL: the complete store suite, focused
snapshot/configuration tests (including application-group downgrade attempts for
backup and schedule-sync), and HTTP consistency admission plus legacy backup
publication/restart regression tests. Protocol generation and diff checks passed.

Both completion admission and restore-point publication now check the persisted
plan for legacy tasks without a versioned creation-time snapshot. A missing,
cross-tenant, cross-cluster, or strict-mode plan fails closed; lookup errors are
propagated. Legacy FSB remains allowed for a verified live-filesystem plan.
Versioned tasks retain their historical configuration independently of later
plan changes, but malformed or unsupported snapshot versions are rejected.
This is an omission defense, not reconstruction of historical strict intent.
Focused consistency admission tests and the broader HTTP backup/restore-point/
publication regression selection passed against isolated PostgreSQL after this
change. No cluster mutation or production dispatch was performed for this check.

The completion validator now lives in the store package and is reused by HTTP
admission. Restore-point creation and atomic backup-result publication validate
evidence against the locked database task payload before publishing or starting
replacement deletion; legacy tasks additionally verify the persisted plan mode.
Accepted strict evidence remains in the existing tenant-scoped backupResult
snapshot and restore-point metadata.velero. Replays compare requested/achieved
consistency and the full record, rejecting different evidence even when backup
name and size metrics match. Comparison normalizes JSON object ordering without
rounding integer evidence values.

Migration 000060 prevents replacement/removal of an accepted backupResult and
protects persisted restore-point consistency evidence and its backup-task link.
Database tests cover unproven direct publication/creation, valid synthetic
evidence persistence, identical replay, conflicting durable snapshot replay,
status-payload removal, direct metadata tampering, and atomic task finalization.
These synthetic records test storage/admission, not application qualification or
the still-incomplete live Agent-to-backend publication acceptance.
Focused store/admission tests and the broader HTTP backup/restore-point selection
passed. The full store run exposed three legacy creation regressions: historical
tasks may omit cluster_id and a restore point may have no protection plan. The
nonfinalizing creation check now preserves those cases while enforcing tenant,
any recorded cluster, and exact nullable plan linkage. All three affected tests,
strict publication, and backup-result regressions passed after correction; the
full store suite has not yet been rerun after that correction.

Follow-up: the complete store suite passed after the legacy correction (201.684s).
The Agent now has a read-only completion reader which requires a completed,
resumed and unlocked journal, validates the archived quiescence proof on the
Backup, checks the completed Backup's UID-owned DataUploads against every
original PVC and snapshot class, compares exact Kopia handles/upload UIDs with
the journal, and rejects Backup changes during the check. It returns the
verified Backup object for subsequent publication work; it is not yet wired
into websocket completion/metrics reporting or production capability admission.

The first read-only 123 acceptance attempt found an archived-proof comparison
bug: pretty-printed RawMessage evidence was compared with compact archived
bytes using reflect.DeepEqual. The driver now marshals both typed proof records
to compact JSON before comparison, retaining digest/content/identity checks.
Regression coverage accepts formatting-only differences and still rejects
changed proof/run/snapshot/upload/class, missing inventory, unsafe resume and
Backup changes during verification. No pause or cluster mutation was used.

The read-only retry passed against the existing real runner evidence:
run isolated-runner-1795578936707048631, Backup UID
4ab86fe5-3823-4346-9117-db27680025ea, two exact durable Kopia snapshots.
Both initial failure and retry logs remain under the runtime evidence directory:
live-completion-readonly.log and live-completion-readonly-retry.log. This is
read-only binding of an existing backup, not a new backup/restore acceptance or
an Agent-to-149 completion publication test.

Coordinated execution and recovered success now use a shared websocket completion
builder rather than sending an evidence-only result. It validates the task/group,
derives the same CSI Backup identity as the coordinator, calls the read-only
completion reader, checks task/command labels and immutable Backup UID, requires
one completed volume measurement per group volume, and attaches existing size
metrics. Missing/incomplete/unknown measurements or changed evidence do not emit
a successful terminal event. Collection is bounded to two minutes per attempt;
failed collection retains durable recovery state for later observation/reconnect.
Automatic in-session retry of incomplete completion measurements remains pending.

Unit tests prove valid payload identity/evidence/metrics (including known zero
incremental bytes), rejection before outbox insertion for invalid evidence,
identity or measurements, and durable successful-event retention across a closed
websocket and outbox reopen. The full Agent suite, focused websocket race tests,
vet, protocol generation and diff checks passed.

Read-only live payload construction passed on 123 using the existing completed
isolated runner Backup: logicalBytes=3753114, uploadedBytes=3753114,
allVolumesKnown=true. Evidence: live-completion-payload.log under the runtime
research directory. It read Kubernetes and isolated backup object storage; it
did not create a new backup, pause a workload, or send to the 149 backend. Actual
platform persistence/acknowledgement and dispatch capability admission remain
unverified/incomplete, so production execution stays disabled.

Completion collection now retries in-session within one shared two-minute
context, at two-second intervals, only for explicitly incomplete measurements
and transient observation errors. It never reruns the coordinator, pauses a
workload, or retries terminal websocket delivery (the existing durable outbox
owns delivery replay). Invalid evidence/identity or conflicting failed/extra
volume inventory stops collection immediately. Cancellation/deadline prevents
publishing even if a collector returns a payload after cancellation; no success
event is inserted before collection is proven. Expiry preserves durable state
for later reconnect observation rather than inventing a failed backup outcome.
Full Agent tests, focused race tests, vet, generation and diff checks passed;
retry tests cover pending-to-ready, API unavailable/throttled, permanent proof
failure, pre-cancellation, cancellation during wait/collection and deadline.

Agent registration now has an optional versioned applicationConsistency field
with an exact qualified-profile inventory (coordinated-offline-v1). Old agents
omit it and retain legacy behavior. The client advertises only when coordinator,
recovery observer, ledger/outbox, verified completion/size/volume readers and
matching agent namespace are configured; the runner additionally requires
explicit independent-recovery guard/heartbeat/polling/time-budget settings and
matching adapter names. This is configured support, not a claim that every PVC
supports snapshots: current CSI, UID, workload-user and recovery-heartbeat
readiness are still revalidated by the runner before any pause.

The backend binds a detached profile inventory to the authenticated current
socket, clearing it on replacement/disconnect and rejecting stale socket
updates. Manual backup dispatch now has a coordinated-only path for a valid
version-1 creation snapshot, identified command, tenant/cluster-owned plan,
explicit in-scope group and an advertised matching profile. The exact captured
mode/group are carried in the command. Immediately before sending, capability
and socket ownership are rechecked under the session read lock, so a stale
socket cannot borrow replacement-connection support. Unsupported/legacy agents,
unknown profiles, invalid snapshots and strict native schedule-sync still fail
closed. Default production construction still has no coordinator installed and
therefore does not advertise or enable this execution path. Production adapter/
recovery provisioning, strict scheduling, cancel/recovery acceptance, persistent
capability UX and actual 149 platform acceptance remain incomplete.
Validation passed: full backend HTTP/protocol suites (HTTP 279.229s), focused
backend consistency race tests including actual qualified local-websocket
dispatch and stale-socket rejection, full Agent suite, Agent websocket/executor/
protocol race tests, and vet. Capability tests cover missing execution layers,
namespace mismatch, unsupported/duplicate/empty profiles, legacy agents, plan
scope, snapshot version, disconnect/replacement and detached inventories.
`make verify` passed protocol sync and backend/Agent vet but stopped at the
repository gofmt gate because unchanged backend/internal/httpserver/cluster_contract.go
is unformatted. The goal-owned consistency_result_test.go was formatted; no
unrelated source was reformatted. No live cluster or production installation was
changed during capability implementation, and no release was created.

Actual websocket-during-pause acceptance was attempted on 123 using test-only
runner injection (no production configuration/installer change). Run
15459bb6-9dc6-4deb-952e-39270f29c1cb used the existing isolated writer, matching
two PVCs, fixed snapshot class and independent recovery worker. The local
websocket was closed only after a durable Quiescing journal and replicas=0 were
observed. This was a real transport interruption during pause, not replay after
the fact. The requested successful-completion acceptance FAILED in 85.36s:
independent recovery expired the 80-second capture lease and persisted Failed
with `consistency pause lease expired; capture invalidated by recovery`.
Resumed and LocksReleased were true; source writer returned to 1/1 Ready.
The observed verified-resume timestamp was about 83.86s after coordinator start,
within the authorized 120s limit; this is not a precise measurement of actual
write downtime. Subsequent event inspection placed the scale-up request at
05:17:12 UTC, before the capture deadline of 05:17:22.523 UTC; the container
started at 05:17:25 UTC and the Pod became Ready at 05:17:26 UTC. The coordinator
and recovery worker currently conflate restored replicas with readiness, allowing
the capture watchdog to invalidate a run while readiness is pending. This run
is not application-consistent backup success.

The offline recovery primitive now separates fenced `RequestResume` from
`Resume`, which still requires observed generation and ready replicas. Tests
cover pending readiness, idempotence, replaced workload identity, conflicting
ownership and modified replica counts. This primitive alone does not resolve
the watchdog race by itself. The coordinator now checkpoints a bounded readiness
window only after a fenced restore request succeeds before the capture deadline,
with valid quiescence and secured snapshot inventory. The worker defers takeover
only while that window is valid and a read-only workload check confirms exact
UID, original replicas, removed ownership and the matching resume annotation.
The window cannot exceed 120 seconds from run start; readiness is still required
for success. Invalid, missing or expired evidence preserves normal failed recovery.
New coordination requires the heartbeat capability `resumeWindow=fenced-ready-v1`;
an older recovery worker therefore fails preflight rather than silently ignoring
the new window. Unit tests cover these branches. No deployed worker has yet been
upgraded at that checkpoint. A subsequent isolated-worker upgrade and live retry
are recorded below; neither establishes production rollout or 149 acceptance.

### Bounded readiness window: successful isolated disconnect retry

The isolated recovery worker was upgraded to the static binary SHA256
`9ec38483e66f2f25c9316a103b318d657e3157e03915d421058be7cc8d4fd949`,
with a distinct download URL and matching init checksum. Its fresh heartbeat
advertised `resumeWindow=fenced-ready-v1`. An initial dynamically linked build
failed to start in Alpine; it never enabled a backup and was replaced by the
static build. Rollout status alone was not treated as worker readiness.

Real paused-disconnect retry `c11e669a-f858-4b3d-8671-80c25f78063d` passed in
172.15 seconds. The source writer was deliberately disconnected from the local
test websocket while replicas were zero. Restore request was checkpointed at
05:37:48.652147020 UTC, before the capture deadline 05:37:55.727935740 UTC.
Verified resume was observed at 05:38:07.845876072 UTC, after that capture
deadline but within the readiness deadline 05:38:35.727935740 UTC and the
120-second total budget (92.05 seconds from coordinator start). The journal
completed without watchdog invalidation, with two durable Kopia snapshots.
Shared backend/Agent completion validation also checks any new resume-window
timestamps against the dispatched group's pause budget and requires the
quiescence proof to precede the restore request. Both timestamps absent preserves
legacy journal compatibility; a partially populated or over-budget window is
rejected. Tests cover the shared validator and completed-result publication gate.
The completed event and full metrics survived reopening the outbox and replay
on a replacement local websocket; the test fixture acknowledgement retired it.
This is not a real 149 acknowledgement or restore acceptance for this backup.
The original failed run and its unacknowledged failure evidence remain retained.

Evidence: `live-pause-disconnect-resume-window.log`, the retry UUID-prefixed
task/disconnect/resumed/journal/completed JSON files, and
`pause-disconnect-outbox-4263801316`, all under the runtime research directory.

Timing inspection of this completed run found the following separate stages:

| UTC observation | Event |
| --- | --- |
| 05:36:35.798 | Coordinator started; not the exact write-stop instant |
| 05:36:46.427 | Test observed replicas zero and closed the websocket |
| 05:36:58.225 | Application quiescence proof observed |
| 05:37:04 | Velero created the first source snapshot (commits) |
| 05:37:19 | First DataUpload submitted; second source snapshot (ledger) created |
| 05:37:34 | Second DataUpload submitted |
| 05:37:48.652 | Original replicas restored and resume request checkpointed |
| 05:38:07.846 | Verified source readiness observed |
| 05:38:43 / 05:39:19 | The two DataUploads completed |

Velero's PVC actions created the two source snapshots sequentially, with about
15 seconds each between source snapshot creation and DataUpload submission.
These are observed pipeline intervals, not precise storage-driver snapshot
latencies. The coordinator's 92-second start-to-readiness measurement also
includes preflight, stopping/draining Pods, snapshot rebound/preparation and
application restart. It is not 92 seconds spent creating a snapshot and is not
the duration of the entire upload. Additional mover preparation must not extend
the write pause once all immutable snapshot sources are secured. Server and
node-agent timing logs are retained as `c11e669a-velero-timing.log` and
`c11e669a-node-agent-timing.log` in the runtime research directory.

The subsequent Pending-mover barrier live test, run
`546aae3e-adcf-4129-bf97-527d234a4bcc`, passed in 182.07 seconds. The source was
verified resumed at 05:51:03.524 UTC (90.85 seconds from coordinator start), before
the first DataUpload started at 05:51:13 UTC. The second started at 05:51:45 UTC;
uploads completed at 05:51:40 and 05:52:14 UTC. Both produced durable Kopia
snapshots, the journal completed, and completion survived local websocket replay
and fixture acknowledgement. This proves source readiness before bulk transfer
in this run, not a universal pause-duration target or 149 publication acceptance.
The 90.85-second observation versus the previous 92.05 seconds is not sufficient
to claim a significant performance gain; source snapshot and Pod lifecycle
latency still require attention.

Evidence: `live-pause-disconnect-pending-mover.log`, retry UUID-prefixed JSON,
`546aae3e-transfer-after-resume.json`,
`546aae3e-source-resumed-deployment.json`, and
`pause-disconnect-outbox-378542006`, under the runtime research directory.

The original native Backup isolated-ws-15459bb6-9dc6-4deb-952e-39270f29c1cb was
observed through its existing resource handle until Completed, without restart,
status editing or cleanup. A separate READ-ONLY failure-replay test passed: the
retained real Failed event survived outbox reopen and a replacement local
websocket, preserved exact task/group identity and safe-resume/lock-release
evidence, while the durable consistency journal remained Failed despite native
Backup Completed. Source UID and 1/1 readiness were rechecked. No acknowledgement
was applied to the original event, preserving it for further investigation.

Evidence under the runtime research directory: live-pause-disconnect.log
(failed success acceptance), live-pause-disconnect-failure-replay.log (passing
safety replay), run-prefixed task/disconnect/resumed/journal/native-terminal JSON,
and pause-disconnect-outbox-2207848925. The peer is a local test fixture; neither
149 platform persistence nor its acknowledgement was verified. No production workload or 47 was
operated; no second backup was started to mask the failure. Successful
pause-disconnect completion and actual backend acceptance remain incomplete.

Initial read-only preflight on the 123 source found no
`snapshot.storage.k8s.io` snapshot APIs or VolumeSnapshotClasses; existing Velero
had no explicit EnableCSI server flag. With explicit authorization, the test
cluster now has matching snapshot CRDs/controller and a nondefault Longhorn
snapshot class. A separate isolated Velero deployment with EnableCSI completed
a two-volume CSI data-movement smoke backup. This does not authorize changing
existing plans or prove application consistency. Evidence and test logs are under
`/data/hypercdr-runtime/research/velero-consistency/`.
