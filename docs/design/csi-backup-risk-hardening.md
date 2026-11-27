# CSI backup risk hardening

## Contract

HyperCDR requests CSI snapshots, exposes their data through temporary volumes,
and moves that data into the backup repository. It does not freeze applications,
change business-volume replica counts, or silently substitute live file backup.
Independent CSI snapshots provide driver-level crash consistency, not a shared
transaction boundary across PVCs or namespaces.

Snapshot creation and successful snapshot exposure are separate operations.
Storage configuration, capacity, attachment, scheduling and driver health can
prevent a temporary volume from becoming readable even when its snapshot is
ready. Replica health is not a portable CSI admission requirement.

## Risks and controls

| Risk | Control | Verification |
| --- | --- | --- |
| CSI API exists but Velero skips CSI actions | Installer and upgrade enable `EnableCSI`; snapshot submission requires a ready, updated Velero deployment with that feature enabled | Runtime admission and executor regression tests; real snapshot DataUploads |
| Metadata-only backup reported as successful | Require immutable capture inventory, exact PVC identity, matching completed DataUploads and durable Kopia evidence | Snapshot completion and protocol rejection tests |
| Old or replaced Agent cannot prove CSI results | Control-plane dispatch requires current-session snapshot evidence capability | Backend snapshot dispatch tests |
| Upgrade erases backup PVC storage-class mapping | Merge managed node-agent settings into existing JSON, preserving unrelated keys and ConfigMap entries; reject malformed JSON | Upgrade configuration preservation regression |
| Platform task fails while uploads continue | Persist an identity-checked cancellation request on its Backup; independent cleanup loop repeatedly requests cancellation of owned uploads, including uploads discovered later | Cancellation marker tests and isolated task-failure acceptance |
| Premature cleanup interrupts active work | Wait for Backup and all uploads to become terminal; retain snapshots referenced by PVCs | Active-upload, reference and cancellation regression tests |
| Cleanup deletes someone else's snapshot | Require HyperCDR Backup task identity and snapshot Backup UID/task annotations; delete with UID and resource-version preconditions | Foreign, copied-annotation, wrong-UID/task and retry tests |
| Temporary resources left after cancellation or timeout | Velero cleanup plus independent periodic terminal-snapshot cleanup; retry failed deletions and approved orphans | Isolated cancellation and timeout tests retain a pre-existing snapshot and source writer |
| Kubernetes API hangs during admission | Bound runtime and PVC admission checks to two minutes within the task deadline | Executor uses a derived bounded context |

Task execution honors the dispatched deadline (Agent default: 30 minutes).
CSI creation and Velero item-operation deadlines remain distinct from that
deadline. Task failure requests upload cancellation; it does not claim that a
storage controller has already stopped or that garbage collection has finished.
Cancellation requests use a separate ten-second context, so an expired task
context does not itself prevent the request. The cleanup loop runs independently
of WebSocket connectivity and retries every heartbeat interval.

## Cluster 123 remediation

The `longhorn` StorageClass requests three replicas, but only `k8s-124` is a
Longhorn storage node. Snapshot cloning inherited that configuration and waited
for healthy replicas. This is a driver/environment issue, not a requirement to
validate replica counts for all CSI drivers.

The authorized test environment now has a separate
`hypercdr-backup-longhorn` StorageClass using the same CSI driver and one replica.
The node-agent `backupPVC.longhorn.storageClass` mapping uses this class only for
temporary snapshot-reading PVCs. The original StorageClass, source PVCs and
business-volume replica settings are unchanged. This is an environment-specific
setting, not a universal one-replica product default.

## Residual boundaries

- A read-only check cannot prove future CSI snapshot or clone success. It does
  not reserve storage capacity, repair a driver, or prove application consistency.
- An unavailable Kubernetes API or stopped snapshot/node-agent controller can
  delay cancellation and cleanup. Retry is safe but cannot guarantee physical
  storage reclamation while the relevant controller is unavailable.
- Snapshot objects being absent is Kubernetes-level cleanup evidence, not a
  portable proof that every storage vendor physically reclaimed all blocks.
- Capture currently inventories every PVC in the selected namespaces. A custom
  resource filter that excludes a captured PVC must fail evidence validation;
  it must never produce a successful partially protected snapshot backup.
- New safeguards are deployed to the authorized 123 Agent only. Other clusters
  need compatible Agent/Velero rollout before equivalent behavior is claimed.
- External deletion of a Backup before cleanup ownership is approved can leave
  orphaned resources. Unknown ownership must not be guessed to reclaim them.

## Acceptance evidence

Logs, Kubernetes responses and task records are kept outside the repository in
`/data/hypercdr-runtime/validation/csi-sync-failure-20261126/`.
The first retry task `7ef74022-63c5-4bc3-853f-480fd378e360` succeeded after the
initial environment remediation. Its accepted backup result contains three
captured PVCs, three snapshot evidence entries, and achieved consistency
`snapshot-crash-consistent`. This first retry included manual adjustment of its
temporary volumes and is not, by itself, evidence of unattended operation.

The unattended acceptance task `6f9d0c4c-788d-46a8-9a3f-ec4687d5490b`
succeeded on 2026-11-26 at 04:37:19 UTC. It used the saved temporary-volume
mapping without per-volume edits. Across two namespaces, all three DataUploads
completed (1,335,145,488 logical volume bytes); the platform accepted three PVC
inventory entries and three secured Kopia snapshot entries with achieved
consistency `snapshot-crash-consistent`. Execution from acceptance took about
5 minutes 34 seconds, including CSI clone attachment retries.

No temporary exposure PVCs or pods remained in the Agent namespace. The only
remaining VolumeSnapshot was the pre-existing `ledger-storage-probe`; business
pods remained Running with zero restarts. The full platform response is saved
as `unattended-task.json` and the final Kubernetes inventory as
`unattended-cleanup-proof.txt` in the evidence directory.

Three isolated live tests passed: cancellation (137.58 seconds), timeout
(143.09 seconds), and task-failure cancellation intent (147.82 seconds). Each
verified cleanup while retaining the existing probe snapshot and source writer.
See `termination-live-tests.log`.

Validation also passed: Agent `go test ./...`, targeted Agent `go vet`, and
`bash scripts/test-backend.sh ./internal/httpserver ./internal/store ./internal/protocol`.
See `agent-full-tests.log`, `agent-vet.log`, and `backend-risk-tests.log`.
The 123 deployment uses a temporary statically linked test Agent image; no
formal version, Git commit, tag, or release was created for this acceptance.
