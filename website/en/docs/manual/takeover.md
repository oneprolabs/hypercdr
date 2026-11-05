---
title: Formal takeover
description: Read the readiness verdict first, then switch a protected application to the target cluster and complete the business validation.
---

# Formal takeover

Entry point: **DR → Restore Points → Takeover**. The readiness verdict is shown in the configuration under **DR → Application DR**.

A takeover restores a selected restore point to the target cluster. It is a real business switch, not a drill. Readiness assessment and change approval come first; access cutover and business validation come after.

::: danger A takeover rewrites the target environment

A takeover creates or overwrites resources in the target cluster. The wrong restore point or the wrong target can affect workloads already running there. Check every option in the confirmation window — do not probe by clicking through.

:::

## Drill versus takeover

| | Drill | Takeover |
| --- | --- | --- |
| Purpose | Prove a restore point is usable | Actually move the business to the target |
| Target environment | An isolated namespace | A production target cluster |
| Cost of failure | Only this drill; fix and re-run | The target has already changed; you need a rollback plan |

**Restore** in the task list is a record type only — the page has no standalone Restore creation button. The available entry points are Drill and Takeover, and they are not interchangeable: a successful drill is not a takeover record and does not replace one.

## 1. Start with readiness

Readiness answers one question: can this source cluster's restore point actually recover onto this target cluster. Backup completion and recoverability are separate states — a finished backup does not mean a recoverable one.

There are four verdicts:

| Verdict | Meaning | Can you proceed |
| --- | --- | --- |
| Ready | Every check passes | Yes |
| Ready with warnings | No deterministic blocker, but warnings or unknowns need judgement | Yes, after reviewing each warning |
| Blocked | At least one deterministic requirement is missing or incompatible | Rejected by default |
| Not assessed | The DR pair is unavailable, or no evidence has been collected | Rejected by default; run an assessment first |

### When evidence is collected

Capability scanning is not part of the periodic cluster inventory upload. It is triggered on demand:

- when you select a target cluster in the DR configuration;
- when the configuration is saved;
- when you select **Run assessment**;
- before a recovery workflow needs fresh evidence.

The platform keeps the latest scan together with its collection time for comparison and audit. Evidence goes stale — running **Run assessment** yourself is safer than trusting an old verdict.

### What the scan looks at

```text
API resources the target cluster serves (exact group / version / resource)
Custom resource types that actually exist in the protected namespace
StorageClasses and CSI drivers
IngressClass, RuntimeClass, PriorityClass
VolumeSnapshotClass and cert-manager ClusterIssuer, where those APIs exist
```

![Checks and evidence in the readiness assessment detail](/images/user-guide/recovery-readiness.png)

### Incomplete evidence is not a pass

This is the easiest thing to misread: **an empty list is data, not proof**.

If collection hit a permission error, a timeout, a discovery failure, or an outdated Agent, the evidence is incomplete. Incomplete evidence can produce at most **Ready with warnings** — it can prove neither compatibility nor incompatibility. An older Agent does not raise an error; missing capabilities show up as `Unknown`, and `Unknown` must be treated as "not checked", never as "checked and fine".

### Proceeding when the assessment failed

When deterministic blockers exist, Drill and Takeover are rejected by default. To accept the risk anyway, tick the explicit confirmation:

> I understand the identified risks and want to proceed anyway.

No reason text is required. The task payload and task events record the operator, the time, the readiness snapshot at that moment, the blocker count and `forceProceed=true`, so the decision stays auditable. Make sure your change approval already covers those risks before you tick it.

## 2. Choose the recovery mode

| Mode | As labelled on the page | What it means |
| --- | --- | --- |
| Clone to a new namespace | Clone to new namespace | Reuses nothing in the target, so conflicts are rare — but access entry points and external dependencies still need separate handling |
| Restore to the original namespace | Full restore to original namespace | Existing resources in the same-named namespace may be overwritten; an extra risk confirmation is required |

If you choose the original namespace, list what already exists there, assess the impact of overwriting it, and only then tick the risk confirmation.

![Restore point, target and original-namespace risk confirmation in the takeover window](/images/user-guide/takeover-options.png)

The screenshot shows part of an unsubmitted configuration window with **Full restore to original namespace** selected and the risk box not yet ticked. That state is not an executed takeover, and it is not a recommended configuration.

## 3. Check the resource scope

**Restore scope** covers namespace-scoped resources by default and does **not** include cluster-scoped resources. Do not read it as "restore the whole cluster".

You can switch between the default and a custom scope. Custom lists only the types that currently have objects in this namespace; see [Application disaster recovery](/en/docs/manual/application-dr#resource-scope) for the rules.

## 4. Check the environment mapping

When the source and target environments differ, expand **Advanced options** and go through each item. The same name does not mean the same underlying capability, and this is where takeovers most often go wrong.

![Service NodePort mapping in the takeover advanced options](/images/user-guide/takeover-nodeports.png)

| Mapping | What to check |
| --- | --- |
| StorageClass | Map the backed-up storage class to one that genuinely exists on the target; the same name does not mean the same backend |
| Workload image | Point a source image at a target image; leaving it blank keeps the backed-up value, and the target cluster must then be able to pull it |
| Service NodePort | Configured per service and port, valid range `30000–32767`; leaving it blank lets Kubernetes allocate a port — afterwards check firewalls, load balancers and access addresses |

Do not copy the `31080` from the screenshot, and do not assume an automatic allocation matches the source port.

## 5. Steps

1. Open **DR → Restore Points**, confirm the source cluster, namespace and restore point time, and select the restore point to take over.
2. Select **Takeover** to open **DR Takeover**. Check the restore point once more so you do not use the wrong application or timestamp.
3. Choose the target under **Target cluster**. Unavailable or incompatible clusters cannot serve as a valid target.
4. Choose the recovery mode: clone to a new namespace, or restore to the original namespace on the target.
5. Check the default or custom resource scope under **Restore scope**.
6. If the environments differ, expand **Advanced options** and check the StorageClass, image and NodePort mappings, plus the workload readiness and application validation options.
7. Re-check target, scope and data timestamp against the approved takeover plan, then select **Start Takeover**. **Cancel** or closing the window submits nothing.
8. Watch the takeover task under **DR → Backup & Recovery Tasks**, and check the recovery stages and final result in the application's **Recovery Status**.
9. Check workloads, PVCs, services and business data on the target, then cut over the access entry point and finish validation as your plan requires.

## 6. Confirming the result

A completed task only means the platform-side flow ended. A takeover succeeded when all of these hold:

- [ ] Target workloads, PVCs and services are ready
- [ ] Application health checks pass and the application serves traffic
- [ ] Business data matches the restore point timestamp
- [ ] Access entry points and external dependencies (DNS, load balancers, third-party services) were switched as planned
- [ ] Writes on the source side have stopped, or the boundary for running both sides is explicit

Record the restore point, target cluster version, start and end times, and any remaining alerts.

## 7. When it fails

- **Stop first, then investigate.** On a target conflict, permission error or data anomaly, stop further changes and preserve the state. Do not retry repeatedly against the same target.
- **Failure does not mean nothing was created.** An interrupted takeover may have created resources already. Compare what exists before retrying, and do not wipe the original business namespace.
- **Do not trust a percentage.** Until traffic has been cut over, do not declare the business recovered based on a progress number.

## Related

- [Application disaster recovery](/en/docs/manual/application-dr): resource scope and target cluster selection
- [DR drills](/en/docs/manual/drill): verify the same restore point in an isolated environment first
- [Tasks and restore points](/en/docs/manual/tasks): how task states map to restore points
