---
title: Operation manual
description: "HyperCDR tasks organised by module: dashboard, clusters, storage, policies, application DR, tasks, drills and agent removal."
---

# Operation manual

This section is organised by module and is meant for readers who have already completed the installation and need to look up a specific operation. If this is your first time, work through [First use](/en/docs/user-guide/first-use) first.

## Recommended order

For a first setup, configure in this order:

1. Complete the installation and open the control plane ([Install in detail](/en/docs/deployment/install)).
2. Sign in, change the initial password, and confirm the organization and authentication mode.
3. Register the source and target clusters and confirm the agents are online.
4. Configure object storage and test the connection.
5. Create a protection policy and an application protection configuration.
6. Run one sync or backup and confirm that a usable restore point is produced.
7. Run a drill in an isolated environment and check workloads, PVCs and application data.
8. When a real takeover is required, use Takeover and complete acceptance in the task details.

> A success status in the task list only means the platform-side flow has finished. Production acceptance still requires checking Pods, PVCs, Services, application health and the actual data.

## Modules and dependencies

| Module | Purpose | Prerequisite |
| --- | --- | --- |
| [Dashboard](/en/docs/manual/dashboard) | Check the current state of the platform and clusters | At least one cluster online |
| [Clusters](/en/docs/manual/clusters) | Connect a cluster to the platform | A usable kubeconfig |
| [Object storage](/en/docs/manual/storage) | Configure where backup data is written | Reachable S3-compatible storage |
| [Protection policies](/en/docs/manual/policies) | Define backup schedules and retention rules | Storage configured |
| [Application DR](/en/docs/manual/application-dr) | Define protected applications and recovery targets | Clusters, storage, policy |
| [Tasks and restore points](/en/docs/manual/tasks) | Review backup results | A protection task already triggered |
| [DR drills](/en/docs/manual/drill) | Verify that a restore point is usable | A usable restore point |
| [Remove a cluster agent](/en/docs/manual/agent-uninstall) | Take a cluster off the platform | Agent namespace reachable |

Dependencies run in this order:

```text
clusters + storage + policies  →  application DR  →  tasks  →  restore points  →  drills / takeover
```

The first three are base resources, and a protection configuration cannot be created without all of them. Create them on their own management pages, or create them together in the protection wizard.

## Preconditions

Three preconditions apply whenever you configure or use the platform.

::: warning A saved configuration is only a saved configuration

Saving storage, a policy or a protection configuration only writes a record to the database. Whether it really works is confirmed by an actual backup and recovery.

:::

::: warning A successful task does not prove recovery works

Task status reflects whether the execution flow finished. Whether recovery works depends on what a drill shows for workloads, PVCs and application data.

:::

::: warning Platform administrators do not reach business resources across organizations

Business resources are isolated by organization. A platform administrator can manage users and organizations, but still sees only the data of their own organization on business pages.

:::

## Routine maintenance

- Each day, check whether cluster agents are online, whether object storage is reachable, and when the most recent restore point was created.
- Periodically run a drill in an isolated environment and record the acceptance result for applications and data.
- Before a platform upgrade, back up PostgreSQL, `.env`, `HCDR_SECRET_KEY`, TLS files and the release manifest.
- When a task fails, record the task ID, timestamp, cluster version and the first error event before investigating.
- Before sharing support bundles or logs, remove tokens, passwords, kubeconfig files, keys and internal addresses.
