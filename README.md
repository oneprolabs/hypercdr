# HyperCDR

[中文](README.zh-CN.md) | English

HyperCDR is a Kubernetes disaster recovery platform. The control plane manages clusters, protection policies, synchronization, restore points, drills, recovery tasks and component upgrades. Cluster agents connect outbound to the platform and coordinate Velero or the OpenShift OADP path.

Community supports self-service email registration, Google sign-in and organization invitations. Each account belongs to one organization; DR resources are isolated by organization. Configure system SMTP for verification/invitations and optional Google credentials as described in the [configuration guide](docs/deployment/configuration.zh.md).

## Install a released version

Use a Linux host with Docker Engine/Compose v2, Bash, curl, tar, jq and OpenSSL. Replace the example address with one reachable by browsers and cluster agents.

```bash
curl -fsSL https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- --base-url https://platform.example.com:12443 --install-dir /var/lib/hypercdr --yes
```

No version selects the latest GitHub Release; add `--version VERSION` to pin one. The platform uses HTTPS 12443. Add the no-value flag `--install-website` to install the optional website on HTTPS 12445. An external Nginx Proxy Manager or local Harbor is not required. Docker must already be installed.

Initial login is `admin` / `admin123`; change the password before business use. Authentication supports image captcha or Cloudflare Turnstile, according to the installed configuration. Generated TLS certificates are not automatically trusted by browsers.

Read the [installation guide](docs/deployment/deployment-guide.zh.md), [first-use guide](docs/user-guide/first-use.zh.md), [configuration](docs/deployment/configuration.zh.md), and [troubleshooting](docs/operations/troubleshooting.zh.md). Published artifacts are in [GitHub Releases](https://github.com/oneprolabs/hypercdr/releases).

## How it works

```text
Browser → HTTPS edge → active blue/green frontend + Go API → PostgreSQL
Cluster agent → outbound WSS → platform tasks and inventory
Cluster agent → Kubernetes API → Velero / OADP → object storage
```

PostgreSQL persists business state; there is no MemoryStore fallback. Platform-direct registration additionally requires the registration executor to reach the cluster API using kubeconfig. Command registration runs on a host with that connectivity.

GitHub Release manifests pin component versions. Installation and successful blue/green upgrades activate a local manifest; cluster registration and component upgrades use its component catalog. The host upgrade runner executes platform upgrade tasks. Platform upgrades do not imply that every cluster component has upgraded.

See [current architecture](docs/architecture/control-plane-architecture.md) and [documentation index](docs/README.md). Repository assets do not certify every distribution, driver or deployment topology; consult the relevant compatibility and verification documents.

## Source layout

| Directory | Responsibility |
|---|---|
| `backend/` | API, PostgreSQL, migrations, scheduling, upgrades |
| `frontend/` | React UI and shared components |
| `agent/comm-agent/` | Cluster communication and task execution |
| `deploy/online/` | Public installer |
| `scripts/release/` | Build, packaging, blue/green and lifecycle tools |
| `website/` | Independent public website |
| `docker/`, `charts/` | Deployment/image assets; charts are not the online install path |
| `third_party/velero/` | Pinned source used by component builds |
| `docs/` | Usage, architecture, deployment and operations |

## Development and contributions

Runtime data, caches, builds and logs belong in the sibling `hypercdr-runtime` directory. Host development uses Go 1.25.13, Node.js 22, Docker/Compose, PostgreSQL and OpenSSL; see [development setup](scripts/dev/README.md). A separate full-container source workflow is documented in [source development](docs/deployment/build-release-install.md#portable-source-development).

```bash
make dev
make status
make update-dev
make stop
make verify
```

Backend tests use isolated real PostgreSQL databases. Never point test configuration at a business database. See [test setup](docs/development/platform.md) and [CONTRIBUTING](CONTRIBUTING.md).

Tag releases currently auto-deploy to the CI-configured environment; manual workflow deployment defaults off. PR checks do not publish or deploy. See the [release runbook](docs/deployment/release-flow.zh.md).

Do not commit credentials, kubeconfigs, private keys, certificates, database backups or runtime artifacts. Report vulnerabilities according to [SECURITY.md](SECURITY.md). See [LICENSE](LICENSE).

## Release version policy

Community tags use `ce-vMAJOR.MINOR.PATCH`, starting at `ce-v0.0.0`. Enterprise tags use `ee-vMAJOR.MINOR.PATCH`, starting at `ee-v0.0.0`; the two editions advance independently. Dates and Git commits are build metadata, not version suffixes. Historical releases remain valid explicit installation and rollback targets. The new prefixed numbering era follows historical releases even though its numeric portion starts at zero.

Enterprise pins the Community source commit and published component release in `community.lock`. Changing an Enterprise version does not change that dependency automatically. A missing dependency release fails validation rather than silently substituting another build.
