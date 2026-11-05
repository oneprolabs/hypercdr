---
title: Core concepts
description: How organizations, clusters, storage, policies, application DR, tasks, restore points and drills relate to each other.
---

# Core concepts

The platform organizes pages in the order of these concepts: the organization defines the resource boundary, clusters and storage belong to it, policies are configured on clusters, policies produce tasks and restore points, and restore points feed drills and takeover.

## Concepts at a glance

| Concept | Meaning | Where in the UI |
| --- | --- | --- |
| Organization | Resource isolation boundary. An account belongs to exactly one organization | Settings → Organization |
| Cluster | A registered Kubernetes / CCE / OpenShift cluster | Configuration → Clusters |
| Storage | Object storage endpoint, bucket and credentials | Configuration → Storage |
| Policy | Schedule and retention rules of a protection policy | Configuration → Policies |
| Application DR | Combined protection configuration of source namespace, target and storage | DR → Application DR |
| Task | Execution record of one backup or recovery | Backup & Recovery Tasks |
| Restore point | Recoverable point in time produced by a successful backup | Restore Points |
| Drill | Verifies in an isolated scope whether recovery works | Drill |
| Recovery | Capability and task record type for recovery; the available entry points are Drill and Takeover | Backup & Recovery Tasks |
| Takeover | Switches business to the target following the DR failover process, after validation and change approval | Takeover |
| Sync / backup | One data sync or backup run performed by the agent, which produces a restore point | Tasks |

## Relationship model

```text
Organization
├── Cluster (source / target, install path differs by cluster type)
├── Storage (object storage, can be referenced by multiple policies)
├── Policy (schedule + retention rules)
└── Application DR
    ├── Source namespace scope
    ├── Target cluster
    ├── Storage
    └── Policy
        └── Task → Restore point → Drill / Recovery
```

## Commonly confused points

**Source and target clusters are separate.** A protection flow needs both. Whether a cluster is suitable as a recovery target depends on the application and the recovery configuration, and cannot be assumed.

**Saving a configuration only writes it to the database.** A storage configuration is a reference; access permissions and the data path are verified by an actual backup and recovery.

**Recovery scope and application boundaries are decided separately.** An application may span several namespaces, and several applications may share one namespace. Choose the recovery scope by business need, and decide up front whether to keep applications separate or combined.

**Task success only means the execution flow finished.** Whether recovery works depends on drill results and application checks.

**Platform administrators have no cross-organization business access.** Their permissions cover platform management such as users and organizations. Business pages provide no organization switcher.

## Boundaries between restore points and recovery

- **A protection configuration** combines the source cluster, application or namespace, policy and storage repository into one executable relationship;
- **sync and backup** are run by the agent, which prepares storage, takes the backup or advances state, and the platform receives events and creates or updates the restore point;
- **a restore point** is identified by the source cluster, protection configuration, backup task and Velero backup name, and exposes status, retention information and a content listing;
- **Restore** is the capability and record type for recovery tasks. The verified entry points for acting on a restore point are **Drill** and **Takeover**; there is no separate Restore creation button.
- **Drill** verifies the recovery path against an isolated target; **Takeover** is the failover that requires validation and change approval.

Do not start recovery when a restore point is still being created, its source does not match, its protection configuration has been deleted or the repository is unreachable. After a task succeeds, still check the target workloads, PVCs, Services, application health and data. Deleting a restore point record in the platform does not immediately erase all data in object storage.

## Configuration boundaries and security responsibilities

Control plane metadata such as users, organizations, policies, tasks and restore points is stored in PostgreSQL, and secret fields are protected by `HCDR_SECRET_KEY`.

A complete platform backup must therefore keep **the database, `HCDR_SECRET_KEY`, TLS files, `.env`, the release manifest and the object storage access configuration** together. A database backup without the key may not decrypt storage credentials, and object storage alone without control plane metadata cannot restore a complete management plane.

Sign-in verification supports an image captcha and Cloudflare Turnstile: the image captcha is four digits or letters with case-insensitive letters; Turnstile needs a valid key, browser access to the Cloudflare widget and outbound server-side verification. The mode recorded in the install directory's `.env` is authoritative.

## Continue reading

- [Control plane architecture](/en/docs/product/architecture): components and request paths
- [Users, organizations and platform permissions](/en/docs/user-guide/users-and-organizations): permission boundary details
- [Operation manual](/en/docs/manual/): the specific operations behind each concept
