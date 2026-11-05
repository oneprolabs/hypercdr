---
title: Platform operations and upgrades
description: Starting and stopping the platform, checking service status, the upgrade chain, backup and restore essentials, and how to uninstall.
---

# Platform operations and upgrades

The default install directory `/var/lib/hypercdr` holds `.env`, TLS, Compose, persistent directories and the release manifest. Run the following commands in that directory.

## Start, stop and status

```bash
./start-platform.sh
./stop-platform.sh
./restart-platform.sh
sudo systemctl status hypercdr.service hypercdr-upgrade-runner.service
```

## Upgrade flow

```text
Upgrade page fetches GitHub release info → creates an upgrade task
→ host upgrade runner reads the queue → generates the target manifest
→ calls the blue-green deploy script → switches traffic → activates the new manifest
```

::: warning Upgrade result check

Check that the runner is running. **A queued task only means the task was submitted; the upgrade result is the status the runner reports for that task.** A cluster component upgrade also requires checking the version the agent reports and the task result.

:::

## Rollback

When the previous release is still available:

```bash
./deploy-blue-green.sh --rollback
```

It restores application traffic and the release manifest, and **does not restore the database**. If migrations have already run, the schema does not revert with the rollback.

## Backup and restore

Before an upgrade or a host migration, back up:

| Item | Notes |
| --- | --- |
| PostgreSQL | Business state; a complete restore also requires the other items |
| `.env` | Especially `HCDR_SECRET_KEY`; without it protected data cannot be decrypted |
| TLS | Certificates and private keys |
| Compose / proxy configuration | Runtime topology and external proxy rules |
| Release manifest | `current-release.json` and `releases/` |

::: danger Do not copy a live data directory

Copying the PostgreSQL data directory while the database is running is not a consistent backup. Use a PostgreSQL logical backup, or stop the platform and take a consistent backup.

:::

A restore needs matching PostgreSQL and application versions and **the original key that protects the secret data**. Database files without that key are not a complete backup.

After a restore, check sign-in, storage credentials, agent connections, tasks and the upgrade runner.

Application backup data is stored in object storage, independent of the platform database: restoring the platform does not restore business backups, and restoring business backups does not restore the platform.

## Uninstall

```bash
./uninstall.sh --execute
```

Data is retained by default. After confirming the backup and the cleanup scope:

```bash
./uninstall.sh --purge-data --execute
```

`--purge-data` deletes the install directory and **cannot be undone**.

External proxies, external object storage and business clusters are not part of the platform install directory and are managed separately.

For more on the platform lifecycle scripts, see `scripts/release/PLATFORM-LIFECYCLE.md`.

## Related

- [Blue-green deployment and rollback](/en/docs/operations/blue-green)
- [Troubleshooting](/en/docs/operations/troubleshooting)
- [Configuration reference](/en/docs/deployment/configuration)
