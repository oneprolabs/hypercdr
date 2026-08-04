# Drill recovery implementation audit

Date: 2026-08-03

## Scope

This audit records the implementation baseline for the staged Drill recovery
work. It distinguishes functionality that exists today from functionality that
must be implemented before it is exposed in the UI.

## Current recovery path

1. The frontend posts a recovery request to `POST /api/v1/tasks/drill`.
2. The platform resolves the selected restore point and dispatches a
   `RestoreCommand` to the target Comm Agent.
3. The agent creates a Velero `Restore` and a Resource Modifier ConfigMap.
4. Velero restores Kubernetes resources and persistent data.
5. After Velero reports completion, the agent checks namespace, Pod and
   workload readiness.
6. The platform stores the latest task status and diagnostic payload.

The path already distinguishes Velero completion from readiness in progress
messages, but it does not persist a first-class stage model.

## Capability matrix

| Capability | Baseline | Implementation decision |
| --- | --- | --- |
| Namespace mapping | Supported | Keep and expose as the target namespace. |
| Namespace CR restore | Supported by the full Velero restore | Include by default. Never add product-specific implicit exclusions. |
| Default exclusions | No product-specific defaults | Preserve the empty default and its regression tests. |
| Cluster-scoped resources | Velero boolean only | Keep disabled by default; design an administrator-controlled policy later. |
| PVC rebinding | Supported | Existing Resource Modifier clears `spec.volumeName`. |
| Drill NodePort handling | Supported | Existing Resource Modifier removes the restored NodePort allocation. |
| StorageClass mapping | Not implemented | Add a validated Resource Modifier rule before exposing the option. |
| Image mapping | Not implemented | Add exact-image and registry-prefix transforms with manifest-level tests. |
| Custom JSON Patch | Not implemented | Defer until allow-listing and patch preview are available. |
| Data-only restore | Explicitly rejected | Do not expose in the first Drill UI release. |
| Historical resource catalog | Not stored | Build from the selected Velero backup contents; do not use current cluster inventory. |
| Per-kind resource selection | Not implemented | Implement only after the historical catalog is available. |
| Per-object resource selection | Not implemented | Defer until dependency and Velero selection semantics are proven. |
| StorageClass inventory | Supported | Use target inventory for factual validation. |
| API/GVK inventory | Partially supported | Extend the target inventory or use discovery during per-task preflight. |
| Workload readiness | Supported | Preserve evidence and promote it to a first-class task stage. |
| Registry failure classification | Partially supported | Split timeout, authorization, not-found and TLS causes. |
| PVC and volume diagnostics | Partially supported | Promote volume progress and PVC evidence to a first-class task stage. |
| Application-specific validation | Not implemented | Defer to the advanced phase; workload validation remains the default. |

## Restore-point data finding

The platform restore-point record currently contains the Velero backup name,
source namespace, timestamps, size, storage location and summary metadata. It
does not contain the historical manifests or an object-level resource catalog.

Consequently, resource selection and transform preview must be derived from the
selected backup. Source-cluster inventory is not a valid substitute because it
describes current state rather than point-in-time backup contents.

## Default exclusion finding

`defaultExcludedResources` intentionally returns an empty list, including for
`kasten-io`. Existing tests enforce this behavior. Protection-plan exclusions
remain explicit user configuration and are passed to Velero unchanged.

The current real backups for `backup-test-no-pvc` and `demo-mysql-csi` exclude
only CSI `VolumeSnapshot` and `VolumeSnapshotContent` resources according to
their protection-plan configuration. No Kasten Action CR default is present in
the current code path.

## Real-cluster baseline

The inspected Velero restores for both baseline applications report
`Completed`:

- `backup-test-no-pvc`: Kubernetes resource restore completed; the HyperCDR
  task subsequently failed during workload readiness because the restored Pod
  could not pull its image.
- `demo-mysql-csi`: Kubernetes resource restore completed and the application
  has previously completed Drill successfully.

This confirms that a single task-level `Failed` label is insufficient. The new
model must preserve successful restore stages when a later validation stage
fails.

## Phase 1 constraints

- Add stage data without breaking existing task API consumers.
- Continue populating legacy `status`, `progress`, `message`, `errorCode` and
  `errorMessage` fields.
- Persist stage snapshots inside the task payload first; a normalized table is
  unnecessary until stage history volume proves it is needed.
- Do not expose unsupported restore modes or mappings in the UI.
- Keep raw Velero and Kubernetes evidence alongside normalized stage results.

## Baseline verification

- Comm Agent Velero, Kubernetes and executor tests: passed.
- Platform recovery API and default-exclusion regression tests: passed.
- Real source/target Velero Backup and Restore CR inspection: completed.

