---
title: OpenShift POC validation checklist
description: Item-by-item checklist for OpenShift application migration validation, covering the source cluster, target cluster, storage, network and acceptance records.
---

# HyperCDR OpenShift application migration POC checklist

**Applies to**: OpenShift 4.14/4.15, Linux AMD64, multi-node clusters; HyperCDR, the standalone `oadp-comm-agent`, OADP 1.3, Node Agent and Kopia. Phase one validates full backups, RWO PVC restores and complete Drill only.

::: tip Port notes

This checklist follows the port conventions of the POC environment. The actual deployment uses the entry port configured at install time; the control plane defaults to **12443**, and a source development environment may use 3002.

:::

## 1. Source cluster information {#source}

- [ ] OpenShift is version 4.14 or 4.15, and the full patch version is recorded.
- [ ] All Master and Worker nodes run Linux AMD64 with RHCOS or RHEL.
- [ ] A kubeconfig that reaches the cluster API is available, and the account has `cluster-admin`.
- [ ] Record the API address, cluster name, node count and node Ready status.
- [ ] Confirm that OADP is not installed; phase one of HyperCDR does not reuse an existing OADP.
- [ ] Record the namespaces to migrate, with their PVCs, capacity, AccessMode and StorageClass.
- [ ] Confirm that all PVCs to migrate are `ReadWriteOnce`; RWX and ROX are out of scope for this POC.
- [ ] Record the images the application references and their pull Secrets, and confirm that the target cluster can obtain those images.
- [ ] Record cluster-level dependencies such as CRDs, ClusterRoles, SCCs, Operators and external services. They determine whether manual installation or configuration is still needed after a full restore, and they are not a phase-one resource filter.

## 2. Target cluster requirements {#target}

- [ ] OpenShift is version 4.14 or 4.15 from the compatibility matrix.
- [ ] All Master and Worker nodes run Linux AMD64 with RHCOS or RHEL.
- [ ] A kubeconfig that reaches the cluster API is available, and the account has `cluster-admin`.
- [ ] OADP is not installed; HyperCDR installs it during registration.
- [ ] The target namespaces can be created, or there is no name or resource conflict with existing namespaces.
- [ ] The target cluster has the CRDs, Operators, SCCs and external dependencies needed to restore the workloads.
- [ ] The target cluster can pull the application images and the HyperCDR/OADP images from Alibaba Cloud.

## 3. Object storage requirements {#storage}

- [ ] Phase one uses S3 or S3-compatible object storage as the backup transit repository only.
- [ ] Provide the Endpoint, Bucket, Region, Prefix, Access Key, Secret Key and TLS requirements.
- [ ] Both the source and the target cluster can resolve and reach the object storage Endpoint.
- [ ] The credentials have read, write, list and delete permission on the specified Bucket/Prefix.
- [ ] Bucket capacity covers the application resources and the full PVC data, with room reserved for retries and Drill.
- [ ] When both clusters share one repository, the Prefix plan does not conflict with other POCs or production tasks.
- [ ] When the object storage uses a private CA, the full CA certificate chain is available.

## 4. Network requirements {#network}

- [ ] Source and target clusters to the control plane: TCP 3002 (shared by HTTPS, API and WSS).
- [ ] With an external gateway, the clusters reach TCP 443, the gateway forwards to the control plane on 3002, and WebSocket Upgrade and long-lived connections are supported.
- [ ] Control plane to the source and target cluster APIs: usually TCP 6443. When control-plane egress is open by default, no separate egress rule is needed.
- [ ] Source and target clusters to the Alibaba Cloud registry: TCP 443.
- [ ] Source and target clusters to object storage: usually TCP 443; for HTTP, open the port actually in use.
- [ ] DNS resolves the control plane, the cluster APIs, the Alibaba Cloud registry and the object storage domains.
- [ ] Control-plane ingress opens TCP 3002 by default; open TCP 22 for SSH only when needed, and restrict the source network.

## 5. Target storage requirements {#target-storage}

- [ ] The target cluster has a CSI StorageClass that can create RWO PVCs dynamically.
- [ ] A one-to-one mapping from the source StorageClass to the target StorageClass is recorded.
- [ ] The target StorageClass supports the capacity, VolumeMode and file system the application requires.
- [ ] Target storage capacity covers all PVCs to restore, with space reserved for Drill copies.
- [ ] The CSI provisioner, node plugin and mount capability are healthy on every target Worker node.

## 6. Deployment requirements {#deployment}

- [ ] The HyperCDR control plane is available; the default entry is `https://<host>:3002`.
- [ ] The release manifest contains `oadp-comm-agent` and the validated OADP Catalog images and digests.
- [ ] The OADP Operator, Velero, Node Agent, Kopia and AWS/S3 plugin images are all mirrored to the designated Alibaba Cloud namespace, and the Catalog does not fall back to the public Red Hat registry.
- [ ] Pull credentials for the private Alibaba Cloud registry are configured and available to the Catalog, the Operator and the OADP workloads.
- [ ] If the Alibaba Cloud registry is private, the global Pull Secret or an equivalent image access policy is configured to OpenShift cluster security practice; namespace Secrets in `openshift-adp` and `openshift-marketplace` only supplement the install flow and do not replace cluster-level credentials.
- [ ] The control plane has a registration executor that can reach the source and target cluster APIs.
- [ ] Cluster and control-plane clocks are synchronized, certificates are valid, and clock skew is acceptable.

## 7. POC acceptance record {#acceptance}

- [ ] Both the source and target OpenShift clusters register successfully and keep their Agents online.
- [ ] The OADP Operator, DPA, Velero and the Node Agent on every target Worker are Ready.
- [ ] The BackupStorageLocations on both source and target are Available.
- [ ] A full backup of an application with an RWO PVC completes, and Kopia data reaches the object storage.
- [ ] A complete restore runs on the target cluster with StorageClass mapping.
- [ ] Pod, Service, configuration, Secret and PVC data checks pass, and the application health check passes.
- [ ] One complete Drill runs, and task status, events and restore point records match the existing workflow.
- [ ] The actual readiness timings of the Catalog, CSV, DPA, Velero, Node Agent and Agent are recorded to calibrate the current provisional install timeouts.

## 8. Recommended order {#order}

1. Collect source and target cluster information, and confirm that version, node OS and architecture, permissions, StorageClass and PVC AccessMode all meet this checklist.
2. Prepare the Alibaba Cloud registry, the OADP Catalog and the S3 object storage, then verify image pulls and object storage access from each cluster.
3. Register the target cluster, then the source cluster. Choose OpenShift on both sides and use the registration flow that the control plane generates.
4. Create the S3 or S3-compatible repository in the control plane storage settings, and confirm that the BSLs on both sides are Available.
5. Create a full protection task for the source application, and check the Backup CR, the Kopia BackupRepository, the object storage artifacts and the task events.
6. Select the target cluster and run a complete restore, then check the namespaces, workloads, Services, Secrets, PVCs and application data.
7. Run a complete Drill and save the task details, events, restore points and timings, then fill in the compatibility matrix and the issue record.

## Not in phase one {#out-of-scope}

- Backups filtered by resource type, label or object.
- Partial resource or partial artifact restore.
- RWX and ROX PVCs.
- Non-S3 object storage plugins such as Azure and GCP.
- Automatic reuse or removal of an existing OADP in the cluster.
- Extra orchestration for OpenShift-specific resources such as Operator and BuildConfig beyond what OADP can restore.
