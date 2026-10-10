# Build, release, and installation

## Production: unchanged blue/green delivery

The supported first-install entry point is `deploy/online/install.sh`:

```bash
curl -fsSL https://raw.githubusercontent.com/oneprolabs/hypercdr/main/deploy/online/install.sh \
  | sudo bash -s -- \
    --base-url https://platform.example.com:12443 \
    --install-dir /var/lib/hypercdr \
    --yes
```

Omitting `--version` selects the latest release. To install the optional website,
add `--install-website`; it is disabled by default and uses port 12445.
The platform uses 12443. Nginx Proxy Manager is not required.

The installer resolves GitHub Release assets, generates secrets and TLS/edge
configuration, and installs the release's immutable component manifest.
`env.example` documents the production Compose inputs; it deliberately contains
placeholder secrets and image versions and is **not** a runnable installation.
Copying it to `.env` does not prepare certificates, nginx configuration or the
release manifest. Use the installer instead.

CI releases are produced by `.github/workflows/release.yml`. It publishes to the
registry selected by GitHub configuration (currently the Alibaba registry
`registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr`), then creates versioned
GitHub Release assets. Every release retains its own `release-manifest.json`.
Deployment, when enabled in that workflow, uses the existing blue/green
installation, health validation, traffic switch and rollback scripts. The PR
image smoke checks do not publish images or deploy anything.

Registry profiles are declared in `config/registries.conf`; authentication is
separate (`./scripts/registry-login.sh` or CI secrets). Do not commit credentials.
The local source release entry point remains:

```bash
./scripts/release/release-all.sh --config /absolute/path/to/release.conf
```

It builds API, frontend, registration executor and cluster agents, publishes
pinned dependencies, verifies remote images, and packages the manifest and
installer. See [the release runbook](release-flow.zh.md) for options and recovery.

## Portable source development

This is a separate development option, not a production deployment or upgrade.
It needs Docker Engine/Compose v2, Bash, Python 3 and OpenSSL; host Go, Node,
systemd and a pre-existing TLS certificate are not required. Docker downloads
Go/Node base images and dependencies during the build, so network access is
required. If the default package endpoints are unreachable, set
`HCDR_SOURCE_GOPROXY` and `HCDR_SOURCE_NPM_REGISTRY` to your approved mirrors.
Windows users can run the wrapper from WSL; Docker Desktop must expose
the Linux engine there.

Download `release-manifest.json` from the GitHub Release you want to test and
save it **outside the repository**, for example under `../hypercdr-runtime`.
It supplies real cluster-side component images, while the platform API, frontend
and registration executor are built from this checkout.

```bash
./scripts/dev/source-compose.sh up --manifest ../hypercdr-runtime/release-manifest.json
./scripts/dev/source-compose.sh ps
./scripts/dev/source-compose.sh logs --tail 100
./scripts/dev/source-compose.sh down
```

The default browser URL is `http://localhost:13002/login`; this development-only
entry point uses image captcha and binds only to loopback. For access from a test
cluster, explicitly set the reachable host, bind address and matching URLs:

```bash
HCDR_SOURCE_BIND_IP=0.0.0.0 HCDR_SOURCE_PORT=13002 \
HCDR_SOURCE_PUBLIC_URL=http://192.168.8.149:13002 \
HCDR_SOURCE_WS_ENDPOINT=ws://192.168.8.149:13002/ws/agent \
  ./scripts/dev/source-compose.sh up
```

Use this HTTP mode only on a trusted development network. Production still uses
TLS and the standard installer. `up` rebuilds source images. It retains the
manifest, generated secrets and PostgreSQL data. `down` removes containers and
networks, **not** volumes. `down --volumes` also deletes this isolated development
database; never use it when you need to retain test data.

Runtime files default to `../hypercdr-runtime/environments/source-compose`.
Override `HCDR_SOURCE_RUNTIME` and `HCDR_SOURCE_PROJECT` together to create another
isolated environment. No fixed container names are used. PostgreSQL, the API and
executor have no host port exposure. Health checks report local process response;
the separate PostgreSQL check reports database readiness. API `/readyz` currently
checks configuration only and is not represented here as a database probe.

The existing host development flow (`make dev`, `make update-dev`) remains
available, with source provenance reporting. See [development details](../development/platform.md).
