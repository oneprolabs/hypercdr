# HyperCDR Bootstrap Deployment

This directory contains the development bootstrap assets for installing the
HyperCDR control plane during testing.

The bootstrap portal is a development distribution point. It is included in this
repository so the source tree is self-contained, but the final product download
page can be hosted by another platform later.

## Included Assets

- `install-platform.sh`: installer for Kubernetes and Docker Compose modes.
- `uninstall-platform.sh`: Docker Compose uninstaller for standalone host deployments.
- `prepare-docker-registry.sh`: optional Docker private-CA preparation script.
- `check-harbor.sh`: validates Harbor API reachability and Docker image pull readiness.
- `../docker-compose.yml`: canonical standalone host Docker Compose template;
  release packages expose it as `compose.yaml`.
- `values.example.yaml`: Helm values example.
- `charts/hypercdr-platform`: minimal Helm chart for the control plane.
- `portal/`: bootstrap download page installer.

The Bootstrap package does not contain a fixed registry CA. Publicly trusted
registries use the host system trust store. A private registry CA must be supplied
explicitly by the installer and must match the selected registry.

The image registry selected during control plane installation is written to
`HCDR_IMAGE_REGISTRY`. The platform uses that value as the default source for
its own image and for generated agent installation scripts, including
`comm-agent`, `velero`, and bundled support images.

The control plane creates a default administrator when the database is first
initialized:

```text
Username: admin
Password: admin123
```

Change this password after the first login.

For a publicly trusted registry, skip CA installation and verify access directly:

```bash
./check-harbor.sh --registry <registry-host>[:port]/hypercdr
```

For a private-CA registry, prepare Docker with the matching PEM certificate:

```bash
./prepare-docker-registry.sh \
  --registry <registry-host>[:port]/hypercdr \
  --registry-trust private-ca \
  --ca-file /path/to/registry-ca.crt
```

The script installs the supplied certificate into
`/etc/docker/certs.d/<registry-host>[:port]/ca.crt`. It does not restart Docker
by default, because restarting Docker can interrupt all containers on the host.

After preparing Docker, verify Harbor readiness:

```bash
./check-harbor.sh --registry <harbor-host>[:port]/hypercdr
```

Only continue to `install-platform.sh docker` after this check succeeds.

If `check-harbor.sh` fails with a Docker certificate error, restart Docker
during a maintenance window and run `check-harbor.sh` again:

```bash
systemctl restart docker
./check-harbor.sh --registry <harbor-host>[:port]/hypercdr
```

## Build Images

Build and push platform and agent images with the standard release script:

```bash
cd /data/hypercdr/scripts/release
./release-all.sh v20260714.5 --config ./release.conf
```

## Uninstall Standalone Host Deployment

For Docker Compose deployments, remove the control plane containers:

```bash
./uninstall-platform.sh --data-dir /var/lib/hypercdr --execute
```

For a full reinstall test, remove containers and local PostgreSQL data:

```bash
./uninstall-platform.sh --data-dir /var/lib/hypercdr --purge-data --execute
```

The uninstaller does not uninstall Harbor and does not stop the bootstrap portal.

## Recommended Development/Test Flow

### 1. Build and Push Release Images

```bash
cd /data/hypercdr/scripts/release
./release-all.sh v20260714.5 --config ./release.conf
```

### 2. Prepare Harbor

Harbor is managed outside this bootstrap directory. Create the `hypercdr`
project before pushing release images.

Create the project:

```bash
/data/hypercdr/scripts/harbor/init-project.sh \
  --harbor-url https://<harbor-host>:5001 \
  --project hypercdr \
  --username admin \
  --password Harbor12345 \
  --execute
```

### 3. Start Bootstrap Download Portal

Generate the downloadable installer package first:

```bash
cd /data/hypercdr/bootstrap
./release-bootstrap.sh v20260714.5
```

This creates an external portal source at `/data/hypercdr-runtime/services/bootstrap-portal/source`
and uses `/data/hypercdr-runtime/build/bootstrap` for temporary files. The Bootstrap
source directory is not modified. It does not start the portal or install the
control plane.

Start or refresh the bootstrap portal:

```bash
./portal/install-bootstrap-portal.sh \
  --source-dir /data/hypercdr-runtime/services/bootstrap-portal/source \
  --data-dir /data/hypercdr-runtime/services/bootstrap-portal/data \
  --port 8080 \
  --execute
```

### 4. Install the HyperCDR Control Plane

Open the bootstrap portal:

```text
http://<customer-bootstrap-host-ip>:8080
```

Use the generated command to install the HyperCDR control plane. The control
plane installer consumes the registry address but does not install or manage the
registry itself.
