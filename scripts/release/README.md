# HyperCDR Build and Release Guide

This directory contains the control-plane build, image publishing, installer packaging, installation, and operations scripts. Bootstrap only provides the Portal UI and distribution assets; it does not build platform images or the platform installer.

The public online installation entry point is `deploy/online/install.sh`. It is
kept outside this release-tool directory so users can run it directly from the
GitHub Raw URL. It downloads a versioned GitHub Release installer asset and
then delegates to the packaged platform installer.

## Recommended command

```bash
cd /data/hypercdr-main/scripts/release
cp release.secrets.conf.example release.secrets.conf
chmod 600 release.secrets.conf
# Populate the Alibaba registry credentials and Turnstile Secret Key.
./release-all.sh --config ./release.conf
```

`release.conf` contains `RELEASE_VERSION`, non-secret registry/build settings,
the public Turnstile Site Key, and `HCDR_RELEASE_SECRETS_FILE`. A relative
secrets path is resolved from the directory containing the selected release
config, independent of the caller's current directory. The populated secrets
file is ignored by Git and must not be committed.

Before building, `release-all.sh` validates the configuration and authenticates
to the registry with `docker login`. Cloudflare validates a Turnstile Secret Key
only when the deployed login flow submits a browser token; the release step can
verify only that the matching Site/Secret settings are present and structurally
consistent. The private installer embeds the deployment Secret Key so a new
installation uses Turnstile immediately. Protect the archive as a credential-
bearing artifact and distribute it only through trusted channels.

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

### Uninstall an installed platform

Run the installed script by its full path, for example:

```bash
/srv/hypercdr/uninstall.sh --help
/srv/hypercdr/uninstall.sh
/srv/hypercdr/uninstall.sh --execute
/srv/hypercdr/uninstall.sh --purge-data --remove-images --execute
```

The script resolves its own directory and requires `docker-compose.yaml` and
`.env` beside it. No `--install-dir` or `--compose-file` options are accepted.
Without `--execute`, it only previews the plan. Data is preserved unless
`--purge-data` is specified. Run the installed copy, not the copy in the
extracted package or source tree. The installation command still accepts
`--install-dir` to select where these files are installed.

For the production blue/green Compose topology used by GitHub Actions, read
[Blue/Green Deployment](../../docs/deployment/blue-green-deployment.zh.md).
That topology uses GitHub Actions as the sole platform deployment controller
and is separate from the development Compose stack.

NPM owns public ports 80/443 and the browser-facing certificate. It forwards via
HTTPS to the server's internal address on port 12443; the edge maps host port
12443 to container port 443 and uses a local origin certificate. The edge then
forwards to the blue/green API and frontend over HTTP on private Docker networks.
Do not publish HyperCDR ports 80/443 or the API/frontend ports on the host.
Attach HyperCDR to NPM's Docker network by setting `HCDR_PROXY_NETWORK` (default
`nginx-proxy-manager_default`). Before upgrading, keep the existing NPM target
and confirm it reaches the server on HTTPS port 12443. The deployment script
refuses to recreate the edge until `HCDR_NPM_UPSTREAM_READY=true` is present in
the server `.env`.

For detailed local-package prerequisites, installation, upgrade commands,
configuration-preservation limitations, backup examples, and verification, read
[Local Package Installation and Upgrade](LOCAL-INSTALLATION.md). The packaging
script includes this guide as `README.md` at the root of every newly generated
installer package. Previously generated archives are not modified.

```bash
./install-platform.sh docker --base-url https://HOST \
  --install-dir /var/lib/hypercdr --execute --confirm-prerequisites
systemctl status hypercdr.service
systemctl restart hypercdr.service
curl -f -o /dev/null -w 'ready=%{http_code}\n' https://HOST/readyz
```

An installation is healthy only when `hypercdr.service` is enabled and active, all five Compose services are running, and `/readyz` returns HTTP 200.
