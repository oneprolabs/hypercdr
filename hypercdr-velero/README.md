# HyperCDR Velero

This repository keeps the Velero source used by HyperCDR.

Current baseline:

- Upstream Velero version: `v1.17.1`
- Upstream commit: `94f64639cee09c5caaa65b65ab5f42175f41c101`
- HyperCDR runtime image tag: `hypercdr/velero:v1.17.1-hcdr.1-20260716`
- Source directory: `velero-1.17.1/`

The HyperCDR platform repository does not build Velero as part of the platform
or agent build. The runtime image is built from the source in this repository
with the upstream Dockerfile workflow. Platform deployment consumes that published image and the
bundled CRDs kept in `hypercdr-platform/platform/backend/internal/veleroassets`.

HyperCDR builds use the upstream Dockerfile and are published with an immutable
`v1.17.1-hcdr.N-YYYYMMDD` tag. Future source extensions are maintained in this
directory and must pass the platform backup and Drill regression suite.

## Build and publish

Run the build from the Velero source directory. Build metadata, downloaded
restic source, and other temporary files are kept outside the repository under
`/tmp/hypercdr-velero-build` by default.

```bash
cd velero-1.17.1
./deployments/build-velero-image.sh \
  --tag v1.17.1-hcdr.N-YYYYMMDD \
  --version v1.17.1-hcdr.N \
  --push
```

The script verifies the recorded upstream commit, builds with the upstream
multi-stage Dockerfile, validates all four runtime binaries and the `cnb:cnb`
runtime user, and only then pushes the image. Re-running a build from unchanged
source and build arguments must produce the same image digest.

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
