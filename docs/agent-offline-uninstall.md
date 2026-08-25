# HyperCDR Agent offline uninstall

Normal cluster unregister should be initiated from the control plane. It first
handles the platform and object-storage lifecycle, then asks the online Agent to
remove its Kubernetes resources. Use the offline uninstaller only when the
Agent or control plane cannot reconnect.

Every Agent installation stores the version-matched script in
`configmap/hypercdr-agent-uninstaller` in the Agent namespace. Extract and
review it before execution:

```bash
kubectl -n hypercdr-agent get configmap hypercdr-agent-uninstaller \
  -o jsonpath='{.data.uninstall-agent\.sh}' > /tmp/uninstall-hypercdr-agent.sh
chmod 0700 /tmp/uninstall-hypercdr-agent.sh
/tmp/uninstall-hypercdr-agent.sh --namespace hypercdr-agent
```

The first run is a dry run. Execute the reviewed plan with:

```bash
/tmp/uninstall-hypercdr-agent.sh \
  --namespace hypercdr-agent \
  --execute
```

If deletion finishes its normal wait but reports only stale Velero resources
whose controller is no longer available, rerun with the controlled fallback:

```bash
/tmp/uninstall-hypercdr-agent.sh \
  --namespace hypercdr-agent \
  --execute \
  --force-finalizers
```

The tool is idempotent and limits finalizer removal to Velero objects inside the
specified, recognizable HyperCDR Agent namespace. It does not delete application
namespaces, Longhorn, backup objects in external object storage, or Velero
resources in other namespaces. Offline cleanup cannot notify a missing control
plane; if the platform still exists, use **Force Remove** afterward to remove
only its stale cluster record.

The same script can also be downloaded while the control plane is reachable:

```bash
curl -k -fsSL https://CONTROL-PLANE/uninstall-agent.sh \
  -o /tmp/uninstall-hypercdr-agent.sh
```
