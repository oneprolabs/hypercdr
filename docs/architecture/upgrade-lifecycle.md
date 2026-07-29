# Upgrade lifecycle and image identity

## Scope

This model applies to platform releases, Comm Agent releases, Velero releases,
cluster upgrade availability, upgrade execution, and post-rollout verification.

## Image identity fields

The system handles three different identifiers. They must never be compared
across layers.

| Identifier | Source | Purpose |
| --- | --- | --- |
| Image reference | Release record and Kubernetes workload | The complete registry/repository:tag target selected by an operator. |
| Registry digest | Registry manifest API | Candidate immutability validation during registration and publication. |
| Runtime image ID | Kubernetes container status | Runtime diagnostics and legacy compatibility only. It may identify an OCI config or platform manifest rather than the registry index. |

Versioned image tags are treated as immutable release identities. A published
release is identified operationally by component, version, and full image
reference. The registry digest proves that the candidate tag did not move
between registration and publication.

## State flow

1. `candidate`: resolve and store a canonical registry index or manifest digest.
2. `active`: re-resolve the same media class and require the stored registry
   digest to match before publication.
3. `available`: target version is newer, or the version is equal and the full
   target image reference differs from the running image reference.
4. `queued/dispatched/running`: persist the target version, image reference,
   registry digest, release ID, workload names, and rollout annotation in the
   task snapshot.
5. `waiting_for_reconnect` or `waiting_for_verification`: the workload update
   was accepted; wait for fresh runtime inventory.
6. `succeeded`: the running version and image reference match the task target,
   and component-specific readiness is satisfied. A matching runtime digest is
   accepted as a compatibility fallback for older agents that cannot report
   the full identity.
7. `failed`: dispatch, rollout, reconnect, or verification fails with an
   actionable error code and message. An upgrade must not remain active
   indefinitely.

## Component verification

| Component | Required identity | Required readiness |
| --- | --- | --- |
| Comm Agent | Agent version and Deployment container image reference | New WebSocket session and healthy heartbeat |
| Velero | Velero version and server image reference | Server ready and every desired node-agent ready |
| Platform | Running build version and configured API/frontend image references | API health check and upgrader job completion |

## UI rules

- Display `Update` only when the backend reports `upgradeAvailable=true`.
- Matching running and target versions plus matching image references must show
  `Current`, regardless of registry/runtime digest differences.
- Active tasks display their persisted progress and state.
- Failed and timed-out tasks display one actionable error; they do not silently
  remain at a fixed percentage.

## Compatibility

Older agents may omit a version or full image reference. For those agents only,
a matching reported digest can complete an upgrade. New agents must report the
version and full image reference. Runtime digest values remain visible for
diagnostics but are not used to derive update availability.
