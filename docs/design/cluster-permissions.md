# Managed cluster permission profile

New installations apply the complete `managed-cluster-v1` Agent permission
profile during registration, regardless of whether CSI protection or other
optional features are enabled. The profile is identified by the
`hypercdr.io/permission-profile` annotation on the Agent ClusterRole.

The installer preauthorizes known operations for inventory, CSI driver and
snapshot-class discovery, dedicated temporary StorageClass creation, owned
snapshot cleanup, Velero task management, runtime configuration, and managed
component upgrades. Velero and node-agent retain their own installation roles
for snapshot creation and data movement; those permissions are not conditional
on selecting CSI for the first DR configuration.

This is not permanent cluster-admin authorization. The communication Agent
does not receive wildcard mutation, `escalate`, `bind`, or `impersonate` rights.
Permissions do not install a CSI driver or guarantee storage snapshot support.
Snapshot cleanup must still enforce HyperCDR ownership at runtime.

Keep installation permissions, Agent upgrade preflight, administrator repair
policy, and permission contract tests aligned when introducing capabilities.
Known optional permissions belong in the first-install profile, not only in a
feature activation path. Do not grant speculative privileges for unknown future
features.

Existing installations are not silently elevated by a communication Agent
image update. Missing permissions require administrator-authorized repair.
Retain temporary administrator authorization as an upgrade fallback for legacy
installations and future operations outside the installed profile. Never retain
administrator kubeconfigs as permanent platform credentials.
