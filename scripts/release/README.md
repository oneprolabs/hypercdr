# Standard Release Scripts

This directory contains the standard HyperCDR platform release scripts.

## One-command release

Copy the config template once:

```bash
cp release.conf.example release.conf
```

Edit `release.conf`, then build and push a release:

```bash
./release-all.sh v20260714.5 --config ./release.conf
```

After tests pass, the release script builds and pushes the images, mirrors the
three Velero object-storage plugins, verifies Harbor pulls, and registers the
version as a platform candidate. It never starts an upgrade. Control plane
upgrades remain an explicit administrator action in the platform UI.

For the initial seed release, when no platform exists yet, use
`--skip-register`. Normal releases require the installer-generated token at
`/var/lib/hypercdr/release-token`.

## Lower-level flow

The one-command script calls these lower-level scripts:

```bash
./build-release.sh v20260714.5 --registry 192.168.8.149:5001/hypercdr
./push-release.sh v20260714.5 --registry 192.168.8.149:5001/hypercdr
```

These scripts build and push:

- `platform-api`
- `platform-frontend`
- `platform-upgrader`
- `comm-agent`

Build work is written to `/data/hypercdr/.build/platform/<version>` and shared
Go/npm caches are written to `/data/hypercdr/.cache` by default. Override them
with `HCDR_BUILD_ROOT` and `HCDR_CACHE_ROOT`. The source tree is not used for
dependencies, compiled binaries, or frontend output.

`deploy-platform.sh` and `verify-platform.sh` are retained as local maintenance
tools, but they are not part of the standard release path.

Velero is intentionally not built here. Build Velero from the Velero source tree:

```bash
/data/hypercdr/hypercdr-velero/velero-1.17.1/deployments/build-velero-image.sh --push
```
