# Local Package Installation and Upgrade

## Scope

This guide covers a Community control plane on a dedicated Linux Docker host.
The **same versioned package** supports both first installation and upgrading an
existing installation. There is no separate upgrade archive.

This is a small online-image installer, not a fully offline image bundle. Copy
the archive to the host; the installer still pulls images from the configured
registry. Neither Bootstrap nor Release Center, nor a Release Center token, is
required for this workflow. Registry authentication, if required, is separate.

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
certificates, lifecycle scripts, and deployment files. It defaults to
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
- Allow access to the selected frontend port, normally TCP 3002. If the registry
  requires credentials, run `docker login REGISTRY_HOST` on this host first.
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
./install-platform.sh docker --base-url https://192.0.2.10:3002
```

Stop if checksum verification fails. Do not mix scripts or manifests from
different releases or edit the package to refer to an unrelated image version.

## First installation

### Private address only

```bash
./install.sh --base-url https://192.0.2.10:3002 \
  --install-dir /var/lib/hypercdr --check

./install.sh --base-url https://192.0.2.10:3002 \
  --install-dir /var/lib/hypercdr
```

Replace `192.0.2.10` with the real host address. `--check` performs prerequisite
checks without installation; it does not prove images can all be pulled or that
the application will start. For installation, review the displayed settings and
type `YES` at the interactive prerequisite prompt.

### Private address and optional public address

```bash
./install.sh --base-url https://192.0.2.10:3002 \
  --public-base-url https://control-plane.example.com:3002 \
  --install-dir /var/lib/hypercdr
```

| Option | Meaning |
| --- | --- |
| `--base-url` | Primary HTTPS address reachable by users and agents; can be private or public. Its explicit port determines the frontend listening port. Without a port, HTTPS uses 443. |
| `--public-base-url` | Optional externally reachable control-plane address; omit when unused. It does not configure NAT, public DNS, or firewalls for you. |
| `--install-dir` | Persistent installation directory, not just a temporary download folder |
| `--check` | Validate prerequisites without installing |

Other settings, such as registry, API port, and an existing TLS certificate/key,
are in `install-config.sh`. Both TLS file settings must be supplied together.
The package generator sets `HCDR_IMAGE_TAG`; normally leave it unchanged.
`HCDR_INSTALL_CONFIG` can point to a different trusted configuration file.

The installer can generate a self-signed certificate. For a lab, use `curl -k`
to check it; for production, use a trusted certificate and normal verification.

## Upgrade using the new version's package

### Current limitations: read before execution

`upgrade-local.sh` currently invokes the lower-level installer immediately with
`--execute` and prerequisite confirmation already set. It has **no interactive
confirmation, no dry-run option, and no automatic rollback or database backup**.
Do not add `--check` or `--execute` to this wrapper; it does not accept them.

The installer rewrites `.env` and the Compose file. It retains the existing
database password, platform secret, and local release/registration credentials
through their supported settings/files, and normally reuses existing TLS files.
It does **not** preserve arbitrary `.env` settings or custom Compose edits. The
wrapper does not forward public-address, private-CA, or custom API-port options.
It does not read `install-config.sh`, and it does not automatically infer the
target image tag from the manifest.

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
  --base-url https://192.0.2.10:3002 \
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
curl -kfsS https://192.0.2.10:3002/readyz
docker inspect hypercdr-platform-api hypercdr-platform-frontend \
  --format '{{.Name}} {{.Config.Image}} {{.State.Status}}'
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
