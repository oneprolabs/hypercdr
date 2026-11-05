---
title: Uninstall a cluster agent
description: Use the version-matched offline uninstall script to clean up in-cluster resources when the agent or control plane cannot reconnect.
---

# Uninstall a cluster agent

Normal cluster unregister should be initiated **from the control plane**: it first handles the platform lifecycle and the object-storage lifecycle, then asks the online agent to remove its Kubernetes resources.

Use the offline uninstaller only when the agent or the control plane can no longer reconnect.

## Script location

Every agent installation stores the version-matched script in `configmap/hypercdr-agent-uninstaller` in the agent namespace. Extract and review it before execution:

```bash
kubectl -n hypercdr-agent get configmap hypercdr-agent-uninstaller \
  -o jsonpath='{.data.uninstall-agent\.sh}' > /tmp/uninstall-hypercdr-agent.sh
chmod 0700 /tmp/uninstall-hypercdr-agent.sh
/tmp/uninstall-hypercdr-agent.sh --namespace hypercdr-agent
```

The first run is a **dry run**: it only prints the plan and deletes nothing.

## Uninstall execution

Execute the reviewed plan with:

```bash
/tmp/uninstall-hypercdr-agent.sh \
  --namespace hypercdr-agent \
  --execute
```

## Cleanup of leftover Velero resources

If deletion finishes its normal wait but reports only stale Velero resources whose controller is no longer available, rerun with the controlled fallback:

```bash
/tmp/uninstall-hypercdr-agent.sh \
  --namespace hypercdr-agent \
  --execute \
  --force-finalizers
```

## Scope of the script

::: warning What the script handles

The tool is idempotent, and finalizer removal is **limited to** Velero objects inside the specified, recognizable HyperCDR Agent namespace.

It does **not** delete:

- application namespaces;
- Longhorn resources;
- backup objects in external object storage;
- Velero resources in other namespaces.

:::

## Handling a reachable control plane

Offline cleanup cannot notify a control plane that no longer exists. If the platform still exists, run **Force Remove** on the platform after the uninstall to remove only its stale cluster record.

## Script download from the control plane

When the control plane is reachable, you can also download the same script:

```bash
curl -k -fsSL https://CONTROL-PLANE/uninstall-agent.sh \
  -o /tmp/uninstall-hypercdr-agent.sh
```

After downloading, apply `chmod 0700` and start with a dry run.

## Related

- [Cluster registration and management](/en/docs/manual/clusters)
- [Troubleshooting](/en/docs/operations/troubleshooting)
