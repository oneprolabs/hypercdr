# HyperCDR Velero

This repository keeps the Velero source used by HyperCDR.

Current baseline:

- Upstream Velero version: `v1.17.1`
- HyperCDR runtime image tag: `hypercdr/velero:v1.17.1-helperfix`
- Source directory: `velero-1.17.1/`

The HyperCDR platform repository does not build Velero as part of the platform
or agent build. Platform deployment consumes the published Velero image and the
bundled CRDs kept in `hypercdr-platform/platform/backend/internal/veleroassets`.

If Velero source is changed, commit the change in this repository, rebuild the
Velero image, push it to the target Harbor registry, and update the platform
image list only if the image name or tag changes.
