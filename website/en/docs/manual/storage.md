---
title: Object storage
description: Configure S3-compatible object storage as the backup target, and verify that storage is usable.
---

# Object storage

Entry point: **Configuration → Storage**.

Backup data is written to the object storage you configure, not to the control plane host. The control plane database records only metadata and task status; the two lifecycles are independent.

## Information to prepare

| Item | Description |
| --- | --- |
| Endpoint | The access address of the S3-compatible service |
| Bucket | The bucket that stores backup data |
| Credentials | Access key and secret key; a dedicated account with least privilege is recommended |
| Region | Some services require it explicitly |
| Certificate | For a self-signed certificate, the cluster side must trust the corresponding CA |

## Configuration steps

1. On the **Storage** page, create a storage configuration and fill in the endpoint, bucket and credentials.
2. After saving, confirm the association with the correct cluster. Storage reachability is checked **from the cluster side**, not from the control plane host.
3. Select this storage in the protection wizard or in the application DR configuration.
4. Run a real backup and confirm that data is written to the target bucket.

::: tip You can also create it in the wizard

The protection wizard and the management page use the same creation UI. After a successful creation in the wizard, the page reloads and selects the new record.

:::

## Verifying storage usability

A successful save only means the configuration was written to the database. The following checks confirm usability:

- [ ] The backup task succeeded, and its duration matches the data volume
- [ ] The corresponding objects are visible in the target bucket
- [ ] A restore from that restore point has completed with the data intact
- [ ] Cluster-side network policies allow access to the storage endpoint

::: danger A successful save is not acceptance

Storage problems often surface only at the first backup, when the protection window may already have passed. Run a real backup immediately after the environment is deployed.

:::

## Common issues

| Symptom | What to check |
| --- | --- |
| A task reports a storage access failure | Whether the credential has expired, whether the bucket exists, whether the policy allows writes |
| Some clusters succeed, others fail | Network policies to the storage endpoint differ per cluster |
| TLS certificate error | Whether the cluster side trusts the self-signed CA |
| Slow backups | Bandwidth, cross-region access, storage throttling |
| A restore point exists but restore fails | Whether the storage credential is still valid at restore time |

## Relationship with retention rules

Storage capacity is finite. The retention rule in the policy determines how long restore points are kept; expired restore points are cleaned up. Plan capacity as data volume × retained copies × number of clusters, and leave headroom for growth.

## Related

- [Protection policies](/en/docs/manual/policies)
- [Tasks and restore points](/en/docs/manual/tasks)
- [Support and compatibility scope](/en/docs/reference/support-scope): the verification boundaries for object storage
