---
title: Application disaster recovery
description: Use a three-stage wizard to combine source applications, target clusters, storage and policies into a working DR configuration.
---

# Application disaster recovery

Entry point: **DR → Application DR**.

This page combines the base resources into one protection configuration: **which applications are protected, where backups are written, which cluster they recover to, and how often they run**.

## Three stages on the page

| Stage | What it shows |
| --- | --- |
| Source applications | The source namespaces that can currently be protected, and their status |
| Protection configuration | The protection relationships already established, and the storage and policy bound to them |
| Target-side status | The recovery state of the target cluster and the most recent recovery |

## Configuration flow

1. Select the source namespaces (multiple selections allowed).
2. Start the protection wizard.
3. Select **scope, target, storage, policy** in that order.
4. Confirm and save, then return to the list and check the protection status.

If storage or a policy does not exist yet, create it in the wizard. The wizard and the management page use the same creation UI; on success the page reloads with the new record selected.

## Protection modes for multiple namespaces

With multiple namespaces you have two options: **independent protection** or **combined protection**.

| Mode | Fits when | Cost |
| --- | --- | --- |
| Independent | Application boundaries are clear and each recovers on its own | More configurations, restore points are spread out |
| Combined | Multiple namespaces belong to the same business and must recover together | Coarser recovery granularity; a single failure affects the whole group |

::: danger Recovery scope and namespaces are not one-to-one

**Do not mistake the recovery scope of multiple applications for a single namespace.** One application may span several namespaces, and several applications may share one namespace. A wrong recovery scope usually surfaces only during a drill, by which time you may already be handling an incident.

:::

## Target cluster selection

The protection flow needs a source cluster **and** a target cluster. Whether the same cluster suits as a recovery target depends on the specific application and recovery configuration; technical availability alone is not enough.

The criteria include the target cluster's storage classes, node architecture and network policies, and whether the target namespace is already occupied.

## Post-save checks

Saving only means the configuration was written to the database. For it to take effect:

- [ ] A task appears when the policy schedule is due
- [ ] The task succeeds and a restore point appears under `Restore Points`
- [ ] The target-side status updates
- [ ] A [drill](/en/docs/manual/drill) confirms that recovery works

## Common configuration mistakes

| Symptom | Cause |
| --- | --- |
| The source namespace cannot be selected in the wizard | The cluster has not reported its inventory, or the cluster is offline |
| No task is produced after saving | The policy is not in effect, the schedule is not due, or protection is not enabled |
| The task fails with a storage error | Storage credentials or cluster-side network issues |
| Resource conflicts during recovery | The target namespace already has resources with the same name, or the resource mapping was not checked |

## Related

- [Protection policies](/en/docs/manual/policies)
- [Tasks and restore points](/en/docs/manual/tasks)
- [DR drills](/en/docs/manual/drill)
- [Core concepts](/en/docs/product/concepts): how the objects relate to each other
