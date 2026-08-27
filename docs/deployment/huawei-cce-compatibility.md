# Huawei Cloud CCE compatibility

HyperCDR supports Huawei Cloud CCE as an additional managed Kubernetes type.
The existing Native Kubernetes registration path remains backward compatible.

## Phase-one scope

- Standard CCE clusters maintained by Huawei Cloud
- Linux amd64 worker nodes; one worker is sufficient
- Static kubeconfig credentials usable by `kubectl`
- Community and Enterprise control planes
- Huawei OBS through its S3-compatible private endpoint
- Bidirectional backup, restore, and drill between CCE and Native Kubernetes
- CCE and the control-plane ECS in the same region and VPC

CCE Autopilot, Windows workers, ARM64 workers, and exec-plugin kubeconfigs are
outside the first compatibility phase.

## Reference deployment

The control plane and Bootstrap Portal run with Docker Compose on a Linux ECS.
The ECS has a public IP for operator access and a private IP for Agent traffic.
CCE workers do not need public IPs; outbound NAT may be used to pull the current
HyperCDR images from Alibaba Cloud ACR. OBS is accessed through its private
endpoint.

## Registration

Choose `Huawei Cloud CCE` in the Register Cluster drawer. Download the CCE
kubeconfig to the Linux operations host (recommended path
`~/.kube/hypercdr-cce.yaml`), copy the generated command, and run it. The
installer discovers candidate kubeconfigs, confirms the context, verifies CCE,
checks permissions, Linux/amd64 nodes, CSI storage, images, and connectivity,
then installs the shared HyperCDR Agent stack.

Automation may specify `--kubeconfig`, `--context`, and `--interactive false`.
The registration command can contain private and public WebSocket endpoints.
The Agent tries the private endpoint first and uses the public endpoint only as
a fallback. While connected through the public endpoint it periodically probes
the private WebSocket endpoint and reconnects through it after service returns.
A deployment without a public Agent endpoint is supported.

The installer downloads and validates the control-plane certificate before it
creates Agent workloads. It never silently falls back to insecure Agent TLS.
Image pulls and, for CCE, dynamic PVC provisioning run first in a temporary
preflight namespace. The namespace is removed before formal Agent resources are
created; failed first-time registration rolls back the temporary and Agent
namespaces.

## Storage

HyperCDR does not require Longhorn. Agent state, restore cache, and protected
application storage are evaluated independently by capability. The default
target StorageClass is used for drill unless the user selects another class in
advanced options.

New object data uses this tenant- and cluster-scoped prefix:

```text
hypercdr/v1/tenants/<tenant-id>/clusters/<cluster-id>/
```

Development data written under the legacy `hypercdr/clusters/` prefix is not
carried forward. Cleanup refuses empty, root, legacy, cross-tenant, or otherwise
unexpected prefixes.

## Acceptance gate

Formal CCE support requires successful real-environment tests for registration,
Agent/Velero/Node Agent readiness, OBS connectivity, backup, restore, drill in
both CCE-to-Native and Native-to-CCE directions, unregister cleanup, retry after
failed installation, and a complete Native Kubernetes regression run.
