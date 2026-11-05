---
title: Protection policies
description: Define protection plans and retention rules to control backup frequency, how much data is retained and storage consumption.
---

# Protection policies

Entry point: **Configuration → Policies**.

A policy defines two things: **when backups run** and **how long restore points are kept**. A policy does not bind to a specific application; binding happens in [application DR protection](/en/docs/manual/application-dr), so one policy can be referenced by multiple applications.

## Parts of a policy

| Part | Purpose |
| --- | --- |
| Protection plan | The execution cadence of backups |
| Retention rule | How long, or how many restore points, are kept |
| Scope | The resources protected when the policy is applied |

You can create a policy on the Policies management page or in the protection wizard; both use the same creation UI.

## Fields in the creation window {#fields}

![The protection policy creation window](/images/user-guide/policy-new.png)

| Field | Meaning |
| --- | --- |
| Policy Name | Identifies and selects the policy; include the service tier, frequency or retention requirement |
| Schedule + Retention | Configures both the schedule and restore point retention — the combination the page recommends |
| Schedule Only | Defines the schedule only; do not assume a retention rule is set |
| Retention Only | Defines the restore point lifecycle only, and produces no scheduled backups by itself |
| Interval | Sets the interval in minutes or hours |
| Daily / Weekly / Monthly Backup | Sets a daily, weekly or monthly schedule; check the timezone shown on the page before saving |
| Restore Point Quota | The quota on retained restore points; every retained point counts toward it |
| Automatic Retention / Add Rule | Adds an optional automatic retention rule; without one, the quota replaces the oldest eligible restore point once full |

The 5 minutes and 7 restore points in the screenshot are the form's current values, not a recommendation. Choose parameters from your data change rate, storage capacity and recovery objective.

::: danger Configuring only half causes problems

**Schedule Only**: backups keep running with no cleanup rule, so storage grows without bound. **Retention Only**: there is a cleanup rule but no scheduled job, so no new restore points appear. Configure both.

:::

## Retention rules and capacity estimates

The retention rule directly determines storage consumption. Estimate capacity as follows:

```text
storage needed ≈ size of one backup × retained copies per application × number of applications × growth factor
```

Points to watch:

- The retained copy count is based on **restore points**, not days. With two backups a day, retaining 7 copies covers only three and a half days.
- When multiple applications share one storage, the capacity adds up.
- Drills and restores also consume restore cache temporarily (see [Velero restore cache](/en/docs/operations/restore-cache)), so leave headroom in capacity planning.

::: warning Cleanup of expired restore points is expected

A restore point count that stops growing does not always mean backups failed; the retention rule may be removing old restore points. Check the task records before drawing a conclusion.

:::

## Impact of changing a policy

- Shortening the retention period speeds up cleanup of old restore points; **deleted restore points cannot be recovered**.
- Increasing the backup frequency adds storage and network load; confirm capacity first.
- After you change a policy, existing restore points are handled under the new rule and are not rebuilt retroactively.

Record the current retention rule and restore point count before the change, then observe one or two cycles before adjusting further.

## Policies and applications

```text
policy A ──┬── application 1 (namespace: app-a)
           └── application 2 (namespace: app-b, app-c)

policy B ──── application 3 (namespace: app-d)
```

An application should avoid binding multiple policies to the same target at once; otherwise tasks overlap and both storage consumption and scheduling load increase.

## Checklist

- [ ] The retained copy count covers the recovery window the business requires
- [ ] Storage capacity is estimated for the worst case
- [ ] Policies are bound to the intended applications, with nothing missing or extra
- [ ] You have confirmed that old restore points can be deleted before changing the retention rule

## Related

- [Object storage](/en/docs/manual/storage)
- [Application DR protection](/en/docs/manual/application-dr)
- [Tasks and restore points](/en/docs/manual/tasks)
