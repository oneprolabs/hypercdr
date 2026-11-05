---
title: Limitations and security recommendations
description: Behavior the product cannot deliver or that you should not rely on, plus security defaults you must handle after deployment.
---

# Limitations and security recommendations

This page has two parts: **capability boundaries** (behavior the product cannot deliver or that you must not rely on) and **security defaults** (unsafe default configurations you must adjust after installation).

## Capability boundaries

### Versions and upgrades

| Limitation | Explanation |
| --- | --- |
| A platform upgrade does not include component upgrades | The in-cluster agent, Velero and OADP need separate upgrade tasks and acceptance |
| Migrations are irreversible | Applied SQL migrations are not reverted by a traffic rollback |
| Rollback does not restore data | `--rollback` only switches back the application version; it does not restore the database |
| Single persistence implementation | PostgreSQL is the only implementation; there is no in-memory fallback |

### Backup and recovery

| Limitation | Explanation |
| --- | --- |
| Storage availability depends on the cluster side | The platform cannot bypass CSI plugin, network policy or credential problems |
| Task success ≠ recovery usable | You must validate recovery with a drill |
| A partial recovery may still report success | When a few resources fail, the task status may still look normal; check every item |
| Recovery overwrites the target | Verify the resource mapping before you run it |
| The restore cache does not fall back automatically | If creating the cache PVC fails at runtime, the task does not fall back to a temporary disk; preflight checks block it |

### Cluster-type specifics

CCE and OpenShift each have a phase-one scope, and capabilities outside it are not supported:

- CCE: no Autopilot, Windows nodes, ARM64 nodes or exec-plugin kubeconfigs; see [CCE notes](/en/docs/reference/cce).
- OpenShift: full backups only, RWO PVCs, S3 storage and the OADP `stable-1.3` channel; see [OpenShift notes](/en/docs/reference/openshift).

## Security recommendations

### Required changes after deployment

::: danger Three configurations to change

1. **Change the initial password.** The first sign-in uses `admin` / `admin123`; change it before any business use.
2. **Replace the self-signed certificate.** Browsers do not trust the certificate the installer generates; production should use a certificate issued by your own CA.
3. **Protect `HCDR_SECRET_KEY`.** It protects the platform's secret data; if you lose it, restoring only the database will not decrypt anything.

:::

### Credential management

- `.env`, kubeconfig files, private keys, certificates and database backups **must never be committed to a repository**.
- Before sharing logs or error messages, remove tokens, passwords, kubeconfigs and private keys.
- The Google Client Secret must not go into the frontend or into a repository.
- Use a dedicated least-privilege account for object storage credentials, and do not reuse it in other systems.
- Registration commands contain access credentials; redact them before pasting into an issue.

### Network

- Only 12443 is exposed on the host; the database, API, frontend and registration executor do not publish ports directly.
- If you use an external proxy, the deployment owner manages its lifecycle and certificates; platform upgrades do not change them.
- Agents use **outbound** connections, so the platform does not need to reach clusters.

### Data protection

- Copying the PostgreSQL data directory while the database is running does not produce a consistent backup.
- Before an upgrade, back up PostgreSQL, `.env`, TLS, the Compose configuration and the release manifest.
- Application backup data lives in object storage and has a lifecycle independent of the platform database; set a retention policy for each.
- `--purge-data` deletes the install directory and cannot be undone; confirm the cleanup scope before you run it.

### Common operational error

::: warning Error codes and commands

`component_target_unavailable` and similar values are error codes, not shell scripts you can paste and run. When you see an unfamiliar error, first establish what kind of message it is, then decide what to do.

:::

## Related

- [Support and compatibility scope](/en/docs/reference/support-scope)
- [Configuration reference](/en/docs/deployment/configuration)
- [Platform operations and upgrades](/en/docs/operations/platform)
