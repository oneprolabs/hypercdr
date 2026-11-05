---
title: Administration
description: Platform lifecycle, upgrades, blue-green rollback, restore cache, diagnostic logs and troubleshooting.
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
  <a class="hcdr-card" href="/en/docs/operations/logging"><b>Diagnostic logs</b><span>Search platform and cluster logs in the control plane, and trace a failure by Task ID or Request ID.</span></a>
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

The point of a routine check is not to confirm that no alert fired. It is to catch broken protection, stale restore points, offline components and repeated failures before an incident — so you do not discover during a recovery that there was no usable restore point.

### Six steps

1. **Look at the whole picture.** Start on the Dashboard for protection activity and alerts, then open **Configuration → Clusters** for agent online status, component state and the last report time.
2. **Look at storage.** Check object-storage connectivity, capacity and credential expiry. Do not test only the network from the control plane — the backup and recovery components must reach the repository too.
3. **Look at protection and restore points.** In **DR → Application DR → Run DR**, check protection configurations, sync status and the next schedule; then confirm the latest usable restore point time in the list.
4. **Look at failed tasks.** Pick out failures and tasks that have not updated in a long time, record the task ID, time, cluster and first error event, then follow it up with [diagnostic logs](/en/docs/operations/logging).
5. **Run a drill.** Execute a drill in an isolated environment on the approved schedule, record the application, data and access checks, and clean up leftover drill resources afterwards.
6. **Prepare backups.** Before platform maintenance, back up the database, `.env`, secret keys, TLS and the release manifest — and verify the backup actually restores. An unverified backup is not a backup.

### Suggested frequency

| Frequency | What to check |
| --- | --- |
| Daily | Cluster online status, failed tasks, upgrade runner service status |
| Weekly | Storage growth, retention rule execution, restore point count |
| Monthly | Run one drill, reconcile component versions against the manifest |
| Before every change | Database and configuration backup, rollback plan |

### What to record

| Item | What to record |
| --- | --- |
| Clusters and components | Online status, last report time, component versions and anomalies |
| Application protection | Configuration state, policy, last sync result and next schedule |
| Restore points | Latest usable time, source application, repository, whether it meets the business requirement |
| Drills | Restore point used, isolated target, task result, business acceptance and cleanup |
| Incident handling | Task ID, first error, impact scope, owner and outcome |

### What counts as done

Every business has a usable restore point that meets its time requirement; every anomaly has an owner and a conclusion; drill results and residual risks are traceable. **The absence of alerts is not proof that recovery works.**

::: warning Do not start a backup or takeover to "test whether the page refreshes"

A misclick really creates a backup task, and a takeover really rewrites the target environment. If you want fresh page state, refresh the page.

:::
