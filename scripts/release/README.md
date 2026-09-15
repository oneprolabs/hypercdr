# HyperCDR Build and Release Guide

This directory contains the control-plane build, image publishing, installer packaging, installation, and operations scripts. Bootstrap only provides the Portal UI and distribution assets; it does not build platform images or the platform installer.

## Recommended command

```bash
cd /data/hypercdr-main/scripts/release
cp release.conf.example release.conf
# Edit release.conf as needed. It contains the complete registry and build configuration.
./release-all.sh 1.0.23.20260914 --config ./release.conf

The configuration file is optional only when all settings are supplied through
environment variables or command-line flags. `release.conf.example` is never
loaded automatically; copy it to `release.conf` (or pass another path with
`--config`). Registry passwords and Release Center tokens must be stored in
separate local files and must not be committed.
```

`release-all.sh` is the complete release entry point. It builds all control-plane and runtime images, pushes them, mirrors Velero/OADP assets, generates the complete `release-manifest.json`, creates the platform installer archive and SHA256 checksum, and registers the candidate release. Use `--skip-register` for the initial seed release.

## Script responsibilities

| File | Purpose |
|---|---|
| `release-all.sh` | Complete release pipeline |
| `build-release.sh` | Build platform binaries and images |
| `push-release.sh` | Push built platform images |
| `publish-runtime-images.sh` | Publish Velero/runtime images |
| `sync-velero-plugins.sh` | Mirror object-storage plugins |
| `mirror-community-oadp-images.sh` | Mirror OADP/OpenShift images |
| `build-community-oadp-bundle.sh` | Build OADP deployment resources |
| `build-community-oadp-catalog.sh` | Build the OADP Catalog image |
| `package-release.sh` | Package an installer from an existing manifest; does not build images |
| `upgrade-local.sh` | Upgrade an existing Docker installation from an extracted local package |
| `publish-package.sh` | Publish an existing platform installer to Bootstrap |
| `install-platform.sh` | Install the platform and configure systemd recovery |
| `deploy-platform.sh` | Render or deploy Compose configuration |
| `start-platform.sh`, `stop-platform.sh`, `restart-platform.sh` | Platform lifecycle operations |
| `uninstall.sh`, `uninstall-platform.sh` | Uninstall entry point and implementation |
| `verify-platform.sh`, `verify-oadp-catalog.sh` | Release/deployment validation |
| `common.sh` | Shared release functions |
| `templates/hypercdr.service` | systemd service template |

## Call graph

```text
release-all.sh
├── build-release.sh
├── push-release.sh
├── publish-runtime-images.sh
├── sync-velero-plugins.sh
├── mirror-community-oadp-images.sh
├── build-community-oadp-bundle.sh
├── build-community-oadp-catalog.sh
└── package-release.sh
    └── hypercdr-installer-<version>.tar.gz

publish-package.sh
└── verifies and distributes the installer already produced by release-all.sh
```

The legacy `bootstrap/scripts/package-release.sh` entry point is retained only for compatibility and must not replace `release-all.sh`.

## Output

```text
/data/hypercdr-runtime/build/platform/<version>/
/data/hypercdr-runtime/releases/community/<version>/
├── hypercdr-installer-<version>.tar.gz
├── hypercdr-installer-<version>.sha256
├── release-manifest.json
└── manifest.json
```

## Installation and validation

For detailed local-package prerequisites, installation, upgrade commands,
configuration-preservation limitations, backup examples, and verification, read
[Local Package Installation and Upgrade](LOCAL-INSTALLATION.md). The packaging
script includes this guide as `README.md` at the root of every newly generated
installer package. Previously generated archives are not modified.

```bash
./install-platform.sh docker --base-url https://HOST:3002 \
  --install-dir /var/lib/hypercdr --execute --confirm-prerequisites
systemctl status hypercdr.service
systemctl restart hypercdr.service
curl -k -o /dev/null -w 'ready=%{http_code}\n' https://HOST:3002/readyz
```

An installation is healthy only when `hypercdr.service` is enabled and active, all five Compose services are running, and `/readyz` returns HTTP 200.
