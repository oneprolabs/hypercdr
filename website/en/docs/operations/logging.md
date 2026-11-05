---
title: Diagnostic logs
description: Search platform and cluster diagnostic logs in the control plane, trace failed requests, tasks and components, and export the result for support.
---

# Diagnostic logs

Entry point: **Operations → Diagnostic Logs**.

The platform keeps its own structured diagnostic index for safe querying in the UI. It does not replace raw container logs — Docker and Kubernetes own those and rotate them. The diagnostic index answers a different question: which step failed, and what the earliest error was.

## Two log sources

| Source | What it covers |
| --- | --- |
| Platform | The platform's own API requests, task lifecycle, and the upgrade executor |
| Cluster | comm-agent, Velero and node-agent inside managed clusters |

Cluster logs are **collected on demand**: the platform fetches them over the authenticated agent connection, and the browser can never name an arbitrary namespace, pod or container. That is why you must pick a cluster and a component before searching.

## Filters

| Filter | Meaning |
| --- | --- |
| Log Scope | Tenant and system, Tenant logs or System logs; the latter two are system-administrator only |
| Tenant | A system administrator can pick all tenants or one; a normal account is fixed to its own tenant |
| Cluster | Required when the source is Cluster |
| Level | Info, Warning, Error, Debug |
| Component | Platform side: Platform API, Platform Upgrader, Task Lifecycle. Cluster side: comm-agent, Velero, node-agent |
| From / To | The time range; every query must be bounded |
| Search | Matches message content, Task ID and Request ID |

## Searching

1. Choose the log source.
2. Set the tenant, cluster, level, component and time range as needed.
3. Type a keyword in the search box, then press Enter or select **Search**.
4. Select a row to open **Log Details** and read the Correlation and Diagnostic Context sections.

Correlation lists Level, Operation, Status, Task ID, Command ID, Request ID and Error Code. When you report a problem, include both the Task ID and the Request ID so the other side can trace the same path.

## Retention and limits

- The structured diagnostic index is kept for **14 days** by default;
- A single query returns at most **5,000 records**. If results look truncated, narrow the time range rather than assuming there is no data;
- Raw Docker logs rotate per service (50 MB per file, five files per service). Kubernetes raw-log retention belongs to the kubelet and the container runtime.

## Access boundaries

::: warning Tenants cannot see each other's logs

A normal account can only query its own tenant and its own clusters, and exports are bound by the same rule. System logs carry no tenant ID and are visible only to system administrators.

:::

## What is never recorded

Passwords, bearer tokens, agent credentials, object-storage secrets, SMTP passwords and Kubernetes Secret content are never stored, and diagnostic payloads are redacted recursively. That does not make any log safe to paste: addresses, cluster names and business object names still appear in messages, so review before sharing externally.

## When to use it

- A task failed and the task details gave only a one-line summary;
- You need to confirm whether a request actually reached the platform — look it up by Request ID;
- A cluster-side component is misbehaving and you need the exact words from comm-agent, Velero or node-agent;
- You need to export a window of errors for support.

## Related

- [Troubleshooting](/en/docs/operations/troubleshooting): find the problem from its symptom
- [Tasks and restore points](/en/docs/manual/tasks): task states and failure types
- [Platform operations and upgrades](/en/docs/operations/platform): upgrading and maintaining the control plane itself
