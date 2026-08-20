# Community and Enterprise resource isolation

Community and Enterprise may manage the same Kubernetes cluster at the same
time. Isolation is based on ownership, not on assuming that only one Velero
installation exists.

## Ownership matrix

| Resource | Community owner/name | Enterprise owner/name | Cleanup rule |
| --- | --- | --- | --- |
| Agent namespace | `hypercdr-agent` | `hypercdr-enterprise-agent` | Delete only the task namespace. |
| comm-agent Deployment | `hypercdr-comm-agent` in its namespace | Same name in its namespace | Namespace-scoped. |
| Velero Deployment | `velero` in its namespace | Same name in its namespace | Namespace-scoped. |
| node-agent DaemonSet | `node-agent` in its namespace | Same name in its namespace | Namespace-scoped. |
| Agent state PVC | `hypercdr-agent-state` in its namespace | Same name in its namespace | Namespace-scoped; the provisioned PV is owned through the PVC. |
| Secrets and ServiceAccounts | Fixed names in the edition namespace | Fixed names in the edition namespace | Namespace-scoped. Never read or delete the peer namespace. |
| Agent ClusterRole/Binding | `hypercdr-agent` | `hypercdr-agent-<sha256(namespace)[0:8]>` | Upgrade and uninstall resolve the name from the current namespace. |
| Velero ClusterRole/Binding | `hypercdr-velero` | `hypercdr-velero-<sha256(namespace)[0:8]>` | Delete only the current namespace's derived name. |
| Velero custom resources | Namespaced under the edition namespace | Namespaced under the edition namespace | Delete only CRs in the current namespace. |
| Velero CRDs | Shared Kubernetes API definitions | Shared Kubernetes API definitions | Never delete during install rollback. During uninstall, retain while any peer Velero/HyperCDR controller or CR exists. |
| Platform database, credentials and tasks | Community control-plane database | Enterprise control-plane database | Each platform mutates only records addressed by its authenticated agent connection and cluster ID. |
| Object-storage data | `hypercdr/clusters/<source-cluster-id>/` | Same domain contract with an independently issued cluster ID | Delete only prefixes persisted in the current platform's storage bindings. A deliberate Community-to-Enterprise migration preserves IDs and transfers ownership; cloned databases must not run concurrently. |

## Required lifecycle invariants

1. Installation may detect the peer Velero installation but must not reject it
   or require `--allow-existing-velero`; only an installation in the same
   namespace is treated as a reinstall.
2. Upgrade permission reconciliation may modify only the current namespace's
   derived ClusterRole.
3. Normal unregister is ordered: object-storage cleanup, cluster-side cleanup,
   agent success response, then platform-record deletion.
4. Failure before the agent success response leaves the platform record and
   task history retryable. The task event retains the underlying Kubernetes or
   storage error.
5. Force removal is platform-only and must state that cluster and storage
   resources may remain.
6. Removing one edition must leave the peer namespace, RBAC, workloads, PVC,
   credentials, CRs, CRDs and agent connection healthy.

## Release validation

Every change to installation, runtime RBAC, upgrade or unregister must test the
cross-product, not only two independent happy paths:

1. Install Community, then Enterprise; verify both become online.
2. Unregister Community; verify Enterprise remains ready and can inventory the
   cluster.
3. Reinstall Community; verify both become online.
4. Unregister Enterprise; verify Community remains ready and can inventory the
   cluster.
5. Reinstall Enterprise and verify both final namespaces, scoped RBAC objects,
   PVCs, Velero CRs and platform records.

Velero CRDs are intentionally shared cluster-scoped resources. Claiming that
they are physically isolated per edition would be incorrect; their lifecycle
is protected by peer-instance detection instead.
