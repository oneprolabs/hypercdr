---
title: Control plane architecture
description: The components, request paths, cluster connectivity and upgrade chain of the HyperCDR control plane.
---

# Control plane architecture

The control plane consists of a Go API, a React frontend, PostgreSQL, an HTTPS edge, the registration executor and the host upgrade runner. The root `docker-compose.yml` defines the production topology; other assets such as `charts/` are not the online installation path.

## Components and responsibilities

| Component | Responsibility | Form |
| --- | --- | --- |
| Edge | Terminates TLS on 12443 and forwards page and API/WebSocket requests to the active color | Nginx container |
| Frontend | Console UI, one copy each for blue and green | React static assets |
| API | Business API, authentication, scheduling, database access | Go container |
| PostgreSQL | Persists business state | Shared database container |
| Registration executor | Runs Install for me registration and uses the kubeconfig to reach the cluster API | Separate internal service |
| Upgrade runner | Reads platform upgrade tasks and invokes the blue-green script | systemd service on the host |
| Cluster agent | In-cluster component that connects outbound to the platform and runs tasks | In-cluster workload |

The blue and green stacks each have their own network and share PostgreSQL; the API also joins an outbound network. The database, API, frontend and registration executor do not publish host ports directly.

## Request and data paths

```text
Browser        → HTTPS 12443 → Edge → active blue/green frontend + Go API → PostgreSQL
Cluster agent  → outbound WSS → platform tasks and resource inventory
Cluster agent  → Kubernetes API → Velero / OADP → object storage
```

The documentation site is a separately deployed service and does not share the control plane frontend.

## Cluster connectivity

Cluster agents normally connect to the platform over **outbound** WebSocket connections, collect resources and run persisted tasks. The platform does not need inbound access to business clusters, which simplifies deployment in segmented networks and behind firewall policies.

Registration has two paths:

- **Install for me**: the registration executor uses the kubeconfig to reach the cluster API and complete the installation, which requires network reachability from the control plane to the cluster API.
- **Run command**: run the generated command on a host that can reach the cluster, which suits environments where the control plane cannot reach the cluster API directly.

Both paths install the same agent; they differ only in who performs the installation.

### Registration differences across cluster types

The registration API explicitly supports three cluster types: `native-kubernetes`, `huaweicloud-cce` and `openshift`. They share the control plane task model, but registration checks, component images, timeouts and backend adaptation differ.

| Type | Registration method and constraints |
| --- | --- |
| Native Kubernetes | Reads the standard Kubernetes API. The kubeconfig must embed credentials, must not exceed 1 MiB, and cannot rely on an external exec plugin or credential file |
| Huawei Cloud CCE | Uses a separate kubeconfig check and registration session: create a temporary upload session and check the context, then create the registration task. The session is bound to the organization and the uploader, and must be uploaded again after a platform restart |
| OpenShift | Uses the OADP-compatible path and is constrained by the OADP version, project permissions, SecurityContextConstraints, CSI snapshots and object storage configuration; the wizard has explicit limits on the default resource scope |

CCE still protects workloads through the agent, the Kubernetes API, Velero and object storage, without calling the cloud provider backup API directly. OpenShift offers a smaller resource selection scope than native Kubernetes. Before going live, complete storage tests, sync and isolated recovery validation on the target distribution.

### Agents and the task model

Agents connect outbound to the control plane over WSS, report the Kubernetes version, nodes, namespaces, resources and component status, and receive tasks from the platform. Current task types are `inventory`, `storage-sync`, `backup`, `restore`, `drill`, `takeover`, `retention-cleanup`, `upgrade` and `unregister`.

The agent handles communication, orchestration and result reporting; Velero/OADP handles Kubernetes resources and persistent data; object storage holds backup data; PostgreSQL holds platform metadata and task state. The unregister script does not delete application namespaces, Longhorn, external object storage or Velero resources in other namespaces.

## Versions and the upgrade chain

The platform ships component versions through the GitHub Release manifest. Installation and a successful blue-green upgrade activate a local manifest, and cluster registration and component upgrades read the versions to install from that manifest.

A platform upgrade runs as follows: the upgrade page fetches release information and creates a task → the host upgrade runner reads the task queue → it generates the target manifest → it invokes the blue-green deployment script to switch traffic.

::: warning Platform upgrades and cluster component upgrades are separate

After the platform finishes upgrading, cluster-side components still need their own upgrade tasks, and you must check the versions that agents report. Database migrations are not reverted automatically when traffic rolls back either.

:::

## Persistence and rollback boundaries

PostgreSQL is the only persistence implementation; there is no in-memory fallback such as MemoryStore. User theme preferences, tasks, resources and version information all come from the database.

Recovering a complete backup therefore requires **the database and `HCDR_SECRET_KEY`**. Database files without the key cannot restore the platform.

Related reading: [Blue-green deployment and rollback](/en/docs/operations/blue-green), [Configuration reference](/en/docs/deployment/configuration), [Operations](/en/docs/operations/)
