# Cluster sync settings

Each native managed source cluster has two persisted settings: simultaneous sync
tasks (default 4, range 1–20) and transfers per node (default 4, range 1–32).
The cluster card and list share More → Sync settings. OADP-managed runtime changes
are explicitly rejected until its operator configuration path is supported.

Platform admission is serialized by a PostgreSQL cluster-row lock. Unfinished
backups with an admission reservation, plus already dispatched legacy backups,
consume capacity. Unadmitted queued backups are admitted oldest-first. Queued
tasks have no dispatch/start timestamp and do not consume execution timeouts.
Reservations survive API restarts and cease consuming capacity at terminal
completion, including failure/cancellation. Same-DR task creation remains guarded
by the existing protection-plan transaction lock; schedule overlap is skipped,
not accumulated. Queued, unadmitted backups can be cancelled without an Agent.

Only configured clusters are reconciled automatically. Changing runtime settings
drains active data operations before a managed same-image Velero upgrade updates
server backup concurrency and node-agent transfer concurrency. New admissions
wait during pending configuration and rollout; previously admitted work continues.
The preparation queue remains internal at 4. A failed apply is visible and requires
an explicit save to retry. Save and applied states are distinct. Same-image
heartbeats cannot confirm the new settings until the Agent has acknowledged
successful rollout. No custom per-task transfer fairness or preemption is added.

Admission and settings-apply task creation use the same cluster lock, so a runtime
restart cannot race a new platform data-task admission. Manual Kubernetes workloads
outside HyperCDR are not governed by this platform queue. Native installation
preserves an existing node-agent ConfigMap and server concurrency; platform
repair/upgrade uses the saved per-cluster settings instead of hardcoded defaults.
Concurrency-only changes preserve cache PVC settings, CSI temporary-volume
storage-class mappings, repository cache limits, and other existing ConfigMap data.

Lowering the task limit does not stop admitted tasks. Pending runtime changes may
temporarily delay new admissions until existing operations finish. Transfer limits
are per node, not a global storage-bandwidth guarantee; multiple nodes can add up.
