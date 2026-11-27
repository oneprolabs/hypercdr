# Automatic backup temporary-volume configuration

CSI snapshot backups retain the standard Velero snapshot → intermediate PVC →
data movement flow. An optional Agent preflight configures Velero's `backupPVC`
mapping; it never changes application PVCs, PVs, snapshots or source StorageClasses.

## Initial adapter

Only `driver.longhorn.io` classes explicitly specifying `dataEngine=v1` or
`dataEngine=v2`, with no
`fromBackup` source, are automatically adapted. The generated class copies the
source parameters, mount options, binding mode and topology, sets
`numberOfReplicas=1` and `reclaimPolicy=Delete`, and does not inherit default-class
annotations. Unknown drivers and unrecognized engines retain the source class.
This is replica-overhead reduction, not a promise of fast clone/COW.

The common policy owns runtime capability checks, discovery, source isolation,
ownership/collision checks, deterministic naming, idempotent creation, and safe
ConfigMap updates. Driver-specific parameter decisions live in individual
`backup_volume_policy_<driver>.go` adapters, registered by provisioner in
`backupVolumeAdapters`. An adapter receives a detached parameter map and returns
false when the source configuration is unsupported. New adapters must preserve
backend identity and topology and supply parameter and mapping-safety tests;
do not generalize Longhorn's replica setting to other drivers.

The Longhorn adapter preserves the selected data engine and does not enable V2,
configure SPDK/hugepages/disks, or migrate existing volumes. The source V2 PVC
must already be operational and support CSI snapshots. Both engines retain
readOnly=false and standard snapshot-based provisioning; no linked-clone flag
is injected. V1 has passed live sync verification; V2 currently has automated
policy/mapping tests only because the validation cluster has V2 disabled.

References: Longhorn 1.11 [StorageClass parameters](https://longhorn.io/docs/1.11.0/references/storage-class-parameters/)
and [V2 volume clone support](https://longhorn.io/docs/1.11.0/v2-data-engine/features/volume-clone/).

The class name hashes the source name/UID and effective configuration. Classes
are created idempotently and retained for reuse; only intermediate PVCs are
task-scoped. Existing source-class mappings, including administrator readOnly
choices, are preserved. Name collisions or altered generated classes are not
adopted. ConfigMap conflict retries preserve all unrelated configuration.

Automatic mappings explicitly use `readOnly=false`. No driver currently has a
verified automatic ReadOnlyMany adapter. Do not infer snapshot-to-ReadOnlyMany
support from read-only pod mounts or CSI driver identity. There is no trial-true
then timeout-and-fallback on the production backup path. Future adapters must
validate driver/version/backend compatibility and demonstrate a benefit before
enabling readOnly. No automatic file-level fallback is added.

## Runtime and installation

Upstream Velero initially reads backupPVC mappings at node-agent startup. The
HyperCDR runtime refreshes only this mapping before each new snapshot exposure,
using the configured node-agent ConfigMap. Existing transfers and their selected
classes are unchanged; concurrency/cache settings are not hot-reloaded. No
workload restart is needed to add mappings.

Automatic configuration requires an image-bound backup-pvc-refresh capability on
the node-agent DaemonSet. Old runtimes are left untouched. The initial development
image marker is `backup-volume-policy-v1`; managed upgrades and native installers
set or clear its capability metadata with the image. Release promotion must add
the immutable release image to this capability allowlist, not mark older images
as capable. Installation grants StorageClass create, not update/delete access.

Optimization failure is logged and standard CSI submission continues, unless the
required snapshot prerequisites themselves fail. Existing mappings are never
silently removed after an error. Enabling this optimization does not prove future
provisioning success or eliminate storage/API failures.
