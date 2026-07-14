# Architecture

The control plane owns business orchestration, state management, and task
scheduling. The cluster-side agent connects to the control plane, collects
cluster inventory, receives tasks, and drives Velero through the Kubernetes API.

Core design principles:

- The control plane does not directly access managed Kubernetes API servers.
- Agents connect outbound to the control plane, which works better with NAT,
  private networks, multi-cloud environments, and multiple data centers.
- PostgreSQL is the only persistent database in Phase 1.
- Velero uses a pinned stable release. HyperCDR does not modify Velero source
  code in the platform repository.
- In the business model, an application maps to a Kubernetes namespace.
