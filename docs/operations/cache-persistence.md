# Kopia cache persistence

Open **Configuration → Clusters → cluster actions → Cache settings**.
Persistence defaults to enabled with a **5 GiB budget per node**. Administrators
can change the budget (1–1024 whole GiB), disable persistence, or confirm an idle
cache cleanup. Desired settings are stored in PostgreSQL and re-applied when an
agent registers. Settings and cleanup operations have persisted task results;
saving desired settings alone does not imply successful cluster application.

## Storage and consistency

- Cache lives at `/var/lib/hypercdr/kopia-cache` on each node, mounted at
  `/var/cache/hypercdr` in new task pods. No StorageClass or PVC is required.
- This uses Kopia's native content, metadata, and index caches. Kopia still
  discovers remote repository changes; persistence does not turn remote
  repository discovery off. It does not cache credentials or repository config.
- File-system backup shards are isolated by backup location UID, repository UID,
  and PVC UID. Non-PVC volumes use namespace/pod/volume identity. CSI backup
  shards use repository-scoped source namespace/PVC name. Moving a task to a
  different node produces a cold cache on that node.
- A shard is exclusively leased for the task lifetime. A concurrent task using
  the same shard falls back to temporary caching rather than sharing a writable
  directory. New and changed remote objects still require downloads.
- Each shard reserves at least 256 MiB against the node budget; Kopia's native
  content and metadata limits together are 256 MiB per shard. Idle shards are
  evicted oldest-first when required for admission, or after seven days when a
  new task acquires cache. There is no background object-storage size scan.
- The budget is **not a filesystem quota**: native indexes and active writes can
  temporarily exceed the reservation. Admission requires remaining free disk
  above the larger of 5 GiB and 10% of the filesystem, plus one shard reservation.
  Admission/locking errors fall back to temporary caching. Monitor node disks;
  an external filesystem quota is needed if a strict physical cap is required.

## Applying and clearing

Changes update the node-agent template and wait for rollout readiness; existing
task pods retain their own settings and mounts. Disabling persistence does not
delete existing files. Cleanup starts a short-lived job on each currently
represented node-agent node and removes only idle cache directories under the
dedicated root. Active shards remain leased and are skipped. Backup objects,
restore points, and volume data are never deleted by cache cleanup.

Cleanup requires namespace-scoped batch job create/get/delete permissions. An
older installation must receive the corresponding RBAC update as part of rollout.
Cleanup jobs do not mount a service-account token. Nodes no longer represented by
node-agent pods require separate operational cleanup after confirming no tasks
remain there.

Only patched Velero images support this feature. The node-agent template must
declare `hypercdr.io/cache-persistence-supported: v1` and bind capability to the
exact installed image with `hypercdr.io/cache-persistence-image`. The development
image convention is a tag ending in `-cache-v1`; installation and upgrade paths
set these annotations for this convention. Unsupported images reject explicit
settings changes rather than reporting persistence as successfully applied.
Existing agents ignore optional protocol fields during rolling upgrades.

## Verification

Compare repeated syncs of the same application with unchanged volume content and
the same node placement. Separate task-pod downloads from completion statistics,
Velero discovery, and retention cleanup. Record packet-capture drops and response
matching completeness. Cold-cache runs are expected to download existing index
and metadata packs; warm runs should reuse unchanged cached objects. Network
bytes are not a cloud invoice: billing also depends on provider request classes,
location, and traffic pricing.
