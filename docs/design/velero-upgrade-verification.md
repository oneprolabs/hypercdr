# Velero upgrade observations

Backup mode is a per-backup decision, not the global `EnableCSI` setting.
File-level backups set `snapshotVolumes=false`, enable filesystem backup, and
clear `snapshotMoveData`. Their finalization must not list CSI snapshots or
snapshot contents even when CSI plugins are enabled for other configurations.
CSI data-movement backups retain their snapshot prerequisite checks, stable
snapshot capture and owned-resource cleanup. Native snapshot-only backups and
legacy unspecified snapshot policies retain their existing persistence path.

An upgrade is not complete when the workload update is accepted. The platform
waits for the existing server/node-agent readiness and release verification
checks; do not bypass those checks or manually mark an upgrade successful.

Communication Agent runtime observations cache expensive cleanup and cache
details for five minutes. Pod image digests and workload readiness must be
refreshed on every runtime observation, even when workload templates have not
changed. Template changes precede Pod replacement, so a template-only cache key
can otherwise retain the old server digest after rollout completes. Exclude
terminating Pods from the active runtime digest calculation. Report changes in
digests or version immediately through the heartbeat channel.

Version reporting preserves the complete release identity, including HyperCDR
build suffixes. For the shared image repository layout, strip only the known
`velero-` tag prefix: `hypercdr:velero-v1.18.2-hcdr.8` reports
`v1.18.2-hcdr.8`, not `velero`. Never shorten the protocol version to make UI
labels compact; display formatting must remain separate from verification.

These fixes are in the communication Agent, so installed clusters need the
updated communication Agent binary/image. Reinstalling Velero or changing
business volumes is unnecessary. Fresh installation and managed Agent upgrades
both use that same binary; no separate runtime patch script is required.
