---
title: Support and compatibility scope
description: Separates code entry points from capabilities validated in an environment, so that implementation is not mistaken for a support commitment.
---

# Support and compatibility scope

This page separates **code entry points** from **environment validation**: an implementation in code does not mean every version combination has been validated.

| Item | Current entry point and limits |
| --- | --- |
| Control plane on a standalone host | GitHub Release online installer, Docker Compose blue-green API and frontend, shared PostgreSQL |
| Documentation site | Separately deployed static site; does not share containers with the control plane installation and release flow; the control plane uses port 12443 by default |
| Kubernetes control plane | Chart assets exist in the repository; the online installer and the current blue-green lifecycle do not use this path |
| Native Kubernetes | Platform registration page and generated install script, using the community Velero path |
| Huawei CCE | Separate provider checks, see [CCE notes](/en/docs/reference/cce); this path does not cover CCE Autopilot or every node architecture |
| OpenShift | Dedicated OADP components and install path, see [OpenShift notes](/en/docs/reference/openshift) and the item-by-item validation checklist |
| Object storage | The UI and backend support several storage configurations; actual availability depends on the cluster-side plugin, the network, the credentials and a real backup and recovery |
| Community/Enterprise extension | The repository exposes public extension boundaries; the Enterprise extension design is separate from the Community edition, which does not ship every Enterprise implementation |

::: danger Evidence that is not a support commitment

- development environment test results
- dedicated POC checklists
- historical image addresses

For production, record the actual platform version, release manifest, cluster version, node architecture, storage driver and the results of a full backup and recovery.

:::

## Related

- [Huawei CCE](/en/docs/reference/cce)
- [OpenShift and OADP](/en/docs/reference/openshift)
- [OpenShift POC validation checklist](/en/docs/reference/openshift-checklist)
- [Control plane architecture](/en/docs/product/architecture)
