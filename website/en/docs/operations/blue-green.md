---
title: Blue-green deployment and rollback
description: How the components relate in a blue-green switch, and what a rollback can and cannot restore.
---

# Blue-green deployment and rollback

The current topology comes from the root `docker-compose.yml`.

## Component relationships

Edge terminates TLS on host port 12443 and forwards page and API/WebSocket requests to the **active color** frontend and API. Blue and green each have their own application network and share PostgreSQL; the API also joins an outbound network. The registration executor is a separate internal service. The product documentation site is deployed independently and is not part of this composition.

```text
                    ┌─ blue: frontend + API ─┐
Edge :12443 ────────┤                        ├── PostgreSQL
                    └─ green: frontend + API ┘
                              ↑
                    switch driven by deploy-blue-green.sh
```

## Switching process

`scripts/release/deploy-blue-green.sh`:

1. Start the inactive color;
2. Run health checks;
3. Switch Nginx traffic;
4. Check the result of the switch;
5. Try to restore the original traffic on failure;
6. Activate `current-release.json` for that release on success.

Platform components and cluster components share one release manifest, but a cluster upgrade has its own task and acceptance.

## Rollback

Run this in the install directory:

```bash
./deploy-blue-green.sh --rollback
```

::: danger Rollback scope

A rollback depends on **the retained images, manifest and runtime state of the previous release**. It switches application traffic back to the old release and **does not restore the database**.

If database migrations already ran during the upgrade, the old application faces the new schema after the rollback and may not run correctly. Therefore, back up the database before the upgrade.

:::

## Pre-upgrade checklist

- [ ] PostgreSQL backed up (logical backup or offline consistent backup)
- [ ] `.env` backed up, especially `HCDR_SECRET_KEY`
- [ ] TLS and Compose configuration backed up
- [ ] Previous release images still present under `releases/`
- [ ] Upgrade runner service healthy
- [ ] Cluster components confirmed to require a separate upgrade

## External proxy

No dependency on Nginx Proxy Manager. If you add a proxy, the deployer manages its lifecycle and certificates; platform upgrades do not modify them.

## Related

- [Installation guide](/en/docs/deployment/install)
- [Configuration reference](/en/docs/deployment/configuration)
- [Platform operations and upgrades](/en/docs/operations/platform)
- [Build and release maintenance](/en/docs/operations/release)
