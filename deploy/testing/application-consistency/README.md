# Isolated application-consistency fixture

> Historical fixture only. Application stop/freeze qualification is no longer
> in scope. Do not stop this workload or deploy a recovery coordinator. Current
> backups use CSI crash-consistent snapshots without application coordination;
> independent snapshots do not promise identical sequences across both PVCs.

The fixture validator streams records instead of loading both files into memory,
so validation remains bounded under the container's 64Mi limit as history grows.
This does not change the fixture's strict cross-volume check or make independent
CSI snapshots application-consistent. Run its regression checks from the repo
root with `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s deploy/testing/application-consistency -p test_writer.py -v`.

This is a test-only, graceful-stop adapter qualification workload, not a generic
database consistency recipe. It appends checksummed sequences to two PVCs with
a deliberate gap between writes. SIGTERM finishes the in-flight pair before
exiting. Startup and shutdown verify identical contents, contiguous sequences
and checksums. A live pair of snapshots is not guaranteed to meet that invariant.

Use only an explicitly authorized test kubeconfig. The namespace, single-replica
Longhorn StorageClass and PVCs are isolated. Do not reuse existing application
namespaces or change the default StorageClass. Two 256Mi PVCs and a small Python
container are created. Installing cluster-wide snapshot APIs/controller requires
separate approval.

Create `writer-script` from `writer.py` in `hcdr-consistency-test` before starting
the Deployment. Manifests intentionally contain no automatic stop or backup:
stopping requires a durable consistency journal, exclusive locks and an armed
independent recovery worker. Test pause deadlines must not exceed 120 seconds.

Acceptance requires a real backup and restore with the original replica count,
verified restored sequence/checksum invariants, recorded graceful termination,
and failure/cancellation/recovery tests. Running the workload or creating CSI
snapshots alone does not satisfy application-consistent backup acceptance.

## Opt-in live checks

The live Go tests additionally require the exact authorized 123 API endpoint,
isolated namespace labels, fixture PVC labels and the independently running
recovery worker with a fresh heartbeat. They refuse to provision missing
infrastructure. Run from `agent/comm-agent`; keep evidence outside the checkout.

```bash
export HYPERCDR_CONSISTENCY_TEST_KUBECONFIG=/data/hypercdr-runtime/config/kubeconfigs/config-123
export HYPERCDR_CONSISTENCY_FAULT_EVIDENCE_DIR=/data/hypercdr-runtime/research/velero-consistency
HYPERCDR_RUN_ISOLATED_CONSISTENCY_FAULT_TEST=1 \
  go test ./internal/kube -run '^TestIsolatedOfflineVerifierFaultsResumeWithoutBackup$' -count=1 -timeout=8m -v
```

The fault check sequentially injects a verifier error, bounded verifier timeout,
task-context cancellation and a verifier that returns success without required
proof after the fixture volume users drain. Each case
requires a Failed journal, verified source restart, application startup data
validation, released locks and absence of a Backup object. Pod readiness alone
does not prove application startup validation: the test waits for the fixture's
`validated-start` output. This tests the concrete coordinator/driver verifier
boundary, not end-to-end execution of user-configured hooks, websocket
disconnect reporting or backend scheduling.

The separate backup/restore test uses
`HYPERCDR_RUN_ISOLATED_CONSISTENCY_BACKUP_TEST=1` and
`TestIsolatedOfflineCSIBackupAndRestore`. It requires an empty labelled restore
target and never deletes existing target workloads or PVCs to make a retry
pass. `HYPERCDR_CONSISTENCY_RESTORE_NAMESPACE` may select a separately provisioned,
labelled `hcdr-consistency-restored-*` namespace with matching isolated Velero
RBAC. It does not relax the empty-target check. Successful graceful verification
now persists a `paired-ledger-v1` proof containing the shutdown sequence and
source Pod UID, bound to the run, group, controller/PVC UIDs and observation
time. Its SHA-256 detects evidence changes; it is not a signature or a generic
database-consistency qualification. Restore requires both this proof and the
completed durable snapshot inventory. Earlier experimental CSI records without
proof cannot be reused as verified application-consistent records; legacy FSB
restores do not acquire this requirement.

The independent lease-expiry recovery test uses
`HYPERCDR_RUN_ISOLATED_CONSISTENCY_TEST=1` and
`TestIsolatedOfflinePauseExpiresAndRecoversWithoutCoordinator`.

### Runner restore target permissions

`restore-target-rbac.yaml` is test-only and names the fixed isolated target
`hcdr-consistency-restored-runner`. Before applying it, verify the kubeconfig
server is exactly `https://192.168.7.123:6443` and that target namespace has
`hypercdr.io/isolated-test=application-consistency`. It grants the isolated
Velero service account explicit namespaced fixture resource permissions, not
cluster-wide wildcard access. It does not grant PV patch/update: those require
separate test provisioning restricted to actual DataDownload-owned PV names.

Restore-only runner acceptance uses `TestIsolatedCompletedRunnerBackupRestore`
with `HYPERCDR_RUN_ISOLATED_RUNNER_RESTORE_TEST=1`, the completed runner JSON in
`HYPERCDR_CONSISTENCY_RESULT_FILE` and the labelled empty target in
`HYPERCDR_CONSISTENCY_RESTORE_NAMESPACE`. It validates the actual durable journal
and target service-account permissions before submission. Do not delete prior
restore evidence or change failed status to success when provisioning is missing.

### Coordinator process-exit test

`TestIsolatedCoordinatorProcessKillRecoversIndependently` requires
`HYPERCDR_RUN_ISOLATED_PROCESS_EXIT_TEST=1`, the exact123 kubeconfig and an
existing `HYPERCDR_CONSISTENCY_FAULT_EVIDENCE_DIR` outside the repository. It
starts the real isolated runner in a child test process, waits for a durable
Quiescing journal and zero writer replicas, then sends SIGKILL to that child
only. The parent never calls Resume: the separately deployed recovery worker
must verify resume, release locks and persist Failed rather than Completed.
The source must emit successful startup history validation after recovery.
This is a real process-exit test, not a websocket-disconnect reporting test.

`TestIsolatedRecoveredFailureSurvivesWebsocketReconnect` additionally uses
`HYPERCDR_RUN_ISOLATED_CONSISTENCY_RECONNECT_TEST=1` and an actual recovered
process-exit record in `HYPERCDR_CONSISTENCY_RESULT_FILE`. It only reads the123
journal, then tests closed-transport failure delivery, disk outbox reopening,
replay and acknowledgement with a local websocket peer. It does not pause the
source or verify the real backend acknowledgement handler.
# Read-only completion binding check

From `agent/comm-agent`, use an existing completed isolated runner record:

```bash
HYPERCDR_RUN_ISOLATED_COMPLETION_READ_TEST=1 \
HYPERCDR_CONSISTENCY_TEST_KUBECONFIG=/data/hypercdr-runtime/config/kubeconfigs/config-123 \
HYPERCDR_CONSISTENCY_RESULT_FILE=/data/hypercdr-runtime/research/velero-consistency/live-coordinated-runner-second-record.json \
go test ./internal/kube -run '^TestIsolatedConsistencyCompletionReadOnly$' -count=1 -v
```

This only reads the authorized 123 fixture and existing Backup/DataUploads. It
does not pause workloads, create resources, repair missing evidence, or publish
to the platform. It requires the fixture namespace safety labels and exact
journaled PVC identities, snapshot classes, upload UIDs and Kopia handles.

# Actual Pause/Disconnect Acceptance

The destructive portion is restricted by code to cluster 123, the labelled
`hcdr-consistency-test` writer fixture, and its qualified two-volume profile.
It closes a local test websocket only after observing a real durable pause,
then requires safe resume, successful evidence/metrics and terminal replay.
It injects the runner only in the test; it does not enable production execution.

From `agent/comm-agent`, explicitly opt in with:

```bash
HYPERCDR_RUN_ISOLATED_PAUSE_DISCONNECT_TEST=1 \
HYPERCDR_CONSISTENCY_TEST_KUBECONFIG=/data/hypercdr-runtime/config/kubeconfigs/config-123 \
HYPERCDR_CONSISTENCY_FAULT_EVIDENCE_DIR=/data/hypercdr-runtime/research/velero-consistency \
go test ./internal/wsclient -run '^TestIsolatedWebsocketDisconnectDuringPausePreservesVerifiedCompletion$' -count=1 -v
```

The first attempt failed successful completion because the capture lease
expired. The source resumed safely and the original native Backup subsequently
completed, but the consistency journal correctly remained Failed. Preserve
the artifacts and inspect the original Backup before considering another run.
Do not interpret the ordinary Agent suite (which skips live opt-ins) as this
live acceptance passing. Read-only replay can use the retained task/state via
`HYPERCDR_CONSISTENCY_DISCONNECT_TASK_FILE` and
`HYPERCDR_CONSISTENCY_DISCONNECT_STATE_DIR`, with
`HYPERCDR_RUN_ISOLATED_PAUSE_FAILURE_REPLAY_TEST=1`, selecting
`TestIsolatedPausedDisconnectFailureReplaysWithoutNativeSuccessPromotion`.

After implementing the bounded readiness window, the isolated retry
`c11e669a-f858-4b3d-8671-80c25f78063d` passed in 172.15 seconds. Its restore
request preceded the capture deadline, while verified readiness followed that
deadline at 92.05 seconds from run start, still within the 120-second limit.
Two Kopia snapshots and a completed journal were verified; the completion and
full metrics survived outbox reopening and replacement-local-websocket replay.
The original failed run remains retained. This passing retry does not prove 149
backend persistence/acknowledgement or a restore of this specific backup.

The new runner requires a fresh recovery heartbeat containing
`resumeWindow=fenced-ready-v1`. Upgrade only the isolated worker before retrying;
old workers are rejected at preflight. For the Alpine test container, build the
worker with `CGO_ENABLED=0`, use a distinct download URL, update the expected
SHA256 in the init command, and verify the actual fresh capability heartbeat.
A successful Deployment rollout alone does not prove the recovery loop is live.
Keep all binaries, logs and generated deployment variants in the runtime tree.
