# HyperCDR

[English](README.md) | [中文](README.zh-CN.md)

HyperCDR is a container disaster-recovery platform for Kubernetes. A central
control plane manages protection policies, synchronization, restore points,
drills, recovery operations, component upgrades, and diagnostics. A
cluster-side `comm-agent` maintains an outbound connection to the platform and
coordinates Velero operations in each managed cluster.

## Architecture

```text
Browser
  -> HyperCDR frontend
  -> HyperCDR API + PostgreSQL
  -> secure WebSocket
  -> comm-agent
  -> Velero + node-agent
  -> object storage
```

## Repository layout

```text
backend/                  Go control-plane API, scheduler, migrations, upgrader
frontend/                 React + TypeScript control-plane UI
agent/comm-agent/         Go cluster communication and execution agent
bootstrap/                First-install portal and standalone installer
charts/                   Control-plane and managed-cluster Helm charts
docker/                   Container image definitions and runtime configuration
scripts/                  Development, build, Harbor, release, and verification
third_party/velero/        Pinned HyperCDR Velero source
docs/                     Architecture, protocols, deployment, and operations
```

Local runtime data, build output, logs, certificates, database files, and
kubeconfigs are not part of the repository. Development scripts use
`../hypercdr-runtime` by default.

## Development

Prerequisites: Go 1.24, Node.js 22, Docker Compose v2, PostgreSQL 16, and
OpenSSL.

```bash
cp scripts/dev/dev.conf.example ../hypercdr-runtime/dev/dev.conf
./scripts/dev/start-dev.sh
./scripts/dev/status-dev.sh
./scripts/dev/stop-dev.sh
```

Run component checks directly:

```bash
(cd backend && go test ./...)
(cd agent/comm-agent && go test ./...)
(cd frontend && npm ci && npm run build)
```

## Release

The release scripts build outside the source tree and publish a single platform
version across the API, frontend, upgrader, and comm-agent images.

```bash
cp scripts/release/release.conf.example scripts/release/release.conf
./scripts/release/release-all.sh vYYYYMMDD.N
```

See [the release guide](docs/deployment/release-flow.zh.md) and
[deployment guide](docs/deployment/deployment-guide.zh.md).

## Deployment modes

- Standalone host: `docker-compose.yml` and the Bootstrap installer.
- Kubernetes control plane: `charts/hypercdr-platform`.
- Managed cluster components: the generated registration installer exposed by
  the platform. `charts/hypercdr-agent` is reserved for a future supported chart.

The standalone host deployment stores persistent data under
`/var/lib/hypercdr` by default.

## Documentation

Start with [docs/README.md](docs/README.md). Protocol and state-machine changes
must update the corresponding document in `docs/protocols` in the same change.

## Security

Do not commit credentials, kubeconfigs, private keys, certificates, database
dumps, runtime logs, or customer data. See [SECURITY.md](SECURITY.md) for the
reporting policy.
