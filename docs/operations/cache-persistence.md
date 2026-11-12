# Kopia cache persistence

Open **Configuration → Clusters → cluster actions → Cache settings**.
Persistence defaults to enabled with a **5 GiB budget per node**. Administrators
can change the budget (1–1024 whole GiB), disable persistence, or confirm an idle
cache cleanup. Desired settings are stored in PostgreSQL and re-applied when an
agent registers. Settings and cleanup operations have persisted task results;
saving desired settings alone does not imply successful cluster application.

## Storage and consistency

- Index cache defaults to `/var/lib/hypercdr/cache/index` on each node; Drill
  data cache defaults to `/var/lib/hypercdr/cache/drill-data`. The two host paths
  are independently configurable and must not overlap. Container mount paths
  are `/var/cache/hypercdr` and `/var/cache/hypercdr/drill-data`, respectively;
  their nesting does not imply nested host directories. No StorageClass or PVC
  is required.
- Index persistence defaults to enabled; Drill data persistence defaults to
  disabled. Index persistence stores metadata and indexes, not business-data
  contents. Restores use persistent business-data contents only when Drill data
  persistence is enabled, independently of the index switch. Backups do not use
  the Drill data cache.
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

Configuration changes are rejected while a sync or Drill is active. A persisted
configuration task gates new cache users while the node-agent template updates
and reaches rollout readiness. Disabling a cache or changing its directory
requires confirmation and records the old paths for a separate `cache-cleanup`
task after configuration succeeds. New tasks use the new settings and do not
wait for old-directory cleanup.

The settings drawer reports configuration application only. Cleanup progress and
results appear in the cluster's Recent Tasks list. The Agent creates one Job per
node and old directory, with five retries using Kubernetes Job backoff and a
24-hour deadline. Failed attempts are progress, not terminal task failures;
the task fails when a Job reaches its failure condition. Agent restart recovery
reuses task-bound Job names so it does not reset retry limits. Failed cleanup
can be retried from its task row; the request includes `taskId`, is scoped to the
selected cluster, and creates a new task with `retryOfTaskId` for traceability.
Enabled cache paths and overlapping directories are rejected as isolation targets.
Old directories are renamed into fixed isolation directories before settings
success; cleanup deletes only the isolated data, never a reused original path.
An isolation failure rolls back directories and configuration without scheduling
deletion. Uncertain recovery retains task-start protection. The Agent persists
transition phases and checks unfinished cache tasks every 30 seconds.
Observed paths and cleanup safety checks resolve the node-agent's HostPath
volumes through its volume mounts. Container mount paths are not node paths;
the fixed `/var/cache/hypercdr` mount must not be reported as a custom host path.
Backup objects, restore points, and volume data are never deleted by cleanup.

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

Changing cache size alone preserves existing data and does not create a cleanup
task. A smaller size takes effect through normal cache eviction during later use;
disk usage need not decrease immediately when settings are saved.

## Verification

Compare repeated syncs of the same application with unchanged volume content and
the same node placement. Separate task-pod downloads from completion statistics,
Velero discovery, and retention cleanup. Record packet-capture drops and response
matching completeness. Cold-cache runs are expected to download existing index
and metadata packs; warm runs should reuse unchanged cached objects. Network
bytes are not a cloud invoice: billing also depends on provider request classes,
location, and traffic pricing.
