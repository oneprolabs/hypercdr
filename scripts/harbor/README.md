# Optional Harbor helper tools

These scripts are optional tools for operators maintaining their own Harbor registry. Harbor is not required by the public online installer, whose default registry is Alibaba ACR. HyperCDR does not install or manage Harbor.

- `init-project.sh`: initialize a project.
- `sync-base-images.sh`, `sync-required-images.sh`: mirror images.
- `update-built-images.sh`: update built images.
- `import-images.sh`: import a separately prepared local image bundle.

Read each script's argument parser before use and supply your environment's registry and credentials. The active release manifest, not this directory's historical image lists, is the authority for deployed component versions. Do not assume registry substitution preserves naming or digests.

Current installation and publication are documented in [the documentation index](../../docs/README.md). These helpers are not the installation path and do not replace GitHub Release assets.
