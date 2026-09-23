# Local Package Installation and Upgrade

The packaged blue/green deployment expects an outer reverse proxy to terminate
HTTPS and route to the internal HyperCDR HTTP service. Nginx Proxy Manager must
share a Docker network with `hypercdr-edge` and forward to
`http://hypercdr-edge:80`. Set `HCDR_PROXY_NETWORK` in `install-config.sh` to the
existing NPM Docker network name. Do not expose inner service ports on the
public host. `--base-url` is the public address used by the platform and agents;
an HTTPS URL without an explicit port uses standard public port `443`.

## Scope

This guide covers a Community control plane on a dedicated Linux Docker host.
The **same versioned package** supports both first installation and upgrading an
existing installation. There is no separate upgrade archive.

This is a small online-image installer, not a fully offline image bundle. Copy
the archive to the host; the installer still pulls images from the configured
registry. Neither Bootstrap nor Release Center, nor a Release Center token, is
required for this workflow. Registry authentication, if required, is separate.

The private installer may contain the Cloudflare Turnstile deployment Secret
Key in `install-config.sh`. Store the archive with restricted permissions and
share it only with authorized operators. A package built with Turnstile enabled
installs that login challenge by default; no separate CAPTCHA configuration is
required on the target host.

## Package contents

| File | Purpose |
| --- | --- |
| `README.md` | This installation and upgrade guide |
| `install.sh` | First-install entry point with interactive prerequisite confirmation |
| `install-config.sh` | Defaults for `install.sh`; command-line address/directory options override them |
| `install-platform.sh` | Lower-level installer used by both entry points |
| `upgrade-local.sh` | Entry point for an existing Docker installation |
| `release-manifest.json` | Release version and component image metadata |
| `compose.yaml` | Docker Compose template |
| `config/registries.conf` | Registry profiles |
| `start-platform.sh`, `stop-platform.sh`, `restart-platform.sh` | Service lifecycle helpers |
| `uninstall.sh`, `uninstall-platform.sh` | Uninstall helpers; do not run these before an upgrade |
| `PLATFORM-LIFECYCLE.md` | Detailed lifecycle and uninstall instructions |
| `charts/` | Kubernetes assets; the Docker examples below do not use these |

The extracted package directory is not the installation directory.
`--install-dir` selects the persistent directory containing runtime configuration,
lifecycle scripts, and deployment files. It defaults to
`/var/lib/hypercdr`. Keep the same installation directory during upgrades.

## Prerequisites

Use a maintenance window for upgrades: containers can restart and the UI may be
temporarily unavailable. Before starting:

- Install Docker Engine, Docker Compose V2, Bash, curl, and OpenSSL. The scripts
  check prerequisites; they do not install these tools automatically.
- Install tar and sha256sum to extract and verify the archive.
- Use root, or equivalent Docker and installation-directory permissions. Root
  is recommended when installing the systemd service.
- Ensure Docker is running, the host has sufficient free disk space (the
  installer requires at least 10 GiB and recommends 100 GiB), and registry DNS,
  network connectivity, and certificate trust work.
- Configure the NPM Proxy Host to forward to `http://hypercdr-edge:80`, and set
  `HCDR_NPM_UPSTREAM_READY=true` in `install-config.sh` only after that route and
  the shared Docker network are ready. Allow public 80/443 on NPM; HyperCDR does
  not publish host ports. If the registry requires credentials, run
  `docker login REGISTRY_HOST` on this host first.
- Enable Docker at boot when automatic recovery after a host restart is needed.

```bash
docker version
docker compose version
bash --version
curl --version
openssl version
tar --version
sha256sum --version
systemctl is-active docker
systemctl is-enabled docker
```

## Verify and extract the package

Obtain the archive and matching checksum from a trusted release source. Replace
the example version with the version actually delivered to you.

```bash
VERSION=1.0.23.20260915
sha256sum -c "hypercdr-installer-${VERSION}.sha256"
tar -xzf "hypercdr-installer-${VERSION}.tar.gz"
cd "hypercdr-installer-${VERSION}"
./install.sh --help
./upgrade-local.sh --help
```

For a standalone Docker host, use `install.sh` as the package entry point; it
selects the Docker deployment automatically. The lower-level
`install-platform.sh` script requires the explicit `docker` mode:

```bash
./install-platform.sh docker --base-url https://hypercdr.example.com
```

Stop if checksum verification fails. Do not mix scripts or manifests from
different releases or edit the package to refer to an unrelated image version.

## First installation

### Single address (public or private)

```bash
./install.sh --base-url https://hypercdr.example.com \
  --install-dir /var/lib/hypercdr --check

./install.sh --base-url https://hypercdr.example.com \
  --install-dir /var/lib/hypercdr
```

Replace the example domain with the address served by your reverse proxy.
`--check` performs prerequisite checks without installation; it does not prove
images can all be pulled or that the application will start. For installation,
review the displayed settings and type `YES` at the interactive prerequisite
prompt.

### Private address and optional public address

```bash
./install.sh --base-url https://hypercdr.example.com \
  --public-base-url https://control-plane.example.com \
  --install-dir /var/lib/hypercdr
```

| Option | Meaning |
| --- | --- |
| `--base-url` | Primary HTTPS address reachable by users and agents; it is served by the outer reverse proxy. |
| `--public-base-url` | Optional externally reachable control-plane address; omit when unused. It does not configure NAT, public DNS, or firewalls for you. |
| `--install-dir` | Persistent installation directory, not just a temporary download folder |
| `--check` | Validate prerequisites without installing |

Other settings, such as registry, NPM Docker network, and the
NPM-ready acknowledgement, are in `install-config.sh`. TLS certificates are
managed by NPM, not mounted into HyperCDR containers. The package generator sets
`HCDR_IMAGE_TAG`; normally leave it unchanged.
`HCDR_INSTALL_CONFIG` can point to a different trusted configuration file.

## Upgrade using the new version's package

### Current limitations: read before execution

`upgrade-local.sh` currently invokes the lower-level installer immediately with
`--execute` and prerequisite confirmation already set. It has **no interactive
confirmation, no dry-run option, and no automatic rollback or database backup**.
Do not add `--check` or `--execute` to this wrapper; it does not accept them.

The installer rewrites `.env` and the Compose file. It retains the existing
database password, platform secret, local release/registration credentials,
NPM network name, and NPM-ready acknowledgement through their supported
settings/files. TLS certificates remain managed by NPM.
It does **not** preserve arbitrary `.env` settings or custom Compose edits. The
wrapper does not forward public-address, private-CA, or custom API-port options.
It reads the package's `install-config.sh` for login-challenge settings, but it
does not automatically infer the target image tag from the manifest.

Therefore the wrapper is currently suitable only for the standard Docker setup
after a configuration review. If your installation has a public fallback address,
custom ports, private registry CA, external database, custom Compose configuration,
or additional environment settings, do not blindly run the wrapper. Resolve the
configuration-preservation requirements before upgrading. Do not assume that
using the same directory preserves all settings.

### Back up and record the current deployment

Back up the database and installation directory before modifying services.
For the standard bundled PostgreSQL deployment, an example is:

```bash
INSTALL_DIR=/var/lib/hypercdr
BACKUP_DIR="/var/backups/hypercdr/$(date -u +%Y%m%dT%H%M%SZ)"
install -d -m 700 "$BACKUP_DIR"
(umask 077; tar -C "$INSTALL_DIR" -czf "$BACKUP_DIR/installation.tar.gz" .)
(umask 077; docker exec hypercdr-postgres \
  pg_dump -U hypercdr -d hypercdr -Fc > "$BACKUP_DIR/database.dump")
docker exec -i hypercdr-postgres pg_restore --list < "$BACKUP_DIR/database.dump"
docker compose --project-name hypercdr \
  -f "$INSTALL_DIR/docker-compose.yaml" ps
```

Keep these backups protected: configuration contains credentials. An installation
directory archive alone is not a database backup; Docker volumes may be stored
elsewhere. Adapt the database backup for nonstandard deployments, and verify that
your restore procedure works. Record the previous image versions as well.

### Execute a standard upgrade

Run this from the **newly extracted package**, not an old installation directory:

```bash
./upgrade-local.sh \
  --base-url https://hypercdr.example.com \
  --image-tag 1.0.23.20260915 \
  --install-dir /var/lib/hypercdr \
  --registry crpi-tne0uo16mzanbvpi.cn-zhangjiakou.personal.cr.aliyuncs.com/hypercdr
```

Replace the tag with the exact version in the new package's manifest, keep the
existing primary address and installation directory, and specify the current
registry explicitly. The wrapper requires an existing `<install-dir>/.env`.
The operation restarts/recreates platform services as required; it is not a
zero-downtime upgrade. Do not uninstall or delete data volumes first.

## Verify after installation or upgrade

```bash
systemctl is-enabled hypercdr
systemctl is-active hypercdr
docker compose --project-name hypercdr \
  -f /var/lib/hypercdr/docker-compose.yaml ps
curl -fsS https://hypercdr.example.com/readyz
docker ps --filter name=hypercdr-platform-api- --filter name=hypercdr-platform-frontend-
```

Expect the service to be enabled and active, `/readyz` to return HTTP 200 with
`status: ok`, and the containers to use the intended release images without
restart loops. Then sign in through the actual browser address, confirm the
version and existing configuration/data, and check agent connectivity. A
successful dry-run or HTTP check alone is not complete upgrade verification.

## Routine operations and troubleshooting

```bash
systemctl start hypercdr
systemctl stop hypercdr
systemctl restart hypercdr
journalctl -u hypercdr --since '15 minutes ago'
docker compose --project-name hypercdr \
  -f /var/lib/hypercdr/docker-compose.yaml logs --tail=200
```

If an image pull fails, check registry reachability, credentials, CA trust, and
the existence of the exact release tag. If the existing installation is not
found, check `--install-dir`; do not create an empty `.env` to bypass the check.
If the readiness check fails, retain the logs and backups and investigate before
retrying. Do not downgrade images blindly after database migrations. Restore
requires the matching configuration, image versions, and a compatible database
backup. See `PLATFORM-LIFECYCLE.md` for lifecycle and uninstall details.
