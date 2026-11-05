---
title: Deployment planning
description: "Deployment shape, ports, capacity and certificate requirements for HyperCDR Community edition, plus how to judge compatibility before you deploy."
---

# Deployment planning

Community edition is a single-host deployment: one Linux host runs Docker Compose, and the control plane consists of Edge, blue/green API and frontend, PostgreSQL, the registration executor and the upgrade runner, with version switches following a blue-green pattern. Kubernetes clusters connect outbound through an agent, and backup data goes to the object storage you configure.

Installation steps are not on this page: [Quick start](/en/docs/) gives the three-step path, and [Install HyperCDR](/en/docs/getting-started/install) covers the full prerequisites and verification. This page lists what to prepare before you deploy.

## Deployment shape

```text
Browser ──HTTPS 12443──► Edge ──► active frontend
                         └────► active API ──► PostgreSQL
Cluster agent ──outbound WSS──► Edge
Agent / cluster components ──► object storage
```

- API and frontend each run as a blue and a green pair; an upgrade starts the inactive color and switches traffic once health checks pass.
- PostgreSQL is shared persistence and does not switch color.
- The registration executor is an internal service and publishes no host port.
- The documentation site is deployed separately, outside this composition.

## Ports and network

| Port | Purpose | Who connects |
| --- | --- | --- |
| `12443/tcp` | Console pages, API and WebSocket | Browsers, cluster agents |
| `12445/tcp` | Compatibility port for the legacy product portal | Only historical installers use it; the docs site does not |

The database, API, frontend, registration executor and upgrade runner publish no host ports directly.

| Direction | Purpose |
| --- | --- |
| Install host → GitHub Releases | Entry script, installer package and checksums |
| Install host → image registry | Pull API, frontend, registration executor and dependency images |
| Cluster agent → control plane `12443/tcp` | HTTPS/WSS outbound connection |
| Agent / cluster components → object storage | Write and read backup data |
| Registration executor → Kubernetes API | Register clusters with Install for me |

## What to decide first

| Item | How to judge |
| --- | --- |
| Access URL | An HTTPS origin that browsers and every managed cluster can resolve and reach; an IP works, a DNS name is recommended for production |
| Certificates | The installer's certificate is not browser-trusted; bring a trusted certificate for production and keep the name aligned with the URL |
| Capacity | Plan for PostgreSQL, logs and retained releases; backups land in object storage, not the install directory |
| Cluster types | Native Kubernetes, Huawei CCE and OpenShift differ in component images, registration checks and limits |
| Object storage | S3-compatible; bucket, credentials and cluster-side network all need to be tested |

::: tip Optional components

Nginx Proxy Manager and a local Harbor are not prerequisites. The Kubernetes assets under `charts/` are not the delivery path for the current online installer.

:::

## Compatibility check

| Topic | Reference |
| --- | --- |
| Distribution, storage driver and topology support | [Support and compatibility scope](/en/docs/reference/support-scope) |
| Differences on Huawei Cloud CCE | [Huawei CCE](/en/docs/reference/cce) |
| OpenShift and OADP | [OpenShift and OADP](/en/docs/reference/openshift) |
| Known limits and security guidance | [Limitations and security](/en/docs/reference/limitations) |

## In this section

<div class="hcdr-grid">
  <a class="hcdr-card" href="/en/docs/getting-started/install"><b>Install HyperCDR</b><span>Prerequisites, certificates, install command, result checks and first sign-in.</span></a>
  <a class="hcdr-card" href="/en/docs/deployment/install"><b>Install in detail</b><span>Every parameter, non-interactive installs, offline packages, upgrade, rollback, uninstall and troubleshooting.</span></a>
  <a class="hcdr-card" href="/en/docs/deployment/configuration"><b>Configuration reference</b><span>Environment variables, challenge modes, registration settings and access URL changes.</span></a>
</div>

## After installation

After the install, complete these steps in order:

1. [First use](/en/docs/user-guide/first-use): register a cluster, prepare storage and policies, configure protection, verify backups and recovery.
2. [Platform operations and upgrades](/en/docs/operations/platform): lifecycle scripts, the upgrade chain and backup essentials.
3. [Troubleshooting](/en/docs/operations/troubleshooting): entry points for common failures.

Upgrading an existing environment follows the blue-green flow, run by the upgrade runner on the host. Back up the database and runtime configuration first, because **database migrations do not roll back with the traffic switch**. See [Blue-green deployment and rollback](/en/docs/operations/blue-green).
