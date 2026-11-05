---
title: Install in detail
description: "HyperCDR install parameters, non-interactive installs, captcha modes, reading installer output, post-install checks, offline packages, upgrade, rollback, uninstall and install troubleshooting."
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

## Reading the installer output

Do not judge the result from the last line of output. Every block corresponds to a stage, and mixing them up is how an operator mistakes "cancelled" or "preview" for "installed".

| Output or screen | Meaning | What to do next |
| --- | --- | --- |
| Selected release version | The target version is fixed | Verify version, access URL, install directory and the confirmation prompt |
| Continue installation? | Waiting for a human decision | Type `y` once everything is correct; otherwise cancel |
| Installation cancelled | This install was cancelled | Fix the parameters and run again — no need to uninstall the old environment |
| Downloading / Extracting | Downloading or unpacking the package | Wait for it to finish and watch for errors; services are not ready yet |
| Dry run | The local installer only previews parameters | Review, then decide whether to actually execute |
| Sign-in page reachable | The web entry point responds | Continue by validating the challenge mode and a real sign-in |
| Current version | The runtime version the platform reports | Cross-check against service health, `/readyz` and business acceptance |

![The installer showing version, access URL and install directory while waiting for confirmation](/images/installation/installer-confirmation.png)

![The installer reporting a normal cancellation after declining to continue](/images/installation/installer-cancelled.png)

![Parameter preview output from the local installer — not a complete installation](/images/installation/local-package-preview.png)

::: warning Cancelled and preview are not success

`Installation cancelled` and a dry run are normal intermediate states, never proof of a finished install. The real criteria are healthy services, `/readyz` returning 200, and a browser that can sign in and change the password — see [post-install checks](#post-install-checks).

:::

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

## Post-install checks {#post-install-checks}

The installer exiting cleanly only means the deployment flow finished. Acceptance runs in three parts: the entry point and host services first, then one pass through the business configuration.

### Browser side

| Check | Expected result and limits |
| --- | --- |
| Address and certificate | The platform is reachable at the planned address, and the certificate name and chain match the deployment requirements |
| Image captcha / Turnstile | Matches the `--auth-challenge` choice made during the install, and the challenge can be completed |
| Sign-in | A valid account reaches the Dashboard and does not stall on an error or challenge state |
| Page version | The current version shown on the platform upgrade page matches the version installed |

![Checking the current version on the platform upgrade page after installing](/images/installation/installed-version.png)

**Current version** is the runtime version the platform reports. A screenshot taken on an existing environment only shows that the page reads this value; on its own it does not prove a new installation completed.

### Host side

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
sudo systemctl status hypercdr.service hypercdr-upgrade-runner.service --no-pager
curl -fsS -o /dev/null -w 'ready=%{http_code}\n' https://platform.example.com:12443/readyz
```

Edge, API, frontend, PostgreSQL, the registration executor and the upgrade executor should all be healthy, and `/readyz` should return `200`.

`curl -k` is only for checking connectivity against a self-signed certificate in an isolated environment — it does **not** validate trust. Remove `-k` once a trusted certificate is installed. `systemctl` applies only to hosts running systemd.

::: warning A passing `/readyz` does not mean recovery works

The readiness endpoint only proves the control plane can serve requests. Object storage, agent registration, real backups and real recovery tasks remain unverified.

:::

### Seven business steps

1. Open `https://<host>:12443` and confirm the sign-in page, the captcha and the authentication mode match the install.
2. Sign in as the initial administrator, **change the password immediately**, then check the platform address under **Settings → Access & Sign-in**.
3. Register a test cluster and confirm the agent is online, node and namespace inventories were reported, and the inventory refreshes.
4. Create object storage and run **Test Storage Connection**.
5. Create a minimal test policy, run one sync, confirm a restore point is produced, and check the task details.
6. Run a drill in an isolated namespace and validate workloads, PVCs and application data.
7. If email is needed, configure SMTP under **Settings → Email Service** and send a test message — otherwise invitations and password recovery cannot be delivered.

Only after all seven steps is the platform accepted. The first two only prove the control plane is alive.

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

- Browser, host and business checks after installing: [Post-install checks](#post-install-checks)
- Walk the full business flow from scratch: [First use](/en/docs/user-guide/first-use)
- Configuration items and captcha modes: [Configuration reference](/en/docs/deployment/configuration)
- Change the access address: [Change the platform access address](/en/docs/deployment/configuration#access-url)
- Problems after the install: [Troubleshooting](/en/docs/operations/troubleshooting)
