# Backup size statistics and background observation

HyperCDR-created BackupStorageLocations explicitly set both `backupSyncPeriod`
and `validationFrequency` to `3m`. These per-location settings override the
upstream Velero server defaults without modifying Velero. Backup discovery and
storage validation may consequently take about three minutes between checks;
scheduled backup execution is unaffected. Existing locations need their spec
updated separately, or through a storage sync using the updated Agent.

Final restore-point measurements are produced by the task completion path and
persisted by the control plane. The Agent still reads the backup's object list
and volume-info artifact when collecting that result. Velero is unchanged.

Periodic Velero observations and inventory snapshots only report Kubernetes
state and volume progress. They do not read backup objects or scan repository
prefixes for size measurements, including after an Agent restart. Event and
inventory deduplication occur after collection and must not be relied on to
prevent object-store reads.

Task completion continues to use the reliable event outbox and task ledger.
Unacknowledged messages are resent from their persisted payload rather than
recollecting statistics. Recoverable tasks without an outbox result continue
through task status polling. Background observations are not a replacement for
the final completion result or a mechanism for repairing missing measurements.

Plan repository usage is still sampled as part of task completion. This change
removes repeated idle scans, not all size-related object-store requests, and
does not introduce a new repository usage refresh policy. Missing completion
measurements and repository usage refresh after cleanup require separate
lifecycle handling; periodic observation must not silently resume full scans.
