# Volume backup timeline

Both modes discover volumes before data backup. CSI mode additionally shows
snapshot creation and temporary-volume preparation/mounting. Scanning and data
backup are one overlapping Kopia stage, not an invented separate scan phase.
Successful data backup adds an explicit completion entry.

CSI discovery closes against the validated capture inventory on the Backup,
not the PodVolumeBackup-only discovery annotation. Pending inventory volumes
remain visible before DataUpload creation, including identically named PVCs
in separate namespaces. Snapshot readiness comes from VolumeSnapshot
`readyToUse`; DataUpload Prepared/InProgress/Completed or a recorded data-path
start also proves that preparation was passed, even after temporary cleanup.
Failure to observe a snapshot remains unknown/waiting with its original read
error, never an invented ready state. Preparation changes participate in event
deduplication so that readiness transitions are not hidden.

The frontend shows ready/total counts and expandable per-namespace/PVC details.
Concurrent stages remain visible: transfer starts displaying as soon as any
volume actually starts, without waiting for every volume's total size. Unknown
totals use indeterminate progress. File-level tasks do not show CSI steps.
Older Agents cannot provide live CSI preparation evidence; actual transfer or
completion can establish that its prerequisites have passed retrospectively.

Tests cover CSI inventory discovery, missing inventory, preparation evidence,
multi-namespace volumes, transfer before complete discovery and mode-specific
display. No application freeze, replica mutation or new backup semantics are
introduced by these display changes.
