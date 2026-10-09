import { listClusters } from '../api/clusters.ts';
import { listApplications, listTags } from '../api/applications.ts';
import { listPolicies } from '../api/policies.ts';
import { listStorageRepositories } from '../api/storage.ts';
import { listProtectionPlans } from '../api/protection-plans.ts';
import { listRestorePoints } from '../api/restore-points.ts';
import { getTask, listTasks } from '../api/tasks.ts';
import { listItems, type ApiTask, type ApiProtectionPlan } from '../features/recovery/types.ts';
import type { ApiCluster } from '../features/recovery/platform-types.ts';

type CurrentScope = () => boolean;
const activityTypes = ['backup', 'restore', 'drill', 'takeover', 'storage-sync', 'schedule-sync', 'protection-cleanup'];
const readActivityTasks = () => listTasks({ view: 'summary', types: activityTypes, limit: 500 });

async function completePlanTasks(tasks: ApiTask[], plans: ApiProtectionPlan[], isCurrent: CurrentScope) {
  if (!isCurrent()) return null;
  const present = new Set(tasks.map(task => task.id));
  const missing = Array.from(new Set(plans.flatMap(plan => [plan.latestSyncTaskId, plan.latestRecoveryTaskId]).filter((id): id is string => Boolean(id) && !present.has(id))));
  const fetched = await Promise.all(missing.map(id => getTask(id).catch(() => null)));
  if (!isCurrent()) return null;
  return [...tasks, ...fetched.filter((task): task is ApiTask => Boolean(task) && !present.has(task.id))];
}

// These loaders own dependency ordering and session checks. They return domain
// data; mapping and React state commits belong to usePlatformResources.
export async function loadPlatformSnapshot(isCurrent: CurrentScope, onClusters?: (clusters: ApiCluster[]) => void) {
  if (!isCurrent()) return null;
  const clusterRequest = listClusters();
  void clusterRequest.then(response => { if (isCurrent()) onClusters?.(listItems(response)); }).catch(() => undefined);
  const [clusterRes, appRes, storageRes, policyRes, planRes, taskRes, tagRes, pointRes] = await Promise.all([
    clusterRequest, listApplications(), listStorageRepositories(), listPolicies(), listProtectionPlans(), readActivityTasks(), listTags(), listRestorePoints(),
  ]);
  if (!isCurrent()) return null;
  const plans = listItems(planRes);
  const tasks = await completePlanTasks(listItems(taskRes), plans, isCurrent);
  if (tasks === null) return null;
  return {
    clusters: listItems(clusterRes), applications: listItems(appRes), storage: listItems(storageRes), policies: listItems(policyRes),
    plans, tasks, tags: listItems(tagRes), restorePoints: listItems(pointRes),
  };
}

export async function loadApplicationActivitySnapshot(isCurrent: CurrentScope) {
  if (!isCurrent()) return null;
  const [taskRes, pointRes, planRes] = await Promise.all([readActivityTasks(), listRestorePoints(), listProtectionPlans()]);
  if (!isCurrent()) return null;
  const plans = listItems(planRes);
  const tasks = await completePlanTasks(listItems(taskRes), plans, isCurrent);
  return tasks === null ? null : { tasks, plans, restorePoints: listItems(pointRes) };
}

export async function loadTopologySnapshot(isCurrent: CurrentScope) {
  if (!isCurrent()) return null;
  const [clusterRes, appRes, planRes] = await Promise.all([listClusters(), listApplications(true), listProtectionPlans()]);
  if (!isCurrent()) return null;
  return { clusters: listItems(clusterRes), applications: listItems(appRes), plans: listItems(planRes) };
}
