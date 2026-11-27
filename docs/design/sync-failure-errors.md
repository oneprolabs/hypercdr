# Sync failure reporting

Backup failures retain the existing `ErrorCode`, `Message`, `Details` event
contract. `Details.originalErrorCode` and `Details.originalMessage` preserve
the original report. Existing Velero status and per-volume identities remain
available in task details. No database or wire schema change is needed.

The frontend uses its existing six-digit code, title, description and detailed
resolution format. `130101`–`130116` cover storage authorization/capacity, TLS,
RBAC, repository health, CSI runtime/prerequisites/creation, temporary mounts,
worker resources, volume readability, connectivity, deadlines, observation,
unverified evidence and unknown failures. The catalog is
`frontend/src/features/recovery/sync-error-catalog.ts`.

Classification uses reported status and failed-volume messages, not successful
volume messages or arbitrary resource specifications. Text matching is a
best-effort compatibility layer for vendor errors, not proof of root cause.
Unknown causes retain the raw report and diagnostic instructions. Evidence
validation failures remain evidence failures even when their text contains a
transport error. Explicit sync classifications take priority over legacy UI
message matchers, preventing backup read-only errors from being shown as
restore failures.

Failed DataUploads/PodVolumeBackups are reported on the next successful volume
poll rather than waiting for the whole Backup to become terminal. CSI failures
continue through the identity-checked cancellation/cleanup path. A transient
API read failure emits a retrying observation update, not a fabricated backup
failure. Pending attachment, scheduling and slow transfer are not inherently
terminal; a deadline without authoritative evidence is reported as a deadline,
not an invented storage diagnosis.

## Limits

- A disconnected Agent cannot immediately deliver an error to the platform.
  Existing reliable event delivery retries after connectivity is restored;
  platform offline status and task deadlines remain separate signals.
- Kubernetes events that never reach Backup/DataUpload messages cannot be
  inferred by this classifier. Inspect the referenced PVC/Pod/controller events
  using the catalog instructions; not every vendor-specific failure is known.
- Cancellation request or terminal task status does not prove that a storage
  controller has already reclaimed physical blocks. Independent cleanup retries
  continue; cleanup failure must not be confused with backup data corruption.
- This change does not introduce automatic repair, unlimited retry, business
  volume changes, application freezing, or silent file-level fallback.

## Validation

Agent classification tests cover known categories, unknown errors, deadline
and API fallbacks, caller immutability, original evidence retention and ignoring
completed-volume errors. A frontend contract test requires unique six-digit
codes and coverage of every Agent sync classification. Run the Agent suite,
the frontend sync catalog test and the existing frontend build/task tests.
