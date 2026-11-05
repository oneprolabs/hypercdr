---
title: Cluster registration and management
description: Register Native Kubernetes, CCE and OpenShift clusters, check online status, and handle common registration problems.
---

# Cluster registration and management

Entry point: **Configuration → Clusters**.

## Registration prerequisites

| Check | Description |
| --- | --- |
| Cluster type | Native Kubernetes, Huawei CCE or OpenShift; determines which component set to install |
| kubeconfig | Permission to create resources such as namespaces, CRDs and workloads |
| Network egress | Cluster nodes can reach the platform entry point and the image registry |
| Images reachable | Nodes can pull the component images listed in the release manifest |

## Registration methods

The registration page offers two methods. Both install the same agent and differ only in **who runs the installation**:

| Method | Where it runs | Preconditions |
| --- | --- | --- |
| **Install for me** | Platform registration executor | The control plane can reach the cluster API in the kubeconfig over the network |
| **Run command** | A host that can reach the cluster | The platform cannot reach the cluster directly, or you want to review the install commands manually |

Prepare the files described in the first step on the page, then run the generated command.

::: danger Credential safety

Do not share access tokens or kubeconfigs in public issues, and do not commit them to a repository. Registration commands contain access credentials; redact them before sharing.

:::

## Registration success criteria

After the installation completes, return to the cluster list and confirm:

- The cluster status is **online**;
- The node inventory has been reported, and the count matches the actual node count;
- The namespace inventory has been reported;
- The agent version is readable and matches the component version in the release manifest.

A "registration complete" message does not confirm success; the reported inventories are the evidence that the agent is connected.

## Impact of cluster type

| Type | Runtime component | Notes |
| --- | --- | --- |
| Native Kubernetes | Community Velero | Depends on CSI Snapshot and storage plugin capabilities |
| Huawei CCE | Community Velero + CCE-specific provider | Do not infer that CCE Autopilot or every node architecture is supported; see [CCE specifics](/en/docs/reference/cce) |
| OpenShift | OADP | The install path and verification checklist are separate; see [OpenShift and OADP](/en/docs/reference/openshift) |

## Offline clusters

Troubleshoot in this order:

1. Check whether the agent Pod is running and whether the log shows repeated reconnects.
2. Check whether the network from the cluster to the platform WSS address works and whether the certificate is trusted.
3. Check whether the registration credential is valid and whether a matching cluster record exists on the platform.
4. Check whether the platform entry address changed. The connection address of an installed agent does **not** migrate automatically when the platform address changes.

See [Troubleshooting](/en/docs/operations/troubleshooting) for detailed steps.

## Component upgrades

In the cluster details, **Comm Agent** and **Velero/OADP** are upgraded separately. The two component upgrades and the platform upgrade are three independent operations.

Before upgrading, confirm that no task is running on this cluster, that the image registry is reachable, that the target version is compatible with the current platform, and that a rollback path is ready. After the upgrade, verify the component versions reported by the agent and the task results; do not rely on the task status alone.

::: warning A platform upgrade does not cover component upgrades

A blue-green platform upgrade only switches the control plane's own images and traffic; the agent and Velero/OADP in the cluster still need a separate upgrade task and acceptance.

:::

## Taking a cluster offline

A normal decommission starts with an unregister request from the control plane. The platform first handles its own lifecycle and the object-storage lifecycle, then asks the online agent to clean up its Kubernetes resources.

**Only when neither the agent nor the platform can reconnect** should you use the offline uninstall script; see [Uninstall a cluster agent](/en/docs/manual/agent-uninstall).

## Related

- [Object storage](/en/docs/manual/storage)
- [Velero restore cache](/en/docs/operations/restore-cache): the restore cache policy configured automatically at registration
