# Agent ownership across Community and Enterprise

Community and Enterprise use the same Community-owned Agent distribution. A
Kubernetes cluster can be owned by exactly one HyperCDR control plane at a
time. Namespace separation is not an edition boundary: Velero CRDs, cluster
inventory, CSI APIs, and object-storage repositories cannot be made safe for
concurrent control by installing a second Agent.

## Canonical runtime

| Resource | Canonical owner/name | Edition handover rule |
| --- | --- | --- |
| Agent namespace | `hypercdr-agent` | Retain; namespaces are never renamed. |
| comm-agent Deployment | `hypercdr-comm-agent` | Rotate endpoint and credential only. |
| Velero Deployment | `velero` | Retain without reinstalling. |
| node-agent DaemonSet | `node-agent` | Retain without reinstalling. |
| Agent state PVC | `hypercdr-agent-state` | Retain the PVC and bound PV identity. |
| Secrets and ServiceAccounts | Fixed names in `hypercdr-agent` | Snapshot the old control-plane credential before handover. |
| Agent ClusterRole/Binding | `hypercdr-agent` | Reconcile in place; never create an edition-suffixed peer. |
| Velero ClusterRole/Binding | `hypercdr-velero` | Reconcile in place. |
| Velero custom resources and CRDs | Existing cluster resources | Retain BSL, repository, backup, restore, and data-mover state. |
| Object-storage and Kopia state | Existing repository and prefix | Preserve in place; only control-plane ownership changes. |

## Required lifecycle invariants

1. The standard installer scans all namespaces before changing cluster state.
   Any existing `hypercdr-comm-agent` causes installation to fail with guidance
   to use Community Migration or Disaster Handover.
2. Both editions generate commands for the canonical `hypercdr-agent`
   namespace. There is no force flag for installing a second Agent.
3. Community-to-Enterprise migration freezes Community writes, verifies that
   no backup, restore, drill, delete, or repository-maintenance task is active,
   and then changes only the Agent control-plane endpoint and credential.
4. Velero, node-agent, Kopia, object-storage settings, PVCs, cluster identity,
   and historical resources remain unchanged during handover.
5. Before migration commit, an unsuccessful handover restores the encrypted
   credential snapshot and reconnects the same Agent to Community.
6. Disaster Handover is a separate audited recovery workflow for a permanently
   unavailable source control plane. It is never an installer bypass.
7. Normal unregister remains destructive and is not used for edition migration.

## Release validation

Every installation or handover change must prove:

1. A fresh Community or Enterprise control plane installs the same Agent into
   `hypercdr-agent`.
2. A second installation from either edition is rejected before namespace,
   Secret, RBAC, workload, PVC, Velero, or object-storage mutation.
3. A successful handover preserves Kubernetes cluster UID, namespace UID, PVC
   UID, BSL, Kopia repository, and historical restore-point relationships.
4. An injected target-connection or verification failure restores the source
   endpoint and credential within the configured rollback timeout.
5. Commit makes Enterprise the sole writer and leaves Community read-only for
   the configured observation period.
