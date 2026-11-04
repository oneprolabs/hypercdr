---
title: First verified recovery
description: "From cluster registration to a drill: walk the full HyperCDR path of protection, restore points and verified recovery in a test namespace."
---

# First verified recovery

A working console is only an operational surface. What actually needs proving is the path from protection to restore point to recovery.

Run the whole flow in a test namespace before protecting production. It needs no extra components, but it does need one registrable cluster, one writable object storage bucket, and one real backup and recovery.

## 1. Register a cluster

Go to **Configuration → Clusters**, choose the registration method and the cluster type (Native Kubernetes, Huawei CCE or OpenShift), and prepare the kubeconfig the page asks for.

- **Install for me**: use it when the registration executor can reach the cluster API in the kubeconfig; the platform registers the cluster for you.
- **Run command**: use it when the host running the command has cluster network access and permissions; run the generated command locally.

Pick the type that matches the actual distribution, not the cluster name. When registration finishes, confirm the cluster shows **online** and that nodes and namespaces have been reported. Uploaded kubeconfigs must embed credentials, be at most 1 MiB, and must not depend on external exec plugins.

## 2. Prepare storage and policies

Under **Configuration → Storage**, configure the object storage endpoint, bucket and credentials, and confirm the cluster can reach that storage. Under **Configuration → Policies**, set the protection schedule, restore point quota and retention rules.

Both can also be created inside the protection wizard — the wizard and the management pages share one creation form, so return to the list and select the new record afterwards.

Saving a resource does not prove the path works. Storage permissions, network and credentials are only validated by one real backup.

## 3. Configure application protection

Go to **DR → Application DR**. The three tabs cover protectable source applications, protection configurations, and recovery status on the target side.

Select a source namespace and start the protection wizard; choose scope, target cluster, object storage and policy in order. Applications that need different recovery scopes get separate protection configurations — sharing a namespace does not make them one thing.

## 4. Run protection and inspect the restore point

Trigger the policy task or wait for it, follow progress, phases and events under **DR → Backup & Recovery Tasks**, and once it succeeds check the restore point under **DR → Restore Points**.

The criterion is a usable restore point, not a green label in a list:

- the restore point must not still be in progress;
- the source cluster and protection configuration must still match, since a deleted configuration leaves nothing recoverable;
- object storage must be reachable right now.

Do not start a recovery when the restore point is being created, when the source does not match, when the protection configuration has been deleted, or when the storage is unreachable.

## 5. Validate recovery with a drill

Select a restore point under **DR → Restore Points** and use **Drill** to validate recovery in an isolated target. A drill validates the recovery path and the restore point, and is not a production takeover.

Check workloads, PVCs, services and application data — not just task status — and keep the target namespace and network isolated from the production recovery plan. The operation name does not isolate the target for you; the isolation scope is your design.

When a real takeover is required, use **Takeover**. It follows the DR switchover process and needs validation and change approval first. The validated entry points from a restore point are Drill and Takeover; there is no separate Restore creation button.

::: tip Drill versus Takeover

A failed drill is only a failed exercise — fix it and run again. A takeover is a business cutover. Spend the uncertainty on drills before you consider a takeover.

:::

## Acceptance checklist

| Check | Passes when |
| --- | --- |
| Cluster registration | Agent online, nodes and namespaces reported |
| Object storage | Connection test passes and at least one real backup wrote data |
| Protection configuration | Source namespace, target, repository and policy all set |
| Restore point | Status usable, timestamp and retention match expectations |
| Drill | Workloads, PVCs and application data in the isolated target all pass |
| Task record | Phases, events and logs explain the outcome — not just the success label |

## Next steps

<div class="hcdr-grid hcdr-grid-3">
  <a class="hcdr-card" href="/en/docs/manual/clusters"><b>Clusters and resources</b><span>Registration details, agent status, component versions and logs.</span></a>
  <a class="hcdr-card" href="/en/docs/manual/application-dr"><b>Application DR</b><span>Protection scope, target mapping, hooks and recovery actions.</span></a>
  <a class="hcdr-card" href="/en/docs/manual/drill"><b>DR drills</b><span>Drill target planning, isolation scope and result checks.</span></a>
</div>
