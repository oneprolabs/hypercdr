---
title: Install in detail
description: "HyperCDR install parameters, non-interactive installs, captcha modes, offline packages, upgrade, rollback, uninstall and install troubleshooting."
---

# Install in detail

This page is the reference for installation: parameters, captcha modes, offline packages, upgrade, rollback, uninstall and troubleshooting. The online entry point is `deploy/online/install.sh`; `scripts/release/install-blue-green.sh` inside the release package performs the actual deployment.

::: tip Read Quick start first

For a first install, follow the three steps in [Quick start](/en/docs/); prerequisites, certificates and verification are in [Install HyperCDR](/en/docs/getting-started/install). This page assumes you have completed one install and need to look up a parameter or handle a specific case.

:::

## Install command

The minimal online install command is:

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

Without `--version`, the entry script takes the newest stable release from the `latest.json` asset on Releases without calling the GitHub API; with a version it downloads that asset directly. Pin the version for production and when reproducing an issue.

### Non-interactive install

Automated environments use `--yes` to skip the confirmation and must state the captcha mode explicitly:

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr \
    --auth-challenge image \
    --yes
```

### Install parameters

| Parameter | Required | Description |
| --- | :---: | --- |
| `--base-url URL` | Yes | External HTTPS origin of the control plane; default entry port `12443` |
| `--version VERSION` | No | Release version, such as `ce-v0.0.13`; omit it to use the latest stable release |
| `--install-dir PATH` | No | Install directory, default `/var/lib/hypercdr` |
| `--registry REGISTRY` | No | Overrides the default image registry |
| `--install-website` | No | Compatibility parameter for the legacy product portal; not recommended for new deployments; the documentation site is deployed independently |
| `--auth-challenge MODE` | No | `auto`, `image` or `turnstile` |
| `--yes` | No | Skips the install confirmation |

`--install-website` is a **flag with no argument** and does not accept `true`/`false`. Current source no longer deploys the legacy portal automatically, so new deployments should not rely on it; for historical installer packages, the behavior documented for that version applies. The control plane is always reached at `https://HOST:12443/login`, and the documentation site is deployed independently.

::: warning `--public-base-url` has no effect in the current release

The entry script parses this parameter but does not pass it to the downstream install command. Do not rely on it to change the generated registration address; use `--base-url`.

:::

## Captcha and sign-in

A release package may carry a Turnstile site key and secret key. When an interactive install selects `auto` and the credentials are complete, it asks whether the current host/IP is already on the Turnstile allowed-hosts list:

- `y`: use Cloudflare Turnstile;
- `n` or Enter: use the built-in image captcha;
- `--auth-challenge image`: force the image captcha;
- `--auth-challenge turnstile`: force Turnstile, and the install fails when complete credentials are missing.

An unreachable Cloudflare endpoint at runtime does not switch to the image captcha automatically; redeploy, or modify `.env` and rebuild the affected containers. The image captcha has four digits and letters, and letter matching is case-insensitive. The initial account is `admin` with password `admin123`, which must be changed after the first sign-in.

![HyperCDR sign-in page on the 149 development environment](/images/overview/login-149.png)

The screenshot comes from the 149 development environment. Production should use the real address and a trusted certificate.

## Offline installation

First download the matching release assets and checksum file from a networked, authenticated release environment:

```bash
VERSION=ce-v0.0.13
gh release download "$VERSION" -R oneprolabs/hypercdr \
  -p "hypercdr-installer-${VERSION}.tar.gz" \
  -p "hypercdr-installer-${VERSION}.sha256"
sha256sum -c "hypercdr-installer-${VERSION}.sha256"
```

Copy the package that passed the checksum to the target host, extract it, and run the installer inside it:

```bash
tar -xzf hypercdr-installer-${VERSION}.tar.gz
cd hypercdr-installer-${VERSION}
sudo ./install-blue-green.sh "$VERSION" \
  --base-url https://platform.example.com:12443 \
  --domain platform.example.com \
  --registry registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr \
  --install-dir /var/lib/hypercdr \
  --execute
```

An offline install still requires the target host to reach the configured image registry, unless the images were imported in advance and you switch to a reachable local registry. The package does not include Docker Engine, and it does not prepare a Kubernetes cluster or object storage automatically.

## Upgrade, rollback and uninstall

Before upgrading, back up PostgreSQL, `.env`, platform keys, TLS files and the release manifest. Platform upgrades use blue-green deployment; after a successful switch, check the active version, task and agent status.

```bash
# Roll back traffic and the release manifest (does not revert DB migrations)
sudo /var/lib/hypercdr/deploy-blue-green.sh --rollback

# Default uninstall, keeps data
sudo /var/lib/hypercdr/uninstall.sh --execute

# Remove install directory, database volume and images (irreversible)
sudo /var/lib/hypercdr/uninstall.sh --purge-data --remove-images --execute
```

::: warning Boundaries of rollback and uninstall

Rolling back platform images does not roll back database migrations, and it does not roll back cluster agents automatically; a platform upgrade is not an agent, Velero/OADP or Kubernetes component upgrade. Uninstalling the control plane does not delete cluster agents, application namespaces or backups in external object storage.

:::

## Installation artifacts and services

The installer prepares TLS files, random runtime secrets, Compose configuration, the release manifest and lifecycle scripts. The typical install directory is `/var/lib/hypercdr`; the actual files depend on the release package version:

| Path / object | Purpose |
| --- | --- |
| `.env` | Address, authentication, database, secrets and image settings; permissions must be protected |
| `docker-compose.yml` | Service composition of the current release package |
| PostgreSQL data volume | Control plane metadata and task state |
| `certs/` | TLS certificate material |
| `release-manifest.json` | Active version and component image identifiers |
| `restart-platform.sh`, `configure-public-url.sh` | Helper scripts for restart and access address |

The platform usually consists of Edge, blue/green API, blue/green frontend, PostgreSQL, the registration executor and the upgrade runner. The control plane uses `12443`; the documentation site is deployed independently. Historical installer packages may include the legacy portal on `12445`; before upgrading, check the release notes of the selected version and the port usage.

## Common issues and troubleshooting

| Symptom | Check first |
| --- | --- |
| Entry download stalls | DNS, IPv4/IPv6 and HTTPS egress to `raw.githubusercontent.com`; `--version` removes the version discovery step |
| GitHub API returns 403 | The new entry script should not depend on the GitHub API; confirm you are using the latest `install.sh` on `main` |
| Image pull fails | Registry address, credentials, DNS, proxy and firewall |
| Login captcha errors | `/api/v1/auth/config`, the mode in `.env`, and frontend/API version consistency |
| Turnstile fails | Site/secret key, allowed domains, browser and API outbound network |
| Browser certificate warning | Use a trusted certificate, or import the test certificate according to your organization security policy |
| Agent offline | Agent HTTPS/WSS egress to `--base-url`, certificate, time sync, token and network policy |
| Storage test fails | Endpoint, DNS/TLS, bucket, region, credential permissions and cluster network |
| Registration task stalls | kubeconfig context, RBAC, registration executor logs and clusterType |
| Recovery fails | Restore point state, target permissions, PVC/CSI, object storage and the first failure event of the task |

## Next steps

- Full acceptance after the install: [First use](/en/docs/user-guide/first-use)
- Configuration items and captcha modes: [Configuration reference](/en/docs/deployment/configuration)
- Change the access address: [Change the platform access address](/en/docs/deployment/configuration#access-url)
- Problems after the install: [Troubleshooting](/en/docs/operations/troubleshooting)
