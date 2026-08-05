# HyperCDR Velero

This repository keeps the Velero source used by HyperCDR.

Current baseline:

- Upstream Velero version: `v1.18.2`
- Upstream commit: `c253c7fe37d78c9b7e55c68544f7c5b2608712d8`
- HyperCDR runtime image tag: `hypercdr/velero:v1.18.2-hcdr.2`
- Source directory: `third_party/velero/`

The HyperCDR platform repository does not build Velero as part of the platform
or agent build. The runtime image is built from the source in this repository
with the upstream Dockerfile workflow. Platform deployment consumes that published image and the
bundled CRDs kept in `hypercdr-platform/platform/backend/internal/veleroassets`.

HyperCDR builds use the upstream Dockerfile and are published with an immutable
`v1.18.2-hcdr.N` tag. Future source extensions are maintained in this
directory and must pass the platform backup and Drill regression suite.

## Build and publish

Run the build from the Velero source directory. Build metadata, downloaded
restic source, and other temporary files are kept outside the repository under
`/tmp/hypercdr-velero-build` by default.

```bash
cd /data/hypercdr-main/third_party/velero
./deployments/build-velero-image.sh \
  --registry REGISTRY/NAMESPACE \
  --tag v1.18.2-hcdr.N \
  --version v1.18.2-hcdr.N \
  --push
```

The script verifies the recorded upstream commit, builds with the upstream
multi-stage Dockerfile, validates all four runtime binaries and the `cnb:cnb`
runtime user, and only then pushes the image. Re-running a build from unchanged
source and build arguments must produce the same image digest.

Velero v1.18 uses Kopia for all new file-system backups and CSI data movement.
The upstream image still includes Restic 0.15 solely so v1.18 can restore and
maintain legacy Restic repositories. Restic backup creation is disabled in
Velero v1.17 and v1.18 and HyperCDR does not configure it as an uploader.

## v1.18 runtime defaults

- Velero backup workers: 2. Velero queues backups whose namespace sets overlap.
- Node-agent data movement workers per node: 2.
- Node-agent prepare queue length: 4.
- DataMover cache PVC: enabled when the cluster reports a default StorageClass;
  backups below 1024 MiB stay on ephemeral cache.
- Incremental bytes: read from v1.18 `DataUpload` and `PodVolumeBackup` status
  and stored separately from logical volume size.

## HyperCDR v1.18.2 fixes

- `v1.18.2-hcdr.2` clamps a small Kopia cached-byte accounting skew to zero so
  `incrementalBytes` can never be negative.
- A completed `DataUpload` or `PodVolumeBackup` with an omitted
  `incrementalBytes` field is treated as a known zero. The Velero API uses
  `omitempty`, so zero is normally absent from the serialized object.
- Restore-point `totalBytes` and `volumeBytes` describe the logical protected
  data. `uploadedBytes` and `uploadedVolumeBytes` describe bytes uploaded by
  that backup. For a fully deduplicated backup, uploaded volume bytes are zero
  while logical volume bytes remain unchanged.
- Comm Agent `v20260805.4` carries the known-zero value through terminal task
  size aggregation. Earlier agents could incorrectly replace it with the
  logical volume size.
- `scripts/release/build-velero.sh` passes the image tag as the binary version;
  the image tag and `velero version` output must therefore match.
- Terminal task completion explicitly clears the last running
  `volumeProgress` sample. This prevents a succeeded task from displaying a
  stale `InProgress` volume while retaining final restore-point size data.

## Release gate

Before making a custom image the platform default:

1. Compare its runtime user, architecture, image layout, Velero version, and
   restic version with the upstream image.
2. Deploy the same immutable image digest to the Velero deployment and every
   node-agent pod.
3. Create a new backup through the HyperCDR platform, not by manually creating
   a Velero CR.
4. Run a cross-cluster Drill from that recovery point.
5. Verify the Restore and PodVolumeRestore completed, workloads are Ready, PVCs
   are Bound, and application data matches the source data.
6. Run the platform backend, agent, and frontend build/test suites.
