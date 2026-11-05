---
title: Glossary
description: Terms that recur throughout the HyperCDR documentation and what they mean.
---

# Glossary

## Platform and deployment

| Term | Meaning |
| --- | --- |
| **Control plane** | The platform service that provides the UI and API, made up of Edge, the frontend, the API, PostgreSQL and more |
| **Edge** | The Nginx layer that terminates TLS on 12443 and forwards requests |
| **Blue / green** | Two parallel application environments; a release switches traffic between them |
| **Active color** | The environment currently receiving traffic (blue or green) |
| **Install directory** | Defaults to `/var/lib/hypercdr` and holds `.env`, TLS, Compose files and the release manifest |
| **Release manifest** | File that pins each component version, activated on installation and upgrade |
| **Upgrade runner** | systemd service on the host that reads upgrade tasks and invokes the blue-green script |
| **Registration executor** | Internal platform service that performs Install for me registration with a kubeconfig |

## Clusters and components

| Term | Meaning |
| --- | --- |
| **Cluster agent** | Component deployed in a business cluster that connects outbound to the platform and runs tasks |
| **Velero** | Backup and recovery component used by native Kubernetes and CCE |
| **OADP** | The Velero distribution for OpenShift; OpenShift clusters use this path |
| **Node Agent** | Velero's node-level component, responsible for file-system backup and recovery |
| **Kopia** | The repository and data movement implementation used by Velero |
| **Data Mover** | CSI snapshot data movement mechanism that uses a restore cache during recovery |
| **Restore cache** | Temporary PVC strategy used during recovery, see [Velero restore cache](/en/docs/operations/restore-cache) |
| **BSL** | BackupStorageLocation, the object storage location on the Velero side |

## Business objects

| Term | Meaning |
| --- | --- |
| **Organization / tenant** | Resource isolation boundary, called `tenantId` in the API and the database |
| **Source cluster / target cluster** | The protected cluster / the cluster that receives recovery |
| **Storage** | Object storage configuration, the destination for backup data |
| **Policy** | Schedule and retention rules of a protection policy |
| **Application DR** | Combined configuration of source namespace, target, storage and policy |
| **Task** | Execution record of one backup or recovery |
| **Restore point** | Recoverable point in time produced by a successful backup |
| **Drill** | Verifies in an isolated scope whether recovery works |
| **Recovery** | Restores a restore point to the target environment |

## Versions and licensing

| Term | Meaning |
| --- | --- |
| **Community edition / ce-** | Tags look like `ce-v0.0.13` (see `VERSION` in the repository root for the current version) |
| **Enterprise edition / ee-** | Tags look like `ee-v0.0.8` and increment independently |
| **community.lock** | Lock file that pins the Community source commit and component versions for the Enterprise edition |
| **In-place upgrade** | The platform switches its own version through blue-green |
| **Component upgrade** | Version upgrade of the agent, Velero or OADP inside a cluster, independent of platform upgrades |

## Commonly confused pairs

| Pair | Difference |
| --- | --- |
| Platform upgrade vs component upgrade | The former upgrades the control plane, the latter upgrades in-cluster components; each runs on its own |
| Traffic rollback vs database rollback | The former switches back to the previous application version, the latter requires restoring a backup; migrations are not reverted automatically |
| Task success vs recovery usability | The former means the execution flow finished, the latter requires drill validation |
