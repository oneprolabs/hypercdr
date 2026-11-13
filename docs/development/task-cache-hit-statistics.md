# Per-task cache hit statistics

Backup details show `Data reuse`, derived from persisted overall logical bytes
and new data bytes (including resource metadata), with the reported available
reuse ratio as a fallback. Index cache counters remain collected but are not
shown as backup data reuse. This metric is independent of index cache settings.
Drill and recovery details show `Data cache hit`: cached logical file bytes
divided by logical file bytes read during restore. Neither denominator includes
the Kubernetes resource metadata package. Object-store billing, compressed
transfer bytes, retries, and prefetched data are different measurements.

Hits include data fetched earlier in the same restore and then read again from
the persistent cache, as well as data retained from previous tasks. A cold-cache
restore can therefore have a nonzero hit rate. This is not a cross-task reuse
rate and must not be presented as the percentage reduction in billed downloads.

The pinned Kopia build applies `0003-task-cache-statistics.patch` from the
existing external-source preparation workflow. A context-scoped observer starts
before the volume's repository is opened. Its counters are isolated from other
tasks and repository maintenance. Existing mapped index files are accounted as
hits; a freshly fetched index is not subsequently double-counted as a disk hit.
File reader provenance survives decompression and indirect objects. Restore's
cancel-channel behavior is retained while its observer context is preserved.

Velero carries versioned statistics through its existing progress callback and
records them in `hypercdr.io/cache-stats-v1` on PVB, PVR, DataUpload and
DataDownload objects. Annotations avoid a CRD schema migration. The final cache
snapshot is flushed before the completed, failed or canceled result callback.
The normal progress cadence is reused; no extra object-storage reads, directory
scans, HTTP requests or per-block task events are introduced.

Agent volume observations forward validated statistics through existing
`volumeProgress.items[].cacheStats`. Existing event persistence retains those
objects, and deduplication includes cache statistics even when the outer Backup
resource version is unchanged. Both task-detail entry points use the latest
task-scoped persisted observation and aggregate byte counters, never averaging
volume percentages. No message type or database schema is added.

The enabled flags represent the volume task's actual persistent-cache lease,
not the cluster's current settings. A fallback to temporary caching is not
reported as enabled persistent caching. `Disabled` means all observed volume
tasks report the cache inactive; `Mixed` means only some do. Missing, invalid or
older-version statistics show `Unavailable`, not 0% or a guessed enabled state.
An enabled cache with no measured reads shows 0.0%. Active tasks awaiting their
first observation also show an initial 0.0%, with a waiting tooltip rather than
claiming a measured hit rate. Empty or incomplete terminal observations do not
replace the latest complete task-scoped cache statistics. Backup summaries also
read the persisted final backup observation when the event list is absent.
Older historical tasks
are not retroactively assigned new statistics. Changing cluster settings does
not reinterpret historical results.

The current logical-data observer covers filesystem restore output. Other
outputs which do not report file-reader provenance must not be inferred from
directory size, elapsed time, uploaded bytes or a cluster-wide metric.

Validation includes compressed single and indirect objects, cold/warm/disabled
reads, observer concurrency and isolation, final flush, annotation preservation,
Agent parsing, deduplication, percentage aggregation and unavailable states.
