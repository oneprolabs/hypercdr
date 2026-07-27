# Deployments (Legacy)

> Historical reference only. Use `scripts/release/`, `scripts/harbor/`, the
> root Compose files, and `docs/deployment/` for current workflows.

Platform-side deployment configuration.

- `docker/`: Dockerfiles, Compose files, and local development deployment
  assets.
- `bootstrap/`: Bootstrap platform installation and delivery assets.
- `harbor/`: Harbor image registry helper scripts.
- `release/`: Standard release build, push, Docker Compose deploy, and verify
  scripts.
- `kubernetes/`: Kubernetes YAML assets.
- `helm/`: Platform Helm chart assets.

## Docker

- `docker/compose.yaml`: Local PostgreSQL dependency.
- `docker/platform-api.Dockerfile`: Builds the API image that includes the
  backend and frontend static assets.
- `docker/comm-agent.Dockerfile`: Builds the cluster-side `comm-agent` image.
- `docker/comm-agent.local.Dockerfile`: Local debugging image for `comm-agent`.

Standard release image builds should use:

```bash
./deployments/release/build-release.sh v20260714.1 --registry 192.168.8.149/hypercdr
./deployments/release/push-release.sh v20260714.1 --registry 192.168.8.149/hypercdr
```

Velero images are built from the Velero source tree:

```bash
/data/hypercdr/hypercdr-velero/velero-1.17.1/deployments/build-velero-image.sh --push
```

## Harbor

`harbor/` contains the recommended image registry helper scripts, including
project initialization, required image synchronization, base image
synchronization, and locally built image updates.

The complete deployment flow is documented in:

```text
docs/deployment-guide.zh.md
```

## Kubernetes And Helm

`kubernetes/` and `helm/` are reserved for platform-side deployment templates.
Environment-specific kubeconfigs, certificates, object storage secrets, and
database passwords must not be committed here.
