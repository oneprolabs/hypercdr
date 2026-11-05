---
title: Huawei CCE
description: Phase-one support scope, reference deployment, registration flow, storage conventions and acceptance requirements for Huawei Cloud CCE.
---

# Huawei Cloud CCE

HyperCDR supports Huawei Cloud CCE as an additional managed Kubernetes type. The existing Native Kubernetes registration path remains backward compatible.

The CCE implementation follows the installer Provider contract. kubeconfig, identity, dynamic storage, certificate and Worker connectivity checks run in the CCE provider, not in the Native Kubernetes provider.

## Phase-one scope {#scope}

- Standard CCE clusters maintained by Huawei Cloud
- Linux amd64 worker nodes; one worker is sufficient
- Static kubeconfig credentials usable by `kubectl`
- Community and Enterprise control planes
- Huawei OBS through its S3-compatible private endpoint
- Bidirectional backup, restore and drill between CCE and Native Kubernetes
- CCE and the control-plane ECS in the same region and VPC

::: warning Outside phase one

CCE Autopilot, Windows workers, ARM64 workers and exec-plugin kubeconfigs are outside the first compatibility phase.

:::

## Reference deployment {#deployment}

The control plane and Bootstrap Portal run with Docker Compose on a Linux ECS. The ECS has a public IP for operator access and a private IP for Agent traffic.

CCE workers do not need public IPs. Outbound NAT may be used to pull the current HyperCDR images from Alibaba Cloud ACR. OBS is accessed through its private endpoint.

## Registration {#registration}

Choose **Huawei Cloud CCE** in the Register Cluster drawer. Download the CCE kubeconfig to the Linux operations host (recommended path `~/.kube/hypercdr-cce.yaml`), copy the generated command and run it.

The installer discovers candidate kubeconfigs, confirms the context, verifies CCE, checks permissions, Linux/amd64 nodes, CSI storage, images and connectivity, then installs the shared HyperCDR Agent stack.

Automation may specify `--kubeconfig`, `--context` and `--interactive false`.

### Endpoint selection behavior

The registration command can contain private and public WebSocket endpoints. The Agent **tries the primary endpoint first** and uses the public endpoint only as a startup fallback.

::: warning An endpoint stays fixed once connected

After one endpoint connects, that address is fixed for the lifetime of the Agent process: reconnects use the same address and no runtime network probing or automatic endpoint switching occurs. Restart the Agent to run endpoint selection again.

Deployments without a public Agent endpoint are also supported.

:::

### Certificates and preflight

The installer downloads and validates the control-plane certificate before it creates Agent workloads. It **never** silently falls back to insecure Agent TLS.

Image pulls and, for CCE, dynamic PVC provisioning run first in a temporary preflight namespace. The namespace is removed before formal Agent resources are created; a failed first-time registration rolls back the temporary and Agent namespaces.

## Storage {#storage}

HyperCDR **does not require** Longhorn. Agent state, restore cache and protected application storage are assessed separately. Drill uses the target StorageClass by default unless the user selects another class in advanced options.

New object data uses this tenant- and cluster-scoped prefix:

```text
hypercdr/v1/tenants/<tenant-id>/clusters/<cluster-id>/
```

Development data written under the legacy `hypercdr/clusters/` prefix is not migrated to the new prefix. Cleanup rejects empty, root, legacy, cross-tenant and other unexpected prefixes.

## Acceptance requirements {#acceptance}

Formal CCE support requires all of these real-environment tests to pass:

- Registration
- Agent, Velero and Node Agent readiness
- OBS connectivity
- Backup
- Restore
- Drill in both CCE-to-Native and Native-to-CCE directions
- Cleanup after unregister
- Retry after a failed installation
- A complete Native Kubernetes regression run

## Related

- [Cluster registration and management](/en/docs/manual/clusters)
- [Support and compatibility scope](/en/docs/reference/support-scope)
- [Velero restore cache](/en/docs/operations/restore-cache)
