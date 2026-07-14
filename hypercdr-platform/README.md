# HyperCDR Platform

This is the source root for the HyperCDR container disaster recovery control
plane. It is intentionally separated from the UI prototype projects
`hypercdr-hyperbdr-style` and `hypercdr-original-3000`.

## Repository Boundary

`hypercdr-platform/` is the official source repository root. Submit this
directory to GitLab as the platform repository.

This directory contains the source code, build scripts, packaging scripts,
deployment templates, and design documents required to run the platform. It
does not include local runtime data such as database files, object storage data,
kubeconfigs, private keys, logs, pid files, build outputs, or temporary backup
archives.

## Project Scope

- `platform/`: Control plane source code, including the frontend, backend API,
  WebSocket server, task engine, scheduler, and database migrations.
- `agent/`: Cluster-side agent source code and delivery assets, including
  `comm-agent`, installer scripts, Kubernetes manifests, and Helm chart assets.
- `third_party/velero/`: Velero dependency notes. The full Velero source has
  been split into the sibling repository `../hypercdr-velero/`; this repository
  only consumes the Velero image and bundled CRDs.
- `deployments/`: Platform-side deployment configuration for Docker,
  Kubernetes, Helm, Harbor, and bootstrap delivery.
- `docs/`: Architecture, API, database, agent, and communication protocol
  documentation.
- `scripts/`: Project-level build, packaging, and startup scripts.
- `tools/`: Development, validation, and generation utilities.
- `portal/`: Static bootstrap download portal used during development and test
  deployments. The final product download page can be moved to another system.

## Phase 1 Components

- Control plane: Web UI, REST API, WebSocket server, PostgreSQL, task engine,
  and scheduler.
- Agent: Official Velero plus the HyperCDR `comm-agent`.
- Communication: The agent connects outbound to the control plane WebSocket
  server for registration, heartbeat, inventory collection, task dispatch, and
  status reporting.
- Execution: The control plane creates tasks. The agent receives those tasks,
  creates or watches Velero CRDs through the Kubernetes API, and Velero performs
  the actual backup and restore work.

## Directory Layout

```text
hypercdr-platform/
  docs/
  platform/
    frontend/
    backend/
      api/
      websocket/
      task-engine/
      scheduler/
      migrations/
  agent/
    comm-agent/
    installer/
    charts/
  deployments/
  third_party/
    velero/
  portal/
    bootstrap/
  scripts/
  tools/
```

## Build

Build all components:

```bash
./scripts/build-all.sh
```

Build the backend only:

```bash
./scripts/build-backend.sh
```

Build the agent only:

```bash
./scripts/build-agent.sh
```

Build the frontend only:

```bash
./scripts/build-frontend.sh
```

Build outputs are written to `dist/` by default. That directory is local
generated output and should not be committed.

## Packaging

Create a release package:

```bash
HCDR_VERSION=0.1.0 ./scripts/package-release.sh
```

The release archive is written to:

```text
dist/hypercdr-platform-<version>.tar.gz
```

Create the development bootstrap download portal:

```bash
HCDR_VERSION=0.1.0-dev \
HCDR_IMAGE_REGISTRY=registry.example.com/hypercdr \
./scripts/package-bootstrap-portal.sh
```

Serve the bootstrap portal locally:

```bash
./scripts/serve-bootstrap-portal.sh
```

Default portal address:

```text
http://127.0.0.1:8080
```

The release package includes:

- `bin/platform-api`
- `bin/platform-migrate`
- `bin/comm-agent`
- `frontend/`
- `deployments/`
- `docs/`
- `scripts/`

## Images

Build the platform API image and agent image:

```bash
HCDR_IMAGE_REGISTRY=registry.example.com/hypercdr \
HCDR_IMAGE_TAG=dev \
./scripts/build-images.sh
```

Build and push images:

```bash
HCDR_IMAGE_REGISTRY=registry.example.com/hypercdr \
HCDR_IMAGE_TAG=dev \
HCDR_PUSH_IMAGE=true \
./scripts/build-images.sh
```

Export an offline image bundle for a customer environment:

```bash
HCDR_SOURCE_IMAGE_REGISTRY=company-registry.example.com/hypercdr \
HCDR_IMAGE_REGISTRY=customer-registry.example.com/hypercdr \
HCDR_VERSION=0.1.0-dev \
./scripts/export-bootstrap-images.sh --pull --execute
```

## Local Startup

Start PostgreSQL:

```bash
docker compose -f deployments/docker/compose.yaml up -d postgres
```

Build and start the platform API:

```bash
./scripts/build-backend.sh
./scripts/build-frontend.sh

HCDR_DATABASE_URL='postgres://hypercdr:hypercdr@127.0.0.1:15432/hypercdr?sslmode=disable' \
HCDR_PUBLIC_BASE_URL='http://127.0.0.1:18080' \
HCDR_AGENT_WS_ENDPOINT='ws://127.0.0.1:18080/ws/agent' \
./scripts/start-platform-api.sh
```

Start the frontend development server:

```bash
HCDR_API_PROXY='http://127.0.0.1:18080' \
./scripts/start-platform-frontend-dev.sh
```

## Key Environment Variables

- `HCDR_DATABASE_URL`: PostgreSQL connection string. Required by the platform
  API.
- `HCDR_HTTP_ADDR`: Platform API listen address. Defaults to `0.0.0.0:18080`.
- `HCDR_PUBLIC_BASE_URL`: External platform API address used by agent
  installers.
- `HCDR_AGENT_WS_ENDPOINT`: WebSocket or WSS endpoint used by agents.
- `HCDR_TLS_ENABLED`: Enables HTTPS/WSS.
- `HCDR_TLS_CERT_FILE`: TLS certificate path.
- `HCDR_TLS_KEY_FILE`: TLS private key path.
- `HCDR_IMAGE_REGISTRY`: Image registry prefix distributed to managed clusters.
- `HCDR_AGENT_IMAGE`: `comm-agent` image.
- `HCDR_VELERO_IMAGE`: Velero image.
- `HCDR_VELERO_AWS_PLUGIN_IMAGE`: Velero AWS plugin image.
- `HCDR_REGISTRY_CA_PATH`: Private registry CA certificate path.
