---
title: Dashboard
description: How the platform overview differs from the cluster overview, how to read the metrics, and what the dashboard cannot prove.
---

# Dashboard

The default page after signing in. It helps you spot **what is wrong** quickly, and does not replace the task list or the logs.

![Protection activity, task trends and cluster state in the platform overview](/images/overview/dashboard-149.png)

The screenshot is from the 149 environment; the numbers and cluster names are examples. Read the cluster selector and time range in the top-left corner first, then the individual metrics.

## Two views

The dashboard separates the **platform overview** from the **selected cluster overview**:

| View | Scope | Use |
| --- | --- | --- |
| Platform overview | All clusters and tasks in this organization | Overall protection coverage, recent failure trends |
| Cluster overview | The single cluster currently selected | This cluster's resources, tasks and protection state |

The cluster selector and the time range together determine the chart content. **Do not read every metric as a platform total**: switching the cluster changes the numbers.

## Reading the metrics

- **Read the trend before the absolute value.** A single value depends on cluster size; the trend shows whether protection and recovery are stable.
- **Failures carry more information than successes.** A growing success count may just mean denser scheduling, while failures usually point to a specific storage, permission or network problem.
- **Empty data does not mean healthy.** No tasks may mean the policy never took effect, or that tasks are failing.
- **Do not use sample data to interpret results.** The numbers on the page reflect real task records.

## Evidence limits

::: warning The dashboard is an aggregate view

The dashboard is an aggregate view and cannot replace checks on specific pages. Each item below must be confirmed on its own page:

- Storage writability: check [task details](/en/docs/manual/tasks).
- Recovery usability: run a [drill](/en/docs/manual/drill).
- Cluster health: check the cluster status and the agent logs. A successful `/healthz` does not mean the registration component works.

:::

## Common warning signs

| Symptom | Likely cause | Next step |
| --- | --- | --- |
| Cluster shows offline | Agent not running, WSS unreachable, certificate problem, registration credential expired | [Troubleshooting](/en/docs/operations/troubleshooting) |
| Tasks keep failing | Object storage permissions, PVC/StorageClass, resource mapping | Check the errors and logs in the task details |
| Policies exist but no tasks | Policy not bound to an application, schedule not due, protection configuration not enabled | Check [application DR protection](/en/docs/manual/application-dr) |
| Restore point count stops growing | Backups are failing, or the retention rule is removing old points | Compare with the retention rule in [protection policies](/en/docs/manual/policies) |
