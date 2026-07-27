# HyperCDR Harbor Helper Tools

Harbor is the recommended image registry for production and customer
deployments. The bootstrap portal and platform installer should use the Harbor
project prefix as the image registry, for example:

```bash
<deploy-host>:5001/hypercdr
```

The control plane stores this value as `HCDR_IMAGE_REGISTRY`. Generated agent
installation scripts then use the same registry for `comm-agent`, `velero`, and
other HyperCDR images.

## Scope

HyperCDR does not own the Harbor lifecycle. Users should install and maintain
Harbor as a prerequisite. This directory provides helper scripts for:

- creating the `hypercdr` Harbor project;
- syncing build base images into a separate `base-images` project;
- syncing the required HyperCDR images from an existing Harbor;
- building and pushing updated platform/agent images;
- importing an image bundle into Harbor;
- verifying the registry prefix used by the platform installer.

## Initialize Projects

Create the `hypercdr` project:

```bash
./init-project.sh \
  --harbor-url https://<deploy-host>:5001 \
  --project hypercdr \
  --username admin \
  --password Harbor12345 \
  --execute
```

Create the `base-images` project:

```bash
./init-project.sh \
  --harbor-url https://<deploy-host>:5001 \
  --project base-images \
  --username admin \
  --password Harbor12345 \
  --execute
```

## Sync Required Images

For a new Harbor, copy only the required HyperCDR images from the existing
Harbor project:

```bash
./sync-required-images.sh \
  --source-registry <source-harbor>/hypercdr \
  --registry <target-harbor>:5001/hypercdr \
  --username admin \
  --password Harbor12345 \
  --execute
```

## Sync Build Base Images

Create a separate Harbor project named `base-images`, then sync common build
base images:

```bash
./sync-base-images.sh \
  --registry <target-harbor>:5001/base-images \
  --username admin \
  --password Harbor12345 \
  --execute
```

## Build and Push Updated Images

After platform or agent source code changes, build the latest images and push
them to Harbor:

```bash
./update-built-images.sh \
  --registry <target-harbor>:5001/hypercdr \
  --base-registry <target-harbor>:5001/base-images \
  --goproxy https://goproxy.cn,direct \
  --gosumdb sum.golang.google.cn \
  --npm-registry https://registry.npmmirror.com \
  --execute
```

## Import Image Bundle

After creating an image bundle with `scripts/export-bootstrap-images.sh`, load
and push it into Harbor:

```bash
./import-images.sh \
  --registry <target-harbor>:5001/hypercdr \
  --bundle-dir ./bootstrap-images \
  --username admin \
  --password Harbor12345 \
  --execute
```

## Platform Install

When installing the HyperCDR control plane, set the image registry to the Harbor
project prefix:

```bash
/data/hypercdr/bootstrap/install-platform.sh k8s \
  --public-base-url http://<node-ip>:30080 \
  --registry <harbor-host>:5001/hypercdr \
  --execute
```
