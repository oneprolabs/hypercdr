# Persistent cache acquisition

Cache contents can change while a restore is running. Kopia replaces temporary
configuration and cache files, so a directory entry can disappear between
enumeration and `Info`. Size scans ignore only `ENOENT` in this case. Permission,
I/O, and symbolic-link errors still reject the cache rather than silently
accepting an unsafe directory. Measurements are live estimates, not filesystem
snapshots.

Acquisitions serialize capacity accounting and eviction with the root's
`.allocation` file lock. Otherwise concurrent callers could both observe unused
capacity before either creates its shard. This lock is held only during
acquisition, not for the lifetime of a backup or restore. The existing shared
root guard prevents cleanup during acquisition; shard leases continue to prevent
eviction of caches in use. All acquisition failures release their locks.

Regression coverage includes file replacement during another volume's cache
acquisition, concurrent single-shard-budget admissions, active lease protection,
idle eviction, shared identities, and symbolic-link rejection:

```sh
GOWORK=off go test -race -count=20 ./internal/persistentcache
```

These changes run inside Velero's node-agent, not the HyperCDR platform API.
Deploying platform code alone does not install the fix. Build a new Velero image
from this source, publish and verify its digest in the release manifest, and
upgrade the target cluster's Velero/node-agent before testing a new drill. Do not
overwrite or reinterpret historical task statistics.
