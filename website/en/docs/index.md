---
title: Quick start
description: "Deploy HyperCDR Community edition on your own Linux host: run the online installer, verify the services, sign in, and move on to your first verified recovery."
---

# Quick start

HyperCDR Community edition runs on a Linux host you own: the online installer downloads a published release from GitHub Releases, the control plane runs on Docker Compose, and browsers reach it on port `12443`.

Current version: `ce-v0.0.13`. This page covers only the shortest path from nothing to a working console. For parameters, offline installation, upgrades and uninstall, see [Install HyperCDR](/en/docs/getting-started/install).

## Before you start

| Item | Requirement |
| --- | --- |
| Host | A long-running Linux host with root or `sudo` |
| Container runtime | Docker Engine and Docker Compose v2 (`docker compose`); the installer will **not** install them for you |
| Base tools | Bash, curl, tar, OpenSSL; `flock` for the deployment lock and `jq` for manifest checks |
| Access URL | An HTTPS origin reachable from browsers and from every managed cluster, e.g. `https://platform.example.com:12443` |
| Network | Installation host reaches GitHub Releases and the image registry; cluster agents reach the control plane on `12443/tcp` outbound |
| Cluster | At least one Kubernetes, CCE or OpenShift cluster to register, so you can validate the protection path |

::: tip What the installer does not do

Docker, DNS, firewalls, public certificates, external reverse proxies, object storage and cluster network policies are all yours to prepare. The TLS certificate generated during installation only makes HTTPS work; it is not a browser-trusted public certificate.

:::

## Three steps to a running console

### 1. Run the installer

Replace the example domain with the actual IP or hostname. It must be reachable from both browsers and cluster agents.

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

The installer prints the resolved version, base URL and install directory, then asks for confirmation before downloading anything. On an interactive install, type `y` at `Continue installation? [y/N]` to continue.

### 2. Check the services

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
curl -kfsS -o /dev/null -w 'ready=%{http_code}\n' https://platform.example.com:12443/readyz
```

Edge, API, frontend, PostgreSQL, the registration executor and the upgrade runner should all be healthy, and `/readyz` should return `200`. A passing `/healthz` only means the process started, and a passing `/readyz` still does not prove that object storage or a real recovery task works.

### 3. Sign in and change the password

Open `https://platform.example.com:12443` in a browser. The initial account is `admin` with password `admin123`. **Change the password immediately after the first sign-in.**

The login challenge is either an image captcha or Cloudflare Turnstile, depending on what you chose during installation. The mode recorded in the install directory's `.env` is authoritative.

## Next steps

<div class="hcdr-grid hcdr-grid-3">
  <a class="hcdr-card" href="/en/docs/getting-started/install"><b>Install in detail</b><span>Install parameters, pinned versions, non-interactive installs, offline packages and the artifact list.</span></a>
  <a class="hcdr-card" href="/en/docs/getting-started/first-recovery"><b>First verified recovery</b><span>Register a cluster, prepare storage and policies, configure protection, then finish one isolated drill.</span></a>
  <a class="hcdr-card" href="/en/docs/operations/troubleshooting"><b>When installation fails</b><span>Work through ports, certificates, image pulls, agent connectivity and failing tasks.</span></a>
</div>

Installed is not the same as usable. Installation only proves the control plane starts; cluster agents, object storage permissions and the recovery path still need their own validation, and installing the platform protects no workload by itself.

::: info Reading conventions

- Platform commands run in the installation directory, `/var/lib/hypercdr` by default.
- The documentation follows the code, scripts and `env.example` in the repository; replace every example value with your own.
- Capabilities present in the code are not validated for every version, driver and topology. Check the [support and compatibility scope](/en/docs/reference/support-scope) before using them in production.
- Never put passwords, kubeconfig files, private keys, certificates or database dumps into documents, repositories or public issues.

:::
