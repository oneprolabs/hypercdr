---
title: Build and release maintenance
description: The GitHub Actions release flow, the release manifest and local release tooling.
---

# Build and release maintenance

This page follows `.github/workflows/release.yml`, `config/registries.conf` and `scripts/release/`.

## GitHub Actions

Pushing a `v*` tag triggers a release; the version format is `MAJOR.MINOR.PATCH.YYYYMMDD`. You can also use `workflow_dispatch` to specify the version, registry profile and deploy.

::: warning Default deploy behavior

A manually triggered deploy defaults to `false`; **after a successful release, a tag push deploys automatically to the environment configured in the workflow**. The deployment does not upgrade every installed platform.

:::

Tests and image builds run in parallel after prepare-release, and the final release waits for the required jobs to succeed. Release assets include versioned installation packages and the component manifest. The GitHub Release is published on the repository's Releases page, not in the source tree.

Every release retains its own manifest; **do not replace every installed version with the latest manifest**.

## Image registry

The registry is resolved from the registry profile and the GitHub configuration; the current online installation entry point defaults to the Alibaba registry `registry.cn-beijing.aliyuncs.com/oneprolabs/hypercdr`. Verify the actual address in the workflow output. Credentials come from CI secrets and are never written to documentation or source.

The PR checks neither publish images nor deploy anything. Only a `v*` tag push triggers a release.

## Deployment and release manifest

CI deployment calls the blue-green scripts. The upgrade path for users installing independently is:

```text
Release provides the installer → upgrade page detects the release and creates a task
→ host platform-upgrade-runner.sh reads the task and component info
→ calls deploy-blue-green.sh
```

Traffic switching and current manifest updates are controlled by the deployment script. **Never change only a version tag or hand-fill missing component versions.**

If a switch fails, inspect the script and runner logs; database migrations do not revert when traffic switches.

## Local release tooling

```bash
scripts/release/release-all.sh --config /absolute/path/to/release.conf
```

This script is the local release entry point. Check the required parameters against the configuration example in the repository, and keep the actual configuration, password files and artifacts outside the source tree.

Remote Actions are the public release entry point; the local tooling is not a prerequisite for ordinary users to install.

## Related

- [Installation guide](/en/docs/deployment/install)
- [Blue-green deployment and rollback](/en/docs/operations/blue-green)
- [Platform operations and upgrades](/en/docs/operations/platform)
