---
title: "First use: from cluster registration to recovery verification"
description: "Complete cluster registration, storage and policy preparation, application protection configuration, backup checks and recovery verification in sequence."
---

# First use: from cluster registration to recovery verification

First complete the [installation guide](/en/docs/deployment/install) and sign in. **Verify the full flow on a test namespace before you protect production.**

## 1. Cluster registration {#register-cluster}

In the top navigation, open **Configuration → Clusters** and choose the registration action and cluster type: Native Kubernetes, CCE or OpenShift. Prepare the kubeconfig that the page requires.

Two registration methods:

| Method | When it applies |
| --- | --- |
| **Install for me** | The platform registration executor can reach the cluster API |
| **Run command** | The platform cannot reach the cluster directly; run the generated command on a host that can |

Prepare the file described in the first step on the page, then run the command it generates. **Do not share the access token or kubeconfig in a public issue.**

When the install finishes, confirm that the cluster status is online and that the node and namespace inventory has been reported.

::: warning Source cluster and target cluster

The protection flow needs a source cluster **and** a target cluster. Whether a single cluster can serve as a recovery target depends on the specific application and recovery configuration.

:::

## 2. Storage and policy preparation {#prepare-storage}

Configure in **Configuration**:

- **Storage**: object storage address, bucket and credentials; confirm that the cluster side can reach the storage.
- **Policies**: protection schedule and options such as retention.

You can also create these resources in the new protection wizard. The wizard and the management page share the creation screens; after creation, the list reloads and selects the new record.

::: danger A saved configuration only writes a record

A saved storage configuration only writes a record to the database. Storage availability requires an actual backup and recovery, which validates access permissions and the data path.

:::

## 3. Application protection configuration {#configure-protection}

Go to **DR → Application DR**. The page shows three stages: protectable source applications, protection configurations and recovery status on the target side.

Select the source namespace and start the protection wizard, then choose scope, target, storage and policy in order.

With multiple namespaces, choose separate or combined scopes according to the business need. **Several applications have separate recovery scopes and cannot be treated as one namespace.**

## 4. Backup and recovery verification {#verify}

| Check | Where | What to check |
| --- | --- | --- |
| Task execution | Backup & Recovery Tasks | Execution status and details; on failure, the error and the logs |
| Restore point | Restore Points | Whether the expected restore point exists and whether its state is usable |
| Recovery drill | Drill | Verify recovery in an isolated scope |
| Application verification | Cluster side | Workloads, PVCs and application data, not only the task success status |

A restore point is usable when three conditions hold: it is not being created, the source cluster still matches the protection configuration, and object storage is currently reachable. If any condition fails, do not start a recovery.

In **DR → Restore Points**, select a restore point and verify it with **Drill**. A drill must check workloads, PVCs, Services and application data, and its target namespace and network must be isolated from the production recovery plan: **the operation name does not guarantee an isolated target**.

::: tip Drill versus Takeover

A failed drill affects the drill only; correct the problem and run it again. A takeover is a business switch as soon as it executes, so it must pass verification and change approval first. The only operations on a verified restore point are Drill and Takeover, with no separate Restore creation button.

:::

Before an actual Recovery, check the target and the resource mapping to avoid overwriting running business workloads.

The dashboard distinguishes **platform overview** from **selected cluster overview**; cluster selection and time range determine which metrics apply, and not every metric is a platform total. When a task fails, read the error and logs; do not use sample data to explain results.

## 5. User settings and cleanup {#settings}

The sign-in page always uses the dark theme, and the first sign-in defaults to the light theme. Theme preferences are stored on the server, and later sign-ins use the personal setting. Times shown to the user use the selected time zone, while server time and scheduling use UTC.

When testing is done, delete or disable the test protection configurations so they stop producing backup traffic and consuming storage.

## Completion criteria

- [ ] Cluster status is online and the resource inventory is complete
- [ ] Storage connectivity is confirmed by an actual backup
- [ ] At least one application protection configuration is running
- [ ] The task list has successful records and restore points are visible
- [ ] After the drill recovery, workloads and PVCs pass verification

Confirm all five items before you start protecting business clusters.
