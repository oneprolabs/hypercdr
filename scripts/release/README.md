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
three Velero object-storage plugins, verifies Registry pulls, resolves the
remote digest of every platform and cluster component, and writes one complete
immutable `release-manifest.json`. It registers that whole version as a
platform candidate. It never starts an upgrade. Control plane upgrades remain
an explicit administrator action in the platform UI.

The active platform release manifest is the sole runtime source for new Agent
installations and existing Agent/Velero upgrade targets. Components are not
published or activated independently. A platform upgrade activates its
manifest only after the upgrade succeeds; a failure or rollback leaves the
previous manifest active.

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
- `cluster-registration-executor`
- `comm-agent`

The registration executor embeds a checksum-verified `kubectl`. Normal builds
download it from the Kubernetes release service within a bounded total time.
For an air-gapped or slow build host, provide a previously trusted binary and
its pinned digest instead:

```bash
HCDR_REGISTRATION_KUBECTL_VERSION=v1.35.7 \
HCDR_REGISTRATION_KUBECTL_BINARY=/secure/cache/kubectl-v1.35.7 \
HCDR_REGISTRATION_KUBECTL_SHA256=<64-character-sha256> \
./release-all.sh v20260901.1 --config ./release.conf
```

The build rejects a local binary unless the supplied digest matches exactly.

## Unified package and Bootstrap flow

`release-all.sh` is the only recommended full-release entry point. It builds
all control-plane/runtime images, pushes them, writes the complete
`release-manifest.json`, and invokes `package-release.sh` to produce
`hypercdr-installer-<version>.tar.gz`. It does not depend on Bootstrap.

`bootstrap/scripts/package-release.sh` is a compatibility entry point only.
Bootstrap consumes an already-created and checksum-verified platform installer;
it never builds platform images or creates a second platform installer.

The scripts in this directory are grouped as follows:

* Build/publish: `build-release.sh`, `push-release.sh`, `release-all.sh`.
* Runtime/OADP: `publish-runtime-images.sh`, `sync-velero-plugins.sh`,
  `mirror-community-oadp-images.sh`, `build-community-oadp-*.sh`.
* Package/distribution: `package-release.sh`, `publish-package.sh`.
* Platform operations: `install-platform.sh`, `deploy-platform.sh`,
  `start-platform.sh`, `stop-platform.sh`, `restart-platform.sh`,
  `uninstall*.sh`.
* Validation/common: `verify-*.sh`, `common.sh`.

Typical output is written to:

```text
/data/hypercdr-runtime/build/platform/<version>/
/data/hypercdr-runtime/releases/community/<version>/
```

The latter contains the installer archive, SHA256 file, `release-manifest.json`
and `manifest.json`. Bootstrap publishing only copies this directory to its
download source.

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
