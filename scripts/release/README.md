# Standard Release Scripts

This directory contains the standard HyperCDR platform release scripts.

## One-command release

Registry endpoints live in `../../config/registries.conf`. Select the default
with `HCDR_ACTIVE_REGISTRY`, then copy the non-secret release settings once:

```bash
cp release.conf.example release.conf
```

Edit `release.conf`, then build and push a release:

```bash
./release-all.sh v20260727.1 --config ./release.conf
```

After tests pass, the release script builds and pushes the images, mirrors the
three Velero object-storage plugins, verifies Registry pulls, and registers the
version as a platform candidate. It never starts an upgrade. Control plane
upgrades remain an explicit administrator action in the platform UI.

For the initial seed release, when no platform exists yet, use
`--skip-register`. Normal releases require the installer-generated token at
`/var/lib/hypercdr/release-token`.

## Lower-level flow

The one-command script calls these lower-level scripts:

```bash
./build-release.sh v20260727.1 --registry registry.example.com/namespace
./push-release.sh v20260727.1 --registry registry.example.com/namespace
```

These scripts build and push:

- `platform-api`
- `platform-frontend`
- `platform-upgrader`
- `comm-agent`

Build work is written to `/data/hypercdr-runtime/build/platform/<version>` and shared
Go/npm caches are written to `/data/hypercdr-runtime/cache` by default. Override them
with `HCDR_BUILD_ROOT` and `HCDR_CACHE_ROOT`. The source tree is not used for
dependencies, compiled binaries, or frontend output.

`deploy-platform.sh` and `verify-platform.sh` are retained as local maintenance
tools, but they are not part of the standard release path.

Velero is intentionally not built here. Build Velero from the Velero source tree:

```bash
/data/hypercdr/third_party/velero/deployments/build-velero-image.sh --push
```
