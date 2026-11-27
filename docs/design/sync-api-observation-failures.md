# Sync API observation failures

A failed Kubernetes API read is not proof that a data transfer failed. CSI
DataUpload progress checks defer timeout, server timeout, throttling and service
unavailable responses until the next controller progress check. The existing
backup item-operation timeout remains authoritative. Missing or duplicate uploads,
permission failures and confirmed failed transfers still fail the operation.

For failed or partially failed backups, the communication Agent reads the matching
backup's persisted Velero results from its configured backup storage location.
Diagnostic collection is bounded to 20 seconds and 4 MiB of decompressed JSON.
Original errors are attached as `velero.backupResults` and persisted as user-facing
failure details. Failure to collect diagnostics is reported separately and never
changes the backup outcome. Validation failures preserve their original message.

DataUpload API-list timeouts use `SYNC_API_READ_FAILED`, distinct from transfer or
volume-mount failures. Completed DataUploads alone never upgrade a partially
failed backup to success or prove that a usable restore point exists.

## Observed incident, 2026-11-26

Task `693dc4be-823b-4f1b-995e-f13d003fc7bb` waited approximately 16–20 minutes
between DataUpload acceptance and transfer start. Source-cluster events reported
Longhorn attachment delays, CSI socket disappearance and mount EOFs. Longhorn
liveness/startup probe failures and repeated CSI restarts were observed; etcd also
logged slow reads and WAL `slow fdatasync` events of approximately 1–2.6 seconds
during the incident. This establishes delayed control-plane disk flushes, but not
whether the underlying disk, virtual host contention or another host issue caused
them. The node did not report memory or disk pressure; memory exhaustion must not
be asserted without additional evidence.

All three DataUploads eventually completed, but Velero had already marked one
asynchronous operation failed after an API-list timeout. The backup correctly
remains unsuccessful pending a new, fully verified run. These resilience changes
do not repair Longhorn or etcd performance and do not rewrite historical outcomes.
