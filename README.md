<div align="center">

# HyperCDR

[English](README.md) | [中文](README.zh-CN.md)

**Application-centric disaster recovery for Kubernetes**

</div>

**HyperCDR** is a centralized disaster-recovery platform for Kubernetes applications. It connects protected clusters to one control plane, discovers application namespaces and storage, schedules synchronization, creates restore points, and coordinates drills and recovery through Velero.

The platform is designed around observable, database-backed workflows. User actions create tasks first; the control plane then dispatches those tasks to a cluster-side `comm-agent`, receives progress and results, and updates the UI from persisted state. This keeps synchronization, cleanup, drill, recovery, upgrades, and diagnostics traceable from the platform to the cluster.

## Why HyperCDR

Kubernetes backup tools solve the mechanics of moving resources and volume data, but operating disaster recovery across clusters requires more than a backup command. Teams still need to answer which applications are protected, when the next synchronization runs, which restore points are usable, where data is stored, whether a drill succeeded, and which cluster components need an upgrade.

HyperCDR provides that operational layer while using Velero as the cluster-side data-protection engine.

- **Application-centric protection** — manage disaster recovery by cluster and namespace instead of individual backup objects
- **Three-stage workflow** — progress from discovery, through configuration, to protected and recoverable applications
- **Policy-driven synchronization** — run interval, daily, weekly, and monthly protection schedules
- **Restore points and retention** — track successful synchronization results and their cleanup lifecycle
- **Drill and recovery** — restore to a target cluster without turning platform state into optimistic UI state
- **Tenant isolation** — isolate clusters and disaster-recovery resources by tenant, with administrator and operator roles
- **Component lifecycle management** — track and upgrade `comm-agent` and Velero versions per cluster
- **Diagnostics** — collect platform and cluster logs with task/request correlation for troubleshooting
- **Pluggable object storage** — support S3-compatible, AWS, Azure, and Google Cloud storage through Velero plugins

## How It Works

```text
Browser
  │
  ▼
HyperCDR frontend
  │ REST / WebSocket status
  ▼
HyperCDR API ───────── PostgreSQL
  │                    tasks, policies, restore points,
  │ secure WebSocket   releases, users, tenants, logs
  ▼
comm-agent
  │ Kubernetes API
  ▼
Velero + node-agent ── Object storage
```

The cluster initiates the connection to the platform, so the control plane does not need inbound administrative access to every managed cluster. The `comm-agent` reports inventory and component versions, executes platform tasks, and coordinates Velero resources. PostgreSQL remains the source of truth for user-visible task and recovery state.

## Core Workflow

1. **Register a cluster** — the platform creates a single-use token and generates the cluster installation command.
2. **Discover applications** — `comm-agent` reports namespaces, workloads, PVCs, StorageClasses, and component versions.
3. **Configure protection** — select storage, policy, source application, and target cluster.
4. **Synchronize** — a manual action or due schedule creates a database task before it is dispatched to the agent.
5. **Create a restore point** — the platform records a restore point only after the agent reports a successful synchronization.
6. **Drill or recover** — restore a selected point to the target cluster and follow persisted task progress.
7. **Retain and clean up** — scheduled cleanup removes expired restore points and their object-storage artifacts.

## Architecture

```text
hypercdr/
├── backend/                   # Go control-plane API, scheduler, migrations, upgrader
│   ├── cmd/                   # platform-api, migration, and upgrader entry points
│   └── internal/              # HTTP, task, store, protocol, and diagnostic implementation
├── frontend/                  # React + TypeScript + Vite control-plane UI
├── agent/
│   └── comm-agent/            # Go cluster communication and execution agent
├── bootstrap/                 # Download portal and first-install/uninstall scripts
├── charts/                    # Control-plane and cluster Helm assets
├── config/                    # Credential-free Registry profiles
├── docker/                    # Runtime image definitions and Nginx configuration
├── scripts/                   # Development, Registry, build, release, and verification tools
├── third_party/
│   └── velero/                # Pinned HyperCDR Velero source tree
├── docs/                      # Architecture, protocols, deployment, testing, and operations
└── api/                       # OpenAPI and WebSocket contract documentation
```

Generated binaries, frontend assets, release packages, logs, certificates, databases, and other runtime state are kept outside the repository. By default, scripts use the sibling directory:

```text
../hypercdr-runtime/
├── build/                     # Versioned build workspaces
├── cache/                     # Reusable compiler and package caches
├── dev/                       # Development configuration, data, and logs
└── bootstrap-portal-source/   # Generated installer portal and release artifacts
```

## Quick Start

### Prerequisites

- Go 1.24
- Node.js 22 and npm
- Docker Engine with Docker Compose v2
- PostgreSQL 16 (provided by Docker for the standard workflows)
- OpenSSL and curl

### 1. Clone

```bash
git clone https://github.com/HyperBDR/hypercdr.git
cd hypercdr
```

The pinned Velero source is committed under `third_party/velero`; no Git submodule initialization is required.

### 2. Start the development environment

```bash
mkdir -p ../hypercdr-runtime/dev
cp scripts/dev/dev.conf.example ../hypercdr-runtime/dev/dev.conf
./scripts/dev/start-dev.sh
```

Check or stop the environment:

```bash
./scripts/dev/status-dev.sh
./scripts/dev/stop-dev.sh
```

Development runtime data remains under `../hypercdr-runtime/dev` when services are stopped.

### 3. Run checks

```bash
make test
make verify
```

Or run components independently:

```bash
cd backend && go test ./...
cd agent/comm-agent && go test ./...
cd frontend && npm ci && npm run build
bash bootstrap/tests/registry-ca-flow.sh
bash scripts/tests/registry-config.sh
```

## Common Commands

| Command | Purpose |
|---|---|
| `make dev` | Start API, frontend, and development dependencies |
| `make status` | Show development service status |
| `make stop` | Stop development services without deleting data |
| `make test` | Test backend, agent, frontend, and Bootstrap flows |
| `make verify` | Run tests plus shell and repository consistency checks |
| `./scripts/registry-login.sh` | Log Docker into the active Registry profile |
| `./scripts/release/release-all.sh <version>` | Build, publish, verify, and package a release |
| `./bootstrap/deploy-bootstrap.sh --execute` | Deploy or update the installer download portal |

## Registry Profiles

Container Registry endpoints are declared in [`config/registries.conf`](config/registries.conf). `HCDR_ACTIVE_REGISTRY` selects the default profile, and scripts can select another profile without changing source code.

```bash
./scripts/registry-login.sh

# One-command override for a configured profile
./scripts/release/release-all.sh vYYYYMMDD.N --registry-profile <profile>
```

Registry credentials are never stored in `registries.conf` or PostgreSQL. Docker/CI manages publication credentials; Kubernetes image pull secrets are used when a private runtime Registry requires authentication. Public OCI Registries use anonymous pull authorization automatically.

## Build and Release

HyperCDR uses one version, `vYYYYMMDD.N`, for the platform API, frontend, upgrader, and `comm-agent`. The customized Velero image keeps its pinned Velero-derived version.

```bash
./scripts/registry-login.sh
./scripts/release/release-all.sh vYYYYMMDD.N
```

The standard release pipeline:

1. Tests the backend, frontend, and agent.
2. Builds all HyperCDR components from the current source tree.
3. Builds the pinned customized Velero from `third_party/velero`.
4. Publishes PostgreSQL and the AWS, Azure, and GCP Velero plugins from configured trusted sources.
5. Pushes images to the active Registry and verifies every remote pull.
6. Generates a versioned Bootstrap installer and SHA-256 checksum.
7. Optionally registers the published release with a running control plane.

If a transfer fails after the four versioned core images are already published, resume without rebuilding immutable tags:

```bash
./scripts/release/release-all.sh vYYYYMMDD.N --skip-register --resume
```

See [Build, release, and installation flow](docs/deployment/build-release-install.md) and the [release runbook](docs/deployment/release-flow.zh.md).

## Production Installation

### Bootstrap portal

After a release is generated, deploy the small download portal:

```bash
./bootstrap/deploy-bootstrap.sh --execute
```

The portal reads the Registry profile embedded in the release. Users only provide the public control-plane address; they do not re-enter the Registry or install a CA when the selected Registry uses a publicly trusted certificate.

### Standalone host (recommended)

Download the generated package from the Bootstrap portal, then run:

```bash
./install-platform.sh docker \
  --public-base-url https://platform.example.com:3002 \
  --data-dir /var/lib/hypercdr \
  --image-tag vYYYYMMDD.N \
  --execute
```

The installer validates ports and images before mutation, creates TLS and persistent configuration, starts the control plane, and initializes the Registry, platform release, and active cluster-component release records in PostgreSQL.

Persistent standalone data is stored under `/var/lib/hypercdr` by default.

### Kubernetes control plane

Use the same package with the Kubernetes mode:

```bash
./install-platform.sh k8s \
  --public-base-url https://node.example.com:30080 \
  --storage-class <storage-class> \
  --node-port 30080 \
  --execute
```

Managed clusters are installed using the single-use registration command generated by the platform. The generated script installs `comm-agent`, Velero, node-agent, CRDs, and configured object-storage plugins from the platform's active Registry releases.

## Data and Time Conventions

- PostgreSQL timestamps are stored in UTC.
- The UI renders timestamps in the user's selected browser-side time zone.
- Schedules are normalized for execution and their next-fire timestamps are persisted in UTC.
- User-visible restore-point names are derived dynamically from task timestamps rather than storing localized time in the database.
- User-visible task state comes from persisted task and event records, not speculative frontend state.

## Tech Stack

- **Control plane backend**: Go · PostgreSQL · REST · WebSocket
- **Frontend**: React · TypeScript · Vite · Tailwind CSS
- **Cluster runtime**: Go `comm-agent` · Kubernetes · Velero · node-agent
- **Infrastructure**: Docker Compose · Helm · Nginx · OCI Registry
- **Storage integrations**: S3/AWS · Azure Blob Storage · Google Cloud Storage

## Design Principles

- **Database-backed truth** — tasks and displayed state are persisted before the UI presents them.
- **Outbound cluster connectivity** — managed clusters connect to the platform, reducing inbound access requirements.
- **Tenant-scoped resources** — clusters, storage, policies, DR configuration, tasks, restore points, and logs are tenant isolated.
- **UTC at rest, local at presentation** — timestamps remain unambiguous in storage and adaptable in the UI.
- **Source-based releases** — HyperCDR and customized Velero images are built from pinned source; runtime artifacts never modify the source tree.
- **Registry portability** — build, release, installation, registration, and upgrades use the same configurable OCI Registry profile.
- **Versioned migrations** — database changes are append-only migration history and are applied during installation or upgrade.
- **Traceable protocols** — platform/agent message and task-state changes update the corresponding documents under `docs/protocols`.

## Documentation

- [Documentation index](docs/README.md)
- [Architecture overview](docs/architecture/overview.md)
- [Deployment guide](docs/deployment/deployment-guide.zh.md)
- [Release flow](docs/deployment/release-flow.zh.md)
- [Platform-agent protocol](docs/protocols/platform-agent-messages.md)
- [DR task state machine](docs/protocols/dr-task-state-machine.md)
- [Logging and diagnostics](docs/operations/logging-design.md)
- [Contributing](CONTRIBUTING.md)

## Security

Do not commit credentials, kubeconfigs, private keys, certificates, database dumps, runtime logs, generated release artifacts, or customer data. See [SECURITY.md](SECURITY.md) for vulnerability reporting guidance.
