# Bootstrap Portal

This directory contains the deployment helper for the development/test bootstrap
download page.

The portal serves generated static files and release artifacts. It does not
build images, run a registry, or install the HyperCDR control plane.

## Install

Serve the repository bootstrap portal:

```bash
/data/hypercdr-main/bootstrap/deploy-bootstrap.sh \
  --source-dir /data/hypercdr-runtime/services/bootstrap-portal/source \
  --execute
```

Use `--mode python` for a foreground development server when Docker is not
available.
