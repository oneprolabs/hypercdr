# OpenShift OADP phase-one support

HyperCDR keeps one control-plane architecture and one backup/restore task
contract. Registration adds `openshift` as a cluster type. Native Kubernetes
and Huawei Cloud CCE continue to deploy `comm-agent` with community Velero;
OpenShift deploys the independently released `oadp-comm-agent` with OADP.

The editable environment collection, prerequisites, network, storage, and POC
acceptance checklist is `docs/openshift-migration-poc-checklist.zh.md`.

## Qualified scope

- OpenShift 4.14 and 4.15 in a validated source/target compatibility matrix.
- Both sides use the same qualified OADP channel (`stable-1.3` by default) so
  backup formats and Kopia repository behavior do not vary across a 4.14/4.15
  migration pair. A different channel requires a separately validated release.
- Linux AMD64 control-plane and worker nodes; multi-node clusters are expected.
- Full backups and the existing Drill workflow only.
- RWO PVCs protected with OADP node-agent and Kopia.
- HyperCDR-managed S3 or S3-compatible object storage is the backup transit
  repository only. Azure, GCP, and other provider-specific plugins are outside
  phase one and are rejected by `oadp-comm-agent`.
- The cluster can pull all qualified OADP and HyperCDR images from the
  HyperCDR Alibaba Cloud registry.
- Registration credentials have `cluster-admin` capability.
- Registration checks the entire cluster for an existing OADP Subscription,
  DPA, or OADP Operator CSV and rejects reuse in phase one. It does not remove
  an existing installation automatically.

## Compatibility boundary

The control plane keeps the existing registration, heartbeat, task, status,
storage mapping, object-storage credential, and Drill contracts. OADP-specific
OLM, DPA, SCC, node-agent, and Kopia behavior belongs inside the OpenShift
registration provider and `oadp-comm-agent`. Existing Kubernetes and CCE
installation paths must remain behaviorally unchanged.

## Required release assets

Each qualified release must publish immutable AMD64 images and digests for the
OADP operator/catalog or bundle, Velero, node-agent/Kopia, object-storage
plugin, and `oadp-comm-agent` in the configured Alibaba Cloud registry. The
OpenShift installer must never fall back to a public registry.

Before publishing, run `scripts/release/verify-oadp-catalog.sh` against the
mirrored catalog. It rejects a catalog without `redhat-oadp-operator` channel
`stable-1.3` or with an image reference outside the configured Alibaba Cloud
registry. Mirroring requires a valid Red Hat pull secret and Alibaba Cloud
registry credentials and is intentionally performed as a release operation,
not while a customer cluster is registering.

`scripts/release/sync-oadp-images.sh --registry <aliyun-host/namespace>`
performs that release-time mirror with `oc-mirror`. It selects only package
`redhat-oadp-operator`, qualified channel `stable-1.3`, mirrors every related
image, validates the result, and prints `HCDR_OADP_CATALOG_IMAGE` for the
release manifest. Registration consumes that immutable manifest value and
does not contact Red Hat registries.

## Lifecycle behavior

The existing unregister workflow is retained. For an OpenShift cluster the
control plane addresses the agent in `openshift-adp`, deletes that dedicated
namespace when requested, and does not invoke the community-Velero CRD cleanup
path because OADP/OLM owns those cluster-scoped resources. Native Kubernetes
and Huawei Cloud CCE continue to use `hypercdr-agent` and their existing
uninstall behavior.

## Provisional installation timeouts

Until timings are collected from real OpenShift 4.14 and 4.15 environments,
the OADP CSV wait limit is a provisional safety bound, not an expected install
duration or compatibility claim. It defaults to 600 seconds and can be set
with `HCDR_OADP_INSTALL_TIMEOUT_SECONDS`. The registration executor's
OpenShift-only overall limit defaults to 2100 seconds and can be set with
`HCDR_OPENSHIFT_REGISTRATION_TIMEOUT_SECONDS`; native Kubernetes and Huawei
Cloud CCE retain their existing 1200-second limit. Qualifying the release must
record CatalogSource, CSV, DPA, Velero, node-agent, and Agent readiness timings
on both supported OpenShift versions and use those measurements to calibrate
the production defaults.
