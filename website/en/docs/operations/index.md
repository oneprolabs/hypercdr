---
title: Administration
description: Platform lifecycle, upgrades, blue-green rollback, restore cache and troubleshooting.
---

# Administration

This section is for administrators who run the platform long term. It covers the contents of the install directory, the upgrade path and troubleshooting.

## Platform layout

- The install directory defaults to `/var/lib/hypercdr` and holds `.env`, TLS, Compose, persistent directories and the release manifest.
- The platform and the upgrade runner are both systemd services: `hypercdr.service` and `hypercdr-upgrade-runner.service`.
- Upgrades switch the API and frontend in a blue-green deployment; PostgreSQL is shared.
- The release manifest is `current-release.json`; previous releases are kept under `releases/`.

## In this section

<div class="hcdr-grid">
  <a class="hcdr-card" href="/en/docs/operations/platform"><b>Platform operations and upgrades</b><span>Lifecycle scripts, service status, backup essentials, the upgrade chain and uninstall.</span></a>
  <a class="hcdr-card" href="/en/docs/operations/blue-green"><b>Blue-green deployment and rollback</b><span>How the switch works, and the scope a rollback can and cannot restore.</span></a>
  <a class="hcdr-card" href="/en/docs/operations/restore-cache"><b>Velero restore cache</b><span>Cache policy during restore, thresholds, and handling a failed preflight.</span></a>
  <a class="hcdr-card" href="/en/docs/operations/troubleshooting"><b>Troubleshooting</b><span>Match a symptom to a cause, with log collection commands.</span></a>
  <a class="hcdr-card" href="/en/docs/operations/release"><b>Build and release maintenance</b><span>The release workflow, the release manifest and local release tooling.</span></a>
</div>

## Operations constraints

::: danger Back up before every upgrade

An upgrade runs database migrations, and **a traffic rollback does not undo applied SQL migrations**. Before upgrading, back up PostgreSQL, `.env` (especially `HCDR_SECRET_KEY`), TLS, Compose configuration and the release manifest.

:::

::: warning Check the result of a queued task

The upgrade runner on the host executes a platform upgrade task only after it is queued. Check the runner status and the final result; a created task only means the task is queued.

:::

::: warning Cluster components need a separate upgrade

After the platform upgrade finishes, cluster-side components still need a separate upgrade task, and the version the agent reports must be checked.

:::

## Routine checks

| Frequency | What to check |
| --- | --- |
| Daily | Cluster online status, failed tasks, upgrade runner service status |
| Weekly | Storage growth, retention rule execution, restore point count |
| Monthly | Run one drill, reconcile component versions against the manifest |
| Before every change | Database and configuration backup, rollback plan |
