# CSI Sync preparation performance

## Scope

These optimizations apply to CSI DataUploads independently of the storage driver.
They do not change business volumes, replica counts, application availability,
snapshot consistency, or cluster-wide scheduling/attachment retry settings.
The deployment defaults allow four data transfers per node and a preparation
queue capacity of four. Per-DR transfer configuration is deferred.

## Standard snapshot access

Create the temporary PVC with a VolumeSnapshot data source through the standard
Kubernetes API. Preserve the existing backupPVC StorageClass mapping; the CSI
driver and storage backend determine how the snapshot becomes a volume. Do not
automatically probe clone modes, inject vendor-specific parameters, or delete and
recreate temporary resources to retry a different cloning strategy. Standard CSI
does not report whether the backend used COW or a full copy, so readiness and
elapsed time must not be presented as proof of either method. Snapshot access
failures remain failures; they never silently switch to live file-level backup.

## Readiness handoff

Pod update handlers only enqueue DataUploads; they do not write API objects or
retry status updates on the shared informer handler. The reconciler promotes an
Accepted upload when its exposing Pod is Running on the local node, carries the
matching upload label, and is controlled by the current DataUpload UID.

An optimistic-lock patch rejects concurrent cancellation/status changes rather
than overwriting them. Accepted uploads retry after five seconds, using the
controller cache for Pod reads, so missed events and transient API write failures
do not leave an already-ready volume waiting for the old minute-based monitor.
Existing exposing-volume validation, cancellation and timeout handling remain.

## Transfer capacity and cleanup

After the data mover has terminated successfully, its completion callback
releases transfer capacity before cleaning temporary resources. The watcher
remains registered until cleanup and completion handling finish, preventing a
missing watcher from being mistaken for an abandoned transfer during cleanup.
Capacity release is idempotent and mutex-protected. It does not increase the
configured limit on active transfers. Failure/cancellation retain their existing
capacity-release behavior.

DataUpload completion is still persisted after cleanup. Snapshot ownership and
cleanup boundaries are unchanged; unrelated snapshots are not touched.

## Limits and validation

This removes controller-side idle time, not storage-driver work. A driver may
still perform a full clone, format/check a filesystem, or wait for attachment.
Do not promise a fixed total-time improvement or silently use file-level backup.

Regression coverage includes lost readiness events, pending Pods, foreign owner
UIDs/nodes, failed readiness patches, queue-only event handlers, capacity release
before cleanup, preserved watcher visibility, and unchanged concurrency limits.
Build using `deployments/prepare-patched-source.sh` to include the pinned Kopia
patches. Upgrade both server and node-agent through the managed upgrade path,
preserving EnableCSI and image-bound capability metadata.
