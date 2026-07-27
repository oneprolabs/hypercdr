# Build, release, and installation flow

HyperCDR uses one source-based build and one profile-driven publication flow.
Generated files are written outside the repository.

## Registry selection

All endpoints are declared in `config/registries.conf`; credentials are never
stored there. `HCDR_ACTIVE_REGISTRY` selects the default. Override it for one
command with `--registry-profile harbor_149`. Authenticate separately with
`docker login`, or inject credentials from CI secrets.

Use the repository script instead of remembering the Registry hostname:

```bash
./scripts/registry-login.sh
```

It logs in to the active profile and lets Docker prompt securely. The login is
normally required only once per build host and operating-system user.

## Local release

```bash
./scripts/release/release-all.sh vYYYYMMDD.N
```

This tests and builds the API, frontend, upgrader, and comm-agent; publishes
PostgreSQL, pinned customized Velero, and three object-storage plugins; verifies
pulls; and optionally registers the release. Use `--skip-register` for the
initial release.

## CI release

Pushing a `vYYYYMMDD.N` tag runs `.github/workflows/release.yml`. Configure
GitHub secrets `REGISTRY_USERNAME` and `REGISTRY_PASSWORD`. The workflow uses
the same profiles and uploads the small installer package as an artifact.

## Installation

```bash
./install-platform.sh docker \
  --public-base-url https://platform.example.com:3002 \
  --execute
```

The installer defaults to the active Alibaba Cloud ACR profile. It persists the
selected prefix in `/var/lib/hypercdr/.env`; platform-generated cluster
installers and component upgrades use the same prefix.
