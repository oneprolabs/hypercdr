# CSI snapshot backup: current scope

## Destination restore requirements

For backups with `spec.snapshotMoveData=true`, the destination restores Kopia
data through Velero `DataDownload` operations. The destination needs provisionable,
mountable volumes and Velero's `EnableCSI` data-mover restore plugins; its storage
does not need snapshot support, a VolumeSnapshotClass, or the source driver.
Installation and Velero upgrades enable the plugins by default. Before namespace
replacement or restore submission, the Agent checks the synchronized Backup's
actual mode and rejects data-mover restores if the enabled Velero deployment is
not ready. File-level restores do not require this feature.

Velero's internal transfer records remain in the backup for restore processing.
They are not selectable application resources or additional business namespaces.

The current product decision supersedes the application coordination proposals
in this directory. HyperCDR uses Velero CSI snapshot data movement and does not
stop services, scale workloads down, freeze applications, or deploy an
application recovery coordinator for backups.

New protection plans select `snapshot-crash-consistent`. Consistency is provided
by the CSI driver. Independent volume snapshots do not imply a cross-volume
atomic snapshot or application consistency. Kopia transfers stable snapshot data
while source applications continue running.

Missing snapshot prerequisites fail closed. Operators must configure compatible
CSI storage and snapshot infrastructure or explicitly accept live-filesystem
backup risks. There is no automatic file-level fallback. Historical FSB restore
compatibility is retained. Legacy application-coordinated plans require explicit
reconfiguration, not silent conversion or execution of old stop/freeze actions.

Task-created temporary snapshots and exposure volumes should be cleaned after
successful data movement, or after failed/canceled movement has stopped. User
snapshots must not be deleted. A disconnected task is not proof of termination.
Velero owns the resource lifecycle; task success alone is not evidence that a
storage driver's asynchronous deletion has finished. Failure-path cleanup and
orphan handling require runtime verification before being claimed complete.

## Verified isolated acceptance

On 2026-11-25, after removing the coordination executor and independent recovery
deployment, `TestIsolatedOrdinarySnapshotBackupCompletion` passed against cluster
123 in 175.71 seconds. Backup `isolated-snapshot-1795603248623673581` reached
`Completed`, with three completed DataUploads and verified durable Kopia evidence.
The isolated writer retained its UID, generation and one ready replica. No
application stop/freeze operation was invoked by the backup path.

After completion, the task's temporary VolumeSnapshots were absent; the only
remaining VolumeSnapshot was the pre-existing `ledger-storage-probe`. This
verifies Kubernetes snapshot cleanup for this successful run, not every storage
driver's physical garbage collection or cleanup-error retry behavior. This run
does not prove application consistency, cross-volume atomicity or restoration.

The same backup was subsequently restored into a new isolated namespace using
PVC-only restore `isolated-csi-restore-20261125-1047`. It reached `Completed`
without errors. A read-only Python verifier mounted all three restored PVCs and
exited successfully: ledger contained 78,150 complete records and commits
contained 78,185. Each volume independently passed contiguous sequence and SHA256
checks, without a partial tail. Their differing counts are expected evidence
that independent CSI snapshots must not be labeled cross-volume application
consistent. No restored application service was started.

The first restore attempt failed because the isolated Velero account lacked
target-namespace permissions; its downloads were canceled before retry. The
second attempt additionally exposed missing PV rebind permissions. Exact-name
RBAC for only the three task-created PVs was granted, and the isolated node-agent
was restarted to reattach completion observation to the same downloads. No
database status was rewritten and no data movement task was recreated during
this completion recovery. This is not evidence of automatic RBAC-error retry.

## Final validation and cleanup

The backend full suite passed using a disposable PostgreSQL container. Agent
unit tests and vet passed after removal of the stop/recovery state machines.
The 149 development API and frontend were rebuilt from the current worktree;
frontend lint, tests and build passed. Browser checks confirmed two backup
methods, default CSI selection, no application-coordination option, explicit
file-level consent and blocked continuation when consent is revoked. Light and
dark screenshots were reviewed; the wizard was canceled without saving a plan.

The restore target namespace, its three restored PVs and the exact-name PV RBAC
were deleted. The obsolete recovery deployment, Role, RoleBinding,
ServiceAccount and heartbeat were removed from the isolated source namespace.
The source writer remained ready. CSI infrastructure and existing source PVCs
were retained. Test browser sessions and the obsolete local UI harness server
were stopped. No production workload, host 47, Git commit or release was changed.

Repository-wide `make verify` is not green: protocol sync and both vet commands
passed, but the format gate reports the pre-existing
`backend/internal/httpserver/cluster_contract.go`. This unrelated file was not
reformatted. Cleanup-error retries and storage-driver physical garbage collection
have not been verified. The original application-consistency/freeze objective
was explicitly superseded by the user's CSI crash-consistency requirement; its
cross-volume and freeze/thaw acceptance criteria must not be reported as passed.

## CSI-only continuation: cleanup audit, 2026-11-26

The user explicitly confirmed continuing CSI-only acceptance, not restoring the
superseded application-coordination objective. Read-only inspection of cluster
123 found only the pre-existing `ledger-storage-probe` VolumeSnapshot and its
VolumeSnapshotContent. The isolated Velero namespace had no exposure PVCs or
upload exposure Pods; its DataUploads were terminal, including two historical
`Canceled` uploads for `isolated-pg-1795599488795760034`. This is evidence of no
currently visible Kubernetes snapshot/exposure leak in that environment, not a
new controlled failure or cancellation test or proof of physical storage cleanup.

Raw API evidence is stored outside the repository under
`/data/hypercdr-runtime/research/velero-consistency/csi-cleanup-20261126/`.
Velero's CSI exposer cleanup deletes the exact original snapshot and exposure
snapshot names passed by the DataUpload controller. It logs deletion errors
without returning them, so terminal DataUpload status alone does not establish
successful cleanup or retry. Controlled deletion-error and failure-path runtime
acceptance remain outstanding.

### Owned snapshot retry implementation

New CSI snapshots created for HyperCDR-managed Backups receive explicit backup
UID and task ID annotations at creation. The CSI exposure snapshot inherits
those annotations. The Agent periodically retries cleanup of those snapshots
for terminal Backups, including earlier runs, only after all associated
DataUploads are terminal. Deletion uses snapshot UID and resource-version
preconditions and rechecks the Backup identity. PVC references prevent cleanup.
Non-owned snapshots are ignored without warnings or changes.

This retry path intentionally does not infer ownership from a name prefix or
retroactively mark unproven legacy snapshots. Backups already removed before
cleanup currently lack the authoritative terminal record required by this
path unless a UID-bound cleanup approval was persisted before their removal.
The approval is written only after terminal Backup/DataUpload verification and
is bound to the exact snapshot UID. Approved orphan snapshots can be retried
after Backup removal, with another check for active uploads and PVC references.
Copying annotations to a different snapshot UID does not transfer approval.
Unproven legacy orphans remain untouched. Snapshot deletion failures
are retried on subsequent runtime observations. The new ownership annotations
require the updated bundled Velero plugin; installed old images do not gain
them merely from updating the control plane. These changes have not yet been
deployed into an installed Agent or Velero image.

Focused Agent tests cover success, failure, cancellation, running/unknown
uploads, external snapshots, mismatched identities, PVC references, deletion
failure and idempotence. The initial direct Velero test attempt failed because
the default module proxy timed out and the unprepared source lacks the bundled
Kopia patches. Subsequent verification used the repository's official
`prepare-patched-source.sh` workflow in the external runtime directory and a
reachable module proxy; no module replacements were edited in the repository.

All three new Velero regression tests passed against that prepared source:
`TestHyperCDRSnapshotOwnershipRequiresManagedBackupIdentity`,
`TestCSISnapshotCleanupScopeAndIdempotence`, and
`TestCSITerminalCallbacksCleanOnlyReferencedSnapshot`. They verify that PVC
labels alone do not grant ownership, cleanup is exact-name/namespace scoped and
idempotent, and completed/failed/canceled callbacks pass only the task snapshot
reference to cleanup. These are fake-client component tests, not live full-task
failure or cancellation acceptance.

The existing Velero completed/failed/canceled callback and cancellation tests
also passed against prepared source. A static Velero CLI/server binary built
successfully from that source and executed `version --client-only`. This is a
local validation binary without release version metadata; no image was pushed,
no installed Velero was replaced, and no release was created. Agent runtime
retry tests additionally verify bounded execution, retry after deletion failure,
and no warning when cleanup merely ignores unrelated snapshots.

The full Agent `go test ./...` suite passed after this change. `make verify`
again passed protocol synchronization and both Go vet commands, then failed
the existing repository gofmt gate. Logs are in the cleanup audit directory.

`TestIsolatedOwnedSnapshotCleanup` then passed against cluster 123 in 39.65
seconds. The test seeded two real CSI snapshots, waited for `readyToUse`, and
associated only one with an existing completed isolated Backup without changing
that Backup's status. Current Agent code deleted the owned snapshot, preserved
the unmarked control snapshot, and remained idempotent. Writer UID and generation
were unchanged. Test teardown removed both seeded test resources by UID.
This validates the live cleanup API path with seeded residual snapshots, not
automatic ownership stamping by a newly deployed Velero image or end-to-end
failure/cancellation recovery. Orphan retry after record removal and copied-UID
rejection are currently verified by fake-API tests, not this live experiment.

### Native cancellation and timeout acceptance

`TestIsolatedSnapshotCancellationCleansTemporarySnapshots` passed on cluster
123 in 119.38 seconds for `isolated-csi-cancel-1795656350726578734`. All three
DataUploads reached `Canceled`; original and exposure snapshots were absent,
and the pre-existing probe snapshot and writer identity were unchanged. This
elapsed time is cancellation/exposure teardown, not source application pause.
The installed Velero image was unchanged, so this does not prove deployment of
the new ownership-marking plugin.

The first native timeout experiment used `spec.itemOperationTimeout: 1s` on a
new isolated Backup, without changing task status. It exposed that a Backup
can be `PartiallyFailed` before its uploads stop: an upload was still `Accepted`.
The initial test correctly failed its premature terminal-upload assertion and
its teardown subsequently confirmed all three uploads canceled. The original
failure log is preserved as `live-timeout.log`. Cleanup now requests cancellation
of still-running uploads owned by failed/canceled Backups and waits for a later
observation of their terminal phases before deleting any snapshots. Completed
Backups do not trigger forced upload cancellation; unrelated uploads are not
patched. The timeout test was updated to observe this asynchronous stop boundary
rather than equate Backup failure with data-path termination.

A subsequent retry stopped at its source-readiness guard, before creating a
Backup. The historical fixture's original startup validator materialized both
126k-record files in memory under a 64Mi limit; container status showed
`OOMKilled` and 95 prior restarts over roughly ten hours. Deployment UID and
generation stability alone therefore do not establish uninterrupted source
writing. The test-only validator was changed to stream paired records in bounded
memory while retaining sequence, checksum and cross-volume mismatch detection.
Three Python regression tests passed, including a 50k-record memory bound test.

The original script ConfigMap was immutable and the attempted in-place update
was rejected. A new test-only script ConfigMap was mounted by an explicit
isolated fixture rollout (generation 51 to 52); the original ConfigMap, PVCs and
data were retained. This was fixture repair outside a backup, not backup-time
application coordination. The new Pod logged `validated-start` at sequence
126214 with zero restarts before the next timeout experiment. No business
application or source data file was changed to force validation to pass.

With the streaming fixture running, the native timeout test passed in 108.68
seconds for `isolated-csi-timeout-1795656937085020058`. Velero reported
`PartiallyFailed` with three errors, all three uploads eventually reached
`Canceled`, and original/exposure snapshots were absent only after termination
was confirmed. The pre-existing probe and writer UID/generation were preserved;
the replacement fixture Pod remained ready with zero restarts. Final Backup,
DataUpload, snapshot/content and writer observations are saved in the external
cleanup audit directory. No failed status was rewritten as success.

A local-only temporary image `hypercdr-isolated/velero:csi-cleanup-20261126`
was built from the validated binary and executed its client-version command as
the existing non-root runtime user. It was not pushed or deployed. Native tests
above therefore exercise installed Velero cancellation and current Agent cleanup
code, while the new plugin's creation-time ownership stamping remains covered
by prepared-source component tests, not a deployed-image end-to-end run.

### Installer authorization audit

The installation template previously granted Agent read access to snapshot
classes and wildcard read-only inventory, but no VolumeSnapshot update/delete
permission. Admin-kubeconfig live tests do not prove installed Agent RBAC.
The template now explicitly grants `get/list/watch/update/delete` on
`volumesnapshots` only, without snapshot creation or mutations of snapshot
classes/contents. An installer regression test parses the actual ClusterRole
template and verifies both the required verbs and those boundaries. DataUpload
patch permission already existed in the Velero-resource rule.

Existing installed Agent roles are not changed by a binary upgrade alone;
the updated installation RBAC must also be applied before periodic cleanup can
be accepted under the real ServiceAccount. No role or business component in any
live cluster was changed by this source-template fix. Temporary Velero image
push and isolated deployment were subsequently authorized by the user.

### Temporary image acceptance in progress

The isolated image was pushed to the existing personal registry as
`hypercdr/velero:isolated-csi-cleanup-20261126`, digest
`sha256:d239aeee81468cf01ec6da49c2bbc46dabeb75d5f77b401cf0059e47a8504d6a`.
Only the isolated Velero Deployment was updated; its original manifest and the
unchanged node-agent manifest were saved in the external cleanup audit directory.
The first rollout observation timed out while the worker was pulling the image;
the old server remained available. A backup started during that pull is not
evidence for the new image. Acceptance now explicitly requires observing the
new plugin's task/Backup-UID ownership annotations on a live snapshot, in
addition to durable backup completion. Image rollout and that acceptance remain
unproven until subsequent runtime observations confirm them.

The rollout subsequently succeeded; the worker pulled the image in 145 seconds.
The cluster123 `hypercdr-agent` ClusterRole received the same narrow snapshot
rule as the installer, after its original JSON was saved. Authorization checks
confirmed snapshot deletion allowed and snapshot creation denied for the actual
Agent ServiceAccount. The live ownership cleanup test then used that account
through Kubernetes impersonation for the cleanup calls (admin remained only for
fixture setup/teardown). It passed in 28.67 seconds: the owned ready snapshot was
deleted, the unowned control remained unchanged, and writer UID/generation were
unchanged. Evidence is `serviceaccount-cleanup-live.log` in the audit directory.
This proves cleanup authorization and ownership boundaries, not periodic Agent
execution or creation-time plugin stamping on a new backup.

The transitional backup `isolated-snapshot-1795658914530090250` completed its
three uploads but correctly failed the new ownership assertion: it had been
created by the old server during image pulling. It is not a passing new-image
acceptance. The post-rollout backup `isolated-snapshot-1795659139818911555`
subsequently produced both source and exposure snapshots carrying task identity
and Backup UID `08fa7d7a-ab25-4ee5-ad14-2784bd909867`; raw evidence is saved in
`new-image-live-ownership.json`, with the running server Pod in
`new-image-server-pod.json`. Its durable completion remains under observation.

That post-rollout backup subsequently passed in 155.63 seconds: all three
DataUploads completed and the Agent validated durable Kopia snapshot evidence;
the continuously writing fixture retained its Deployment generation and ready
replica. Evidence is `new-image-success-after-rollout.log` and
`new-image-completion-result.json`. This proves CSI crash-consistent data
movement only; it does not establish application or cross-volume atomic
consistency. New-image cancellation and timeout cleanup are being revalidated
after all three successful uploads stopped.

Both new-image terminal-path tests passed: cancellation in 90.40 seconds for
`isolated-csi-cancel-1795659296113586708`, and timeout failure in 108.52 seconds
for `isolated-csi-timeout-1795659386506024704`. Each confirmed three canceled
uploads, no original/exposure snapshots, and unchanged probe/writer. The final
snapshot inventory contained only the pre-existing probe; the isolated Velero
namespace had no exposure PVCs. Evidence is recorded in
`new-image-terminal-tests-observed.log`, `new-image-terminal-uploads-final.json`
and `new-image-terminal-snapshots-final.json`. The original server image is being
restored after confirming all uploads terminal; node-agent was never updated.
The snapshot RBAC rule remains installed on cluster123 to match the corrected
installer. Agent `go test ./...` passed again after the acceptance-test changes.

Remaining acceptance limitations: automatic periodic cleanup has unit coverage
but was invoked directly by the live tests; live API deletion-error injection
and disconnected installed-Agent cleanup have not been demonstrated. No result
here proves driver-level physical garbage collection. The unchanged repository
format-gate failure noted above still prevents claiming `make verify` is green.

### Disconnection lifecycle correction

The final lifecycle inspection found that cleanup was launched by the
WebSocket session's runtime observer and stopped when that session exited.
Cleanup now runs once per Agent process, before credential loading and the
connection retry loop, using the process context. It no longer depends on a
connected socket or runtime inventory provider. Each attempt retains its
10-second timeout and the next process timer retries failures. Session runtime
observation no longer starts a duplicate cleanup path. A regression test runs
the process loop without any WebSocket, injects the first failure, verifies
subsequent attempts and checks that cancellation prevents further calls.
Focused wsclient and command tests passed. This source correction still requires
new-binary cluster validation; the installed Agent was not silently replaced.

The corrected actual Agent binary was then built into an isolated temporary
image, digest `sha256:fc4b2d918f415add88cd6d254b18d362dafe8ad672bea4a3870a870f81da210e`.
A separate Pod in the isolated Velero namespace used a dedicated ServiceAccount
bound to the corrected Agent role. Its platform endpoint deliberately refused
every connection, so no platform registration/task dispatch was possible. With
direct cleanup calls disabled in the acceptance harness, background cleanup
deleted the seeded owned snapshot and preserved the unowned control. This
passed in 12.87 seconds (`disconnected-process-cleanup-live-retry.log`). The
initial attempt exposed an acceptance-harness race: background deletion could
finish before the harness observed snapshot readiness. The harness now accepts
that deletion only for its owned fixture in explicit external-cleaner mode;
the failed original log is retained.

A second run temporarily removed only that dedicated Pod's role binding.
The Agent logged forbidden Backup-list errors and continued retrying while
platform connections still failed. Reinstating the binding allowed cleanup to
finish without restarting the process; the test passed in 12.11 seconds
(`disconnected-permission-retry-live.log`). This is live authorization/API-error
retry evidence, not an injected storage-driver delete failure. Logs and Pod
identity are in `disconnected-agent-final.log` and
`disconnected-agent-final-pod.json`. The temporary Pod, dedicated binding and
ServiceAccount are being removed after the test. Existing installed Agent
Deployments were not replaced. No stop/freeze operation was introduced.

## CSI-only delivery audit

The user-confirmed CSI crash-consistency scope replaces the old freeze/thaw
goal. Source implementation, snapshot ownership stamping, durable successful
backup, isolated restore verification, cancellation/timeout cleanup, real Agent
RBAC and disconnected process-level retry have the evidence described above.
External snapshots were preserved throughout. All temporary cleanup Agent
resources were removed; the isolated Velero server returned to its exact original
image with one ready replica, and the source writer remained ready with zero
restarts. Final snapshot inventory contains only `ledger-storage-probe`.

The 149 development API was rebuilt after the installer RBAC correction and
restarted successfully; `/healthz` returned HTTP 200. Its installed binary SHA256
matches the tested build (`a7959c65a1c4f30d3ff3675eef550359a7d724ccc2b47323f24b9ee32442ac99`).
The installer RBAC regression passed again against disposable PostgreSQL.
Agent full tests passed after the process-level cleanup correction. Frontend
behavior and isolated restore evidence were rechecked from their retained logs.

This is development/test delivery, not a published Agent/Velero release or a
fleet upgrade. Existing Agent installations require the new binary, updated
RBAC and compatible Velero ownership stamping to obtain the full behavior.
No Git commit/tag/release was created. Physical storage-driver garbage
collection and application/cross-volume atomic consistency are not claimed.
The repository-wide format gate has an unrelated existing failure; it must not
be reported as fully green.

The new RBAC test passed. The installer/cache-capability regression selection
also passed using `scripts/test-backend.sh` with a disposable PostgreSQL
container. An earlier direct run of that broader selection was rejected by the
test-database guard; it did not use a platform database. Neither test application
nor live ServiceAccount permissions were modified for this installer audit.
