---
title: Quick start
description: "Install HyperCDR Community edition on your own Linux host: run the online installer, check the services, and sign in."
---

# Quick start

HyperCDR Community edition is deployed on a Linux host you own. The online installer downloads a published release from GitHub Releases, the control plane runs on Docker Compose, and browsers reach it on port `12443`.

Confirm these four points before you start:

- a Linux host that will stay running, with root or `sudo`;
- Docker Engine and Docker Compose v2 (`docker compose`) already installed; the installer does not install them;
- an HTTPS address reachable from browsers and from every managed cluster;
- outbound access from the install host to GitHub Releases and the image registry.

Disk capacity, base tools, certificate requirements and all install parameters are covered in [Install HyperCDR](/en/docs/getting-started/install).

## 1. Run the installer

Replace the example domain with the actual IP or hostname. It must be reachable from both browsers and cluster agents.

```bash
curl -fL --progress-bar \
  https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr
```

The installer prints the resolved version, access address and install directory, then asks for confirmation before downloading. At `Continue installation? [y/N]`, type `y` and press Enter to continue.

## 2. Check the services

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
curl -kfsS -o /dev/null -w 'ready=%{http_code}\n' https://platform.example.com:12443/readyz
```

Edge, API, frontend, PostgreSQL, the registration executor and the upgrade runner should all be healthy, and `/readyz` should return `200`. A passing `/healthz` only means the processes started; a passing `/readyz` still does not prove that object storage or a real recovery task has been validated.

## 3. Sign in and change the password

Open `https://platform.example.com:12443` in a browser. The initial account is `admin` and the initial password is `admin123`. **Change the password immediately after the first sign-in.**

Login verification is either an image captcha or Cloudflare Turnstile, depending on what you selected during installation. The mode in effect is the one recorded in `.env` inside the install directory.

## Next steps

<div class="hcdr-grid hcdr-grid-3">
  <a class="hcdr-card" href="/en/docs/getting-started/install"><b>Install HyperCDR</b><span>Prerequisites, certificates, access address, install parameters and verification.</span></a>
  <a class="hcdr-card" href="/en/docs/user-guide/first-use"><b>First use</b><span>Register a cluster, prepare storage and policies, configure protection, then complete an isolated drill.</span></a>
  <a class="hcdr-card" href="/en/docs/operations/troubleshooting"><b>Installation troubleshooting</b><span>Check ports, certificates, image pulls, agent connectivity and failing tasks by symptom.</span></a>
</div>

A successful installation only means the control plane starts. Cluster agents, object storage permissions and the recovery path all need separate validation, and installing the platform protects no workload by itself.

::: info Documentation conventions

- Platform commands run in the installation directory, `/var/lib/hypercdr` by default.
- The documentation follows the code, scripts and `env.example` in the repository; replace example values with those of your environment.
- A capability that is available in code is not necessarily validated for every version, driver and topology. Check the [support and compatibility scope](/en/docs/reference/support-scope) for production use.
- Do not write passwords, kubeconfig files, private keys, certificates or database backups into documents, repositories or public issues.

:::
