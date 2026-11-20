# Node resource observations

The cluster details Nodes table displays CPU usage in cores, memory working set,
and root filesystem usage, alongside their capacities and usage percentages.
CPU and memory capacity come from Kubernetes Node status. Root filesystem
capacity and usage come from the kubelet summary API, not `ephemeral-storage`,
PVC capacity, or Longhorn storage pools.

The communication agent reads `/api/v1/nodes/{name}/proxy/stats/summary` through
the Kubernetes API with its existing service account. No node-agent or
metrics-server is required. The existing minute-level node inventory includes
an optional `resourceUsage` string map on each node, propagated unchanged into
the persisted cluster node inventory. Each resource has its own source timestamp.
Older agents omit this field and remain compatible; their known CPU/memory
capacities still display, with unknown usage.

Statistics requests run with at most four concurrent requests, a three-second
per-node timeout, and an eight-second overall collection deadline. A failed or
forbidden request does not fail the cluster inventory and does not manufacture
zero usage. Missing measurements remain unknown. The API service account must
already have node proxy read permission; this feature does not grant additional
privileges automatically.

The open details panel reads the latest persisted inventory every minute while
visible. Refresh resources requests the existing summary inventory explicitly
and waits for its result before replacing the table data. Cache measurement has
its separate refresh control and retains its hourly collection schedule.
Observations older than three minutes, or from offline clusters, are marked
stale. Local storage is root-filesystem-only; separate data disks are not summed.

Validation: `go test ./internal/kube ./internal/inventory ./pkg/protocol` in
`agent/comm-agent`, the affected backend tests, and `npm run test:topology` and
`npm run lint` in `frontend`, followed by real-node and browser checks on149.
