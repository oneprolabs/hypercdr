# DR Pair readiness

HyperCDR assesses whether an application restore point can be recovered from a
source cluster to a configured target cluster. Backup completion and recovery
readiness are separate states.

## Collection model

Cluster-wide capability evidence is collected on demand. It is not included in
the periodic inventory upload.

A capability scan runs when an operator selects a target in DR Configuration,
when the configuration is saved, when `Run assessment` is selected, and before
a recovery workflow requires fresh evidence. The platform stores the latest
scan with its collection timestamp for comparison and audit purposes.

The scan includes:

- served Kubernetes API resources and exact group/version/resource identities;
- custom resource types that actually exist in the protected namespace;
- StorageClasses and CSI drivers;
- IngressClasses, RuntimeClasses, and PriorityClasses;
- VolumeSnapshotClasses and cert-manager ClusterIssuers when their APIs exist.

Older agents remain compatible. Missing evidence is reported as `Unknown`; it
must never be treated as a successful check.

## Assessment status

- `Ready`: all evaluated requirements pass.
- `Ready with warnings`: no deterministic blocker exists, but warning or
  unknown findings require review.
- `Blocked`: at least one deterministic requirement is missing or incompatible.
- `Not assessed`: the DR pair or capability evidence is unavailable.

Every finding contains its check type, category, status, severity, confidence,
source evidence, target evidence, reason, recommendation, collection time, and
rule version. Recommendations are rule-based and must not claim an Operator or
version unless the collected evidence supports that claim.

## Recovery gate

Drill and Takeover are rejected by default when deterministic blockers exist.
An operator may explicitly select:

`I understand the identified risks and want to proceed anyway.`

No reason text is required. The task payload and task events record the user,
time, readiness snapshot, blocker count, and `forceProceed=true`.

## Runtime resources

Namespaced custom resources are application restore content by default,
including Kasten and Velero custom resources. HyperCDR never silently excludes
them based on their API group. A customer may explicitly exclude resource types
in protection or recovery configuration; the resulting exclusion is preserved
in the task and Velero Restore manifest for audit.

## Current rule coverage

The first rule version performs exact checks for:

- source custom resource API group/version/resource against target served APIs;
- Kasten and Velero runtime-object classification;
- StorageClass name and provisioner compatibility;
- target CSI driver registration;
- referenced IngressClass availability;
- cluster connectivity and evidence freshness.

Additional named capabilities are retained in the scan snapshot for subsequent
dependency rules. Restore-point content should take precedence over current
namespace inventory when artifact-level dependency metadata is available.

## Evidence contract

An empty list is data, not proof. Every capability scan therefore records
whether every required collector completed. `Forbidden`, timeout, discovery
failure, an older agent, or a partial response makes the evidence incomplete.
Incomplete evidence can produce only `Ready with warnings`; it can never prove
either compatibility or incompatibility.

| Evidence | Result |
| --- | --- |
| Complete scan and exact dependency match | Passed |
| Complete scan and confirmed missing/incompatible dependency | Blocked |
| Partial, forbidden, timed out, stale, or unsupported scan | Warning |
| Recent successful Drill against the same pair and artifact | Drill Verified |

Agent upgrades reconcile the read-only ClusterRole rules required for
capability discovery. A scan records the agent version so evidence collected by
an older collector is invalidated after upgrade.

## Validation loop

Readiness rules are calibrated against controlled Drill outcomes. A successful
Drill paired with a blocker is a false positive and fails release acceptance. A
failed Drill paired with Ready is a false negative; its deterministic failure
must become a new rule or be documented as runtime-only. Application health,
external service behavior, and data consistency remain Drill validation rather
than static preflight guarantees.
