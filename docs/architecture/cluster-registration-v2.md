# Current cluster registration

The Clusters management page and protection wizard share registration UI. Native Kubernetes, Huawei CCE and OpenShift expose Install for me and Run command paths. Registration persists cluster/session state; the caller refreshes its resources after success.

Both paths need a kubeconfig with sufficient Kubernetes permissions. Install for me uploads it to the registration session and delegates installation to the internal registration executor; the executor must reach the API endpoint. Run command uses the platform-generated installer on a host that can reach the cluster API and platform. Follow the current UI kubeconfig instructions: files are placed in `/etc/hypercdr/kubeconfigs` and the command installer offers file selection. Do not invent a token or reuse another cluster's command.

The command contains the registration token, platform endpoint, cluster type, namespace and executor options. Treat it and kubeconfig as credentials. Native/CCE and OpenShift have separate component paths, selected from the active release manifest; a successful platform installation alone does not prove all component images are reachable from cluster nodes.

Completion requires a connected agent and reported inventory, not merely copying a command. Check the cluster card status and agent namespace pods. Use the cluster card's unregister action for standard removal. Removing pods alone does not perform platform unregister; deleting only platform records does not prove cluster resources were cleaned up. For disconnected clusters, consult [offline cleanup](../agent-offline-uninstall.md).

Implementation: `frontend/src/features/clusters/`, `backend/internal/httpserver/`, `scripts/agent/`, and the cluster registration executor image. See [first use](../user-guide/first-use.zh.md) and [troubleshooting](../operations/troubleshooting.zh.md).
