---
title: Cluster registration and management
description: Register Native Kubernetes, CCE and OpenShift clusters, check online status, upgrade cluster components, and unregister a cluster.
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

![Preparing a kubeconfig for the platform to install the agent](/images/user-guide/cluster-direct-install.png)

The screenshot shows the kubeconfig preparation step for **Install for me**. Both paths install the same agent; only who runs the command differs.

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

Choose the type that matches the actual distribution — do not guess from the cluster name. Native Kubernetes is selected by default.

![The three cluster types in the registration wizard](/images/user-guide/cluster-types.png)

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

In a cluster, **Comm Agent** and **Velero/OADP** are upgraded separately, and both differ from a platform upgrade: three independent operations. After updating the control plane, check every cluster for available updates — do not assume the agents were upgraded along with it.

![Component versions, update state and connection state on a cluster card](/images/user-guide/cluster-components.png)

**Up to date** means the page currently offers no new version for that component; **Ready** and **Last Seen** are for judging connectivity. The versions in the screenshot are examples, not a version your deployment should pin to.

::: warning A platform upgrade does not cover component upgrades

A blue-green platform upgrade only switches the control plane's own images and traffic; the agent and Velero/OADP in the cluster still need a separate upgrade task and acceptance.

:::

### Before upgrading

- The target cluster and change window are agreed, and the current version and rollback path are recorded;
- The cluster is online, nodes can pull the target images, and there are enough resources for a rolling update;
- No backup, drill, recovery, cleanup or upgrade task is running.

### Steps

1. Open **Configuration → Clusters** and find the target cluster.
2. Check for updates under the component's **Update** entry: **Upgrade Agent** for the communication component, **Upgrade Velero** for Velero. If there is no update entry, do not substitute an upgrade of the other component.
3. Verify the current version, target version and image digest. For Velero, also verify the server and Node Agent scope.
4. Submit once the maintenance conditions are met. **Opening the upgrade window creates no task** — cancel if you are not proceeding.
5. Follow the cluster task records and wait for connectivity to return and the components to become ready.

### Fields

| Item | Meaning |
| --- | --- |
| Current / Current Version | The current version. `Unknown` means the version could not be read, not that an upgrade is mandatory |
| Target / Target Version | The target version for this run; check compatibility with the platform release before submitting |
| Image digest | Confirms the actual image identity; the same tag does not mean the same content |
| Upgrade Scope | In the Velero upgrade window, the Server Deployment and Node Agent DaemonSet scope |
| Node Agent ready | Ready node agents versus the expected count; confirm it reaches the expected value after upgrading |

### Confirming the result

All four must hold: the upgrade task succeeded, the component reports the target version, the agent is back online, and Velero plus Node Agent are ready. Then validate protection and recovery inside the maintenance window — a created task is not an accepted upgrade.

### Common problems

- The agent goes briefly offline during a rolling update. If it stays offline, check the pod, image pulls and outbound connectivity instead of resubmitting the upgrade;
- Component paths on OpenShift differ from native Kubernetes; follow the components and supported scope the page actually offers;
- **A platform rollback does not roll back cluster components.**

## Taking a cluster offline (unregistering)

A normal unregister removes the cluster from platform management and cleans up the managed HyperCDR and Velero resources. **A force remove cannot prove that cluster-side components and backup data were cleaned up** — do not treat the two as interchangeable.

![Source configurations, restore points and data cleanup confirmation before unregistering](/images/user-guide/unregister-precheck.png)

Even though the page shows **Ready**, source protection configurations and restore points still exist and the data cleanup option is not ticked. `Ready` means the operation can run; it does not authorise deleting data.

### Before unregistering

- The cluster no longer carries current protection or recovery duties, and data cleanup is authorised;
- A normal unregister requires the agent to be online with no conflicting running tasks;
- The cluster's protection relationships as a source and as a target, its restore points and the backup retention requirements have been reviewed.

### Steps

1. In **Configuration → Clusters**, find the cluster and open **Unregister Cluster** from the action menu.
2. Check **Selected Cluster** and wait for the **Readiness check** to finish.
3. Read the blocking reasons and check the number of source protection configurations, target references, restore points and active tasks. Do not bypass unfinished tasks.
4. If the page asks for **Clean up related data**, read the consequences in the table below first. Tick it only with cleanup authorisation — never just to make the button available.
5. Once the conditions hold, select **Confirm Unregister** and follow the result in **Recent Tasks**.
6. Consider **Force remove** only when the agent or cluster is permanently unavailable and a normal unregister cannot complete. Type `Yes` as the page requires, then arrange cleanup of the remaining resources separately.

### Fields and options

| Field or option | Meaning and impact |
| --- | --- |
| Source configurations | How many protection configurations use this cluster as a source |
| Target references | How many other protection configurations reference this cluster as a target |
| Restore points / Active tasks | Associated restore points and active tasks, used to judge cleanup risk and blocking conditions |
| Clean up related data (source cluster) | Deletes source protection configurations, restore points and backup data — you may lose the ability to recover |
| Clean up related data (target cluster) | Keeps other sources' protection configurations and backup data, removing only this cluster's default-target reference |
| Historical storage data detected | Historical storage usage was detected; a normal unregister checks and cleans the cluster's storage prefix |
| Force remove | For permanently unavailable clusters; it is not equivalent to a completed cluster-side cleanup |

### Confirming the result

After a successful unregister the platform no longer treats the cluster as a managed cluster, and protection references are handled per the options you confirmed. On the cluster side, confirm that managed components are really gone while business applications remain; after a force remove, log the remaining resources and deal with them separately.

### Common problems

- **Unregistering does not delete business namespaces, workloads or PVCs**, but ticking data cleanup does delete backup data. Do not conflate the two;
- Unregistering does not delete the external object-storage service, but that does not mean the backup prefixes inside it are kept either;
- If unregistering fails, keep the task ID and the first error, fix permissions, the agent or storage, then retry the normal unregister.

**Only when neither the agent nor the platform can reconnect**, switch to the offline uninstall script: see [Uninstall a cluster agent](/en/docs/manual/agent-uninstall).

## Related

- [Object storage](/en/docs/manual/storage)
- [Velero restore cache](/en/docs/operations/restore-cache): the restore cache policy configured automatically at registration
- [Uninstall a cluster agent](/en/docs/manual/agent-uninstall): the fallback cleanup when the agent is unreachable
