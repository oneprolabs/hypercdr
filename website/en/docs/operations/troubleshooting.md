---
title: Troubleshooting
description: Match a symptom to a cause, with log collection commands and common misdiagnoses.
---

# Troubleshooting

First record **the access URL, platform version, time of the error, cluster type and the exact error**. Remove tokens, passwords, kubeconfigs and private keys before you share logs.

## Symptom table

| Symptom | Check first |
| --- | --- |
| The page does not open | Host TCP 12443, security group or firewall, Docker and Edge status, certificate and domain |
| The documentation site is unreachable | Separately deployed service; check its container status and reverse proxy configuration; unrelated to the control plane installation |
| 502 | Edge backend targets, health of the active color API and frontend, container logs; distinguish a 502 returned by an external proxy |
| `component_target_unavailable` | Whether the active manifest is complete and the component for that cluster type exists; **do not run the error text as a shell script** |
| Cluster offline | Agent pods and logs, WSS address and certificate, network egress, registration credentials and the platform record |
| Image pull failure | The full address in the manifest, node-to-registry network, registry authentication, CA; do not substitute a deprecated registry |
| The Turnstile widget succeeds but sign-in fails | Backend network to Cloudflare, site and secret configuration, server-side validation error |
| Backup or restore fails | Task details, Agent/Velero/OADP logs, object storage permissions, PVC/StorageClass and resource mapping |
| The UI upgrade waits forever | `hypercdr-upgrade-runner.service` status and logs, network access to Releases and the target manifest |

## Collecting logs

```bash
cd /var/lib/hypercdr
sudo docker compose --env-file .env -f docker-compose.yaml ps
sudo docker compose --env-file .env -f docker-compose.yaml logs --tail 100
sudo journalctl -u hypercdr-upgrade-runner.service -n 100 --no-pager
```

Append a service name as needed to limit the output to Edge, the API or a specific container.

## Common misdiagnoses

::: warning `/healthz` covers only part of the check

Deployment logs, health checks, a real sign-in and cluster tasks each verify a different stage. When `/healthz` succeeds, the registration components and storage may still be unavailable; `/readyz` currently does not show that the database is fully available either.

:::

::: warning Identify the 502 source first

An external proxy and Edge return a 502 for different reasons. Read the response headers first, then decide which layer to inspect.

:::

::: warning Do not run error codes as commands

Identifiers such as `component_target_unavailable` are error codes and cannot be run as a script. Running error message text as a command is a common deployment incident.

:::

## Order of investigation

```text
1. Does the page load        → network, port, certificate
2. Can you sign in           → auth config, captcha, database
3. Is the cluster online     → agent, WSS, credentials
4. Do tasks run              → storage, PVC, resource mapping
5. Does recovery work        → drill, cache preflight, target mapping
```

Each item depends on the ones before it. Skipping ahead and checking later items first usually lengthens the investigation.

## Filing an issue

Include: platform version, cluster type and version, reproduction steps, redacted error information and relevant log excerpts. **Do not attach kubeconfigs, tokens or private keys.**

## Related

- [Platform operations and upgrades](/en/docs/operations/platform)
- [Diagnostic logs](/en/docs/operations/logging): trace a failure by Task ID or Request ID
- [Velero restore cache](/en/docs/operations/restore-cache)
- [Tasks and restore points](/en/docs/manual/tasks)
