---
title: FAQ
description: Find the first place to look for agent offline, storage test failures, missing restore points, failed drills or takeovers, and undelivered email.
---

# FAQ

This page answers one question: **given this symptom, where do I look first**. The actual procedure lives on the linked page.

## Start from the symptom

| Symptom | Look here first | Details |
| --- | --- | --- |
| Agent offline | WSS egress from the cluster to the platform, certificate, token, agent pod, network policies | [Cluster registration and management](/en/docs/manual/clusters) |
| Storage test fails | Endpoint, DNS/TLS, bucket, region, credential permissions, cluster-side network | [Object storage](/en/docs/manual/storage) |
| No restore points | Whether the protection configuration is active, whether sync tasks succeeded, whether retention removed them | [Tasks and restore points](/en/docs/manual/tasks) |
| Drill fails | Isolated namespace, target permissions, PVC/CSI, capacity, resource conflicts | [DR drills](/en/docs/manual/drill) |
| Takeover or recovery fails | The first failing stage, then target RBAC, Velero/OADP and object storage | [Formal takeover](/en/docs/manual/takeover) |
| Email is not delivered | SMTP configuration, test status, whether the sender address is authorised | [Email service](/en/docs/manual/email-service) |
| Cannot sign in | Captcha mode, certificate, account state and organization membership | [Sign-in and personal security](/en/docs/user-guide/users-and-organizations#personal-security) |
| The platform does not come up | Service status, `/readyz`, container logs, disk space | [Installation in detail](/en/docs/deployment/install) |
| Cannot find logs from a time window | Time range, tenant and log-source filters | [Diagnostic logs](/en/docs/operations/logging) |

## The general order

1. **Check the task status and the first error event.** Task details usually already say whether the problem is storage, PVC/StorageClass or resource mapping.
2. **Then trace the Task ID or Request ID in diagnostic logs.** This confirms whether the request reached the platform and which component failed.
3. **Read raw component logs last.** Starting with comm-agent or Velero output usually buries the cause in unrelated noise.

Skipping the first two steps normally just costs time.

## Common misreadings

| What you see | What it actually means |
| --- | --- |
| Task status success | The platform-side flow finished. Whether the business recovers needs a drill |
| Configuration saved | It reached the database. Whether it works needs a real backup and recovery |
| `/readyz` returns 200 | The control plane can serve requests. Object storage, agent registration and recovery tasks are still unverified |
| Restore point count stops growing | Tasks may be failing, or retention may have removed old points by quota |
| A task called Drill | The name does not guarantee isolation; that depends on the recovery mode and target you chose |
| An `accepted` event | The agent only received the task; recovery has not started |
| A `ContainersNotReady` event | Containers failed a readiness check at some point. If it later becomes Ready, that is a normal progression |
| Agent shows online | Only that the connection exists. Backup usability still depends on storage and tasks |
| Takeover task completed | The platform side finished. Access cutover and business acceptance are still yours |

## Next steps

If none of the above isolates the cause:

- Collect: task ID, time, cluster name, first error event and relevant screenshots;
- Export the relevant time window from [diagnostic logs](/en/docs/operations/logging), and attach a support bundle if needed;
- Before sharing, remove tokens, passwords, kubeconfig files, keys and internal addresses.

## Related

- [Diagnostic logs](/en/docs/operations/logging)
- [Troubleshooting](/en/docs/operations/troubleshooting)
- [Glossary](/en/docs/reference/glossary)
