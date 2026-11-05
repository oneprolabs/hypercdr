---
title: OpenShift and OADP
description: OADP support scope, compatibility boundary, release asset requirements and lifecycle behavior for OpenShift clusters.
---

# OpenShift and OADP

HyperCDR keeps **one control-plane architecture** and **one backup and restore task contract**. Registration adds `openshift` as a cluster type.

Native Kubernetes and Huawei CCE continue to deploy `comm-agent` with community Velero; OpenShift deploys the independently released `oadp-comm-agent` with OADP.

For environment collection, prerequisites, network, storage and the POC acceptance checklist, see [OpenShift POC validation checklist](/en/docs/reference/openshift-checklist).

## Qualified scope {#scope}

- OpenShift 4.14 and 4.15 in a validated source/target compatibility matrix.
- Both sides use the **same** qualified OADP channel (`stable-1.3` by default), so backup formats and Kopia repository behavior stay consistent between 4.14 and 4.15. A different channel requires a separately validated release.
- Linux AMD64 control-plane and worker nodes; clusters are multi-node.
- **Full backups only**, with the existing Drill workflow.
- RWO PVCs protected by the OADP node-agent and Kopia.
- HyperCDR-managed S3 or S3-compatible object storage serves as the backup transit repository only. Azure, GCP and other provider-specific plugins are outside phase one and are rejected by `oadp-comm-agent`.
- The cluster can pull all qualified OADP and HyperCDR images from the HyperCDR Alibaba Cloud registry.
- Registration credentials have `cluster-admin` permission.
- Registration checks the entire cluster for an existing OADP Subscription, DPA or OADP Operator CSV and **rejects reuse** in phase one. It does not remove an existing installation automatically.

## Compatibility boundary {#boundary}

The control plane keeps the existing registration, heartbeat, task, status, storage mapping, object-storage credential and Drill contracts.

OADP-specific OLM, DPA, SCC, node-agent and Kopia behavior stays inside the OpenShift registration provider and `oadp-comm-agent`. Existing Kubernetes and CCE installation paths keep their current behavior.

## Required release assets {#release-assets}

Each qualified release must provide immutable AMD64 images and digests for the following components in the configured Alibaba Cloud registry:

- OADP operator / catalog or bundle
- Velero
- node-agent / Kopia
- Object-storage plugin
- `oadp-comm-agent`

::: danger No fallback to public registries

The OpenShift installer **never** falls back to a public registry.

:::

Before publishing, run the following against the mirrored catalog:

```bash
scripts/release/verify-oadp-catalog.sh
```

It rejects a catalog without `redhat-oadp-operator` channel `stable-1.3`, or one whose image references point outside the configured Alibaba Cloud registry.

Mirroring requires a valid Red Hat pull secret and Alibaba Cloud registry credentials. It **runs only as part of a release**, never while a customer cluster is registering:

```bash
scripts/release/sync-oadp-images.sh --registry <aliyun-host/namespace>
```

This command performs the release-time mirror with `oc-mirror`: it selects only the `redhat-oadp-operator` package and the qualified `stable-1.3` channel, mirrors every related image, validates the result and prints `HCDR_OADP_CATALOG_IMAGE` for the release manifest. Registration consumes this immutable value and does not contact Red Hat registries.

## Lifecycle behavior {#lifecycle}

The existing unregister workflow is retained. For an OpenShift cluster the control plane targets the Agent in `openshift-adp`, deletes that dedicated namespace when requested and **does not invoke** the community-Velero CRD cleanup path, because those cluster-scoped resources are owned by OADP/OLM.

Native Kubernetes and Huawei Cloud CCE continue to use `hypercdr-agent` and their existing uninstall behavior.

## Provisional installation timeouts {#timeouts}

Until timings are collected from real OpenShift 4.14 and 4.15 environments, the OADP CSV wait limit serves only as a **safety limit**. It is not an expected install duration and does not constitute a compatibility claim.

| Configuration | Default | Notes |
| --- | --- | --- |
| `HCDR_OADP_INSTALL_TIMEOUT_SECONDS` | 600 seconds | Upper bound of the OADP CSV wait |
| `HCDR_OPENSHIFT_REGISTRATION_TIMEOUT_SECONDS` | 2100 seconds | OpenShift-only overall limit of the registration executor |
| Native Kubernetes / CCE registration limit | 1200 seconds | Unchanged |

When the release is qualified, record CatalogSource, CSV, DPA, Velero, node-agent and Agent readiness timings on both supported OpenShift versions, and use those measurements to calibrate the production defaults.

## Related

- [OpenShift POC validation checklist](/en/docs/reference/openshift-checklist)
- [Cluster registration and management](/en/docs/manual/clusters)
- [Support and compatibility scope](/en/docs/reference/support-scope)
