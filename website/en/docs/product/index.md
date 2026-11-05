---
title: Product overview
description: HyperCDR positioning, applicable scenarios, core capabilities, interface structure and product boundaries.
---

# Product overview

HyperCDR is a **container disaster recovery platform** for Kubernetes and OpenShift. It adds a management layer on top of Velero and OADP that turns cluster management, protection policies, restore points, drills and recovery tasks into repeatable processes.

The control plane does not transfer backup data. Data movement is performed by cluster agents running in business clusters: agents connect outbound to the platform and coordinate Velero (native Kubernetes, Huawei CCE) or OADP (OpenShift) to run tasks.

The flow is protect, verify, recover: administrators register clusters and storage, choose the applications to protect, produce restore points according to policy, verify recovery results with a drill, and use takeover when an actual cutover is required.

HyperCDR covers Kubernetes application protection. Physical machines, agentless VMware, cloud-provider-specific APIs and cross-platform migration are outside the scope of the current release.

## Common problems and responses

| Common pain point | How HyperCDR handles it |
| --- | --- |
| Every cluster runs its own backup tooling, and schedules are scattered | One control plane registers multiple clusters and defines protection policies centrally |
| Backup tasks succeed, but you cannot tell whether recovery works | Validate recovery in an isolated scope with a drill, checking workloads, PVCs and application data |
| Cross-cluster recovery relies on documents and manual notes | Define the mapping between source and target, and move from policy into recovery |
| Platform and cluster component versions drift apart | Pin component versions with a release manifest and confirm platform and cluster upgrades separately |
| Multiple teams share one platform and resource boundaries are unclear | Accounts belong to organizations, and business resources are isolated per organization |

## Applicable scenarios

- application-level backup and recovery for Kubernetes clusters;
- cross-cluster recovery with object storage as the backup medium;
- verifying restore point usability in an isolated target with a drill;
- tracking progress, status and logs of recovery, sync and DR tasks in one place;
- private deployments that need multi-organization isolation, member management and platform-level operations permissions.

| Goal | Recommended approach |
| --- | --- |
| Day-to-day protection | Create a protection configuration for each application, run sync or backup according to policy, and check the latest restore point |
| Recovery validation | Run a drill in a target namespace isolated from production, and check workloads, PVCs, Services and business data |
| Production takeover | Review the restore point, target cluster and resource mapping, run the takeover flow, and complete application-level acceptance |
| Multi-team collaboration | Isolate business resources with organizations, members and role permissions, and assign platform administration and DR operations by responsibility |

Actual support depends on the Kubernetes distribution, agent version, Velero/OADP path, CSI or storage driver and object storage compatibility. Confirm it against the [support and compatibility scope](/en/docs/reference/support-scope) and on-site validation before deployment.

## Core capabilities

- **Cluster onboarding**: Register native Kubernetes, Huawei CCE and OpenShift clusters, collect node and namespace inventories, and check agent status.
- **Storage and protection policies**: Configure the object storage endpoint, bucket, credentials and cluster-side parameters, then define schedules, resource scope and retention rules.
- **Sync and restore points**: Run sync or backup according to policy; the platform receives events and creates restore point records.
- **Application DR**: Organize the protection scope by source namespace, configure the target side and storage, and track recovery status on the target.
- **DR drills**: Verify the recovery path in an isolated target, checking workloads, PVCs, Services and application data.
- **Takeover**: Run the cutover following the DR failover process, after validation and change approval.
- **Identity and organization isolation**: The Community edition supports email sign-up, Google sign-in and organization invitations, and business resources are isolated per organization.
- **Upgrades and rollback**: The platform runs blue-green, switches traffic on upgrade and records the active version; rollback restores only application traffic and the release manifest.

## Interface structure

The Community edition control plane has five top-level sections: Dashboard, DR, Configuration, Operations and Settings. What you can see also depends on your account permissions; a hidden page does not mean you can bypass permissions by calling the API directly.

| Section | What it covers |
| --- | --- |
| Dashboard | Organization resources, protection health, sync and recovery activity, and current alerts |
| Configuration | Clusters, Storage, Policies, Tag Management |
| DR | Application DR, Restore Points, Backup & Recovery Tasks |
| Operations | Platform-side entry points for operations, upgrades and maintenance |
| Settings | Personal, organization and platform settings, plus platform upgrades |

All business resources are isolated by organization (tenant). Platform administrators can manage users and organizations, but cannot switch to another organization's clusters, storage, policies, applications, tasks or restore points through a URL, organization ID or request header.

## Relationship to Velero and OADP

HyperCDR retains the Velero source required for builds (`third_party/velero/`) and installs version-matched components when a cluster is registered:

- native Kubernetes and Huawei CCE use the community Velero path;
- OpenShift uses a dedicated OADP component and installation path.

The limits of Velero and OADP therefore define the support scope of HyperCDR. Restrictions from storage plugins, CSI snapshots and node architecture still apply, and the platform does not work around them. For registration differences across the three cluster types, see [control plane architecture](/en/docs/product/architecture#registration-differences-across-cluster-types).

## Product boundaries

- The platform does not install Docker; the host must already provide a container runtime;
- the platform does not configure DNS, an external reverse proxy, public certificates, object storage or cluster network policies for you;
- platform upgrades and cluster agent or component upgrades are separate; check versions and task results for each;
- the auto-generated TLS certificate enables HTTPS but is not a public certificate trusted by browsers;
- adaptation code, build assets or experimental paths in the source only show that a capability is implemented; certification is confirmed separately for each distribution, driver and topology.

## Community and Enterprise editions

Community edition tags use `ce-vMAJOR.MINOR.PATCH` and Enterprise edition tags use `ee-vMAJOR.MINOR.PATCH`; the two increment independently.

The Enterprise edition pins the Community source commit and released component versions through `community.lock`; changing the Enterprise version does not change that dependency automatically. The repository exposes public extension boundaries, and the Enterprise extension design is separate from the Community edition, which does not ship every Enterprise implementation.

## Next steps

- System composition: [Control plane architecture](/en/docs/product/architecture)
- Terminology: [Core concepts](/en/docs/product/concepts)
- Deployment: [Installation](/en/docs/deployment/install)
