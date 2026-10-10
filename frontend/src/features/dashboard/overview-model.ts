import type { AppItem, Cluster } from '../clusters/types.ts';
import type { ApiApplication, ApiPolicy, ApiProtectionPlan, ApiRestorePointView, ApiTask, StorageRepo } from '../recovery/types.ts';
import { selectPointedPlanTask } from '../recovery/plan-task-selection.ts';

export const activeTask = (status: string) => ['queued', 'dispatched', 'accepted', 'running', 'canceling'].includes(status);
export const succeededTask = (status: string) => ['succeeded', 'completed'].includes(status);
export const failedTask = (status: string) => ['failed', 'error', 'timeout', 'timed_out'].includes(status);
export const canceledTask = (status: string) => ['canceled', 'cancelled'].includes(status);
export const timestamp = (value?: string) => {
  const parsed = Date.parse(value || '');
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
};
export const taskTime = (task: ApiTask) => timestamp(task.completedAt) ? task.completedAt! : task.createdAt || '';
const planIdOf = (task: ApiTask) => task.protectionPlanId || String(task.payload?.protectionPlanId || '');
const liveTask = (task: ApiTask) => !task.payload?.archivedClusterId && !task.payload?.archivedAppId && !task.payload?.archivedProtectionPlanId;
const planAppIds = (plan: ApiProtectionPlan) => new Set([plan.appId, ...(plan.appIds || [])].filter(Boolean));

export function planSourceId(plan: ApiProtectionPlan, applications: ApiApplication[]): string | undefined {
  return plan.sourceClusterId || applications.find(app => planAppIds(plan).has(app.id))?.clusterId;
}
export function planMatchesApp(plan: ApiProtectionPlan, app: AppItem): boolean {
  return Boolean(app.apiId && planAppIds(plan).has(app.apiId)) || app.protectionPlanId === plan.id;
}
export function pointMatchesApp(point: ApiRestorePointView, app: AppItem, plan?: ApiProtectionPlan): boolean {
  if (point.sourceClusterId !== app.clusterId) return false;
  if (point.protectionPlanId && plan && point.protectionPlanId !== plan.id) return false;
  if (point.includedNamespaces?.length) return point.includedNamespaces.includes(app.namespace || app.name);
  if (point.sourceNamespace) return point.sourceNamespace === (app.namespace || app.name);
  if (point.appId) return point.appId === app.apiId;
  return Boolean(plan && point.protectionPlanId === plan.id && planAppIds(plan).size === 1);
}
export const availablePoint = (point: ApiRestorePointView, now: number) => point.status === 'available' && (!timestamp(point.expiresAt) || timestamp(point.expiresAt) > now);

export type Freshness = 'meeting' | 'risk' | 'initial' | 'manual' | 'paused' | 'unknown';
// Platform schedules are evaluated in UTC, independently of the display timezone.
// Do not call daily/weekly/monthly policies an RPO failure merely because they
// are not interval policies. This measures schedule freshness, not a separate SLA.
function latestCalendarDue(policy: ApiPolicy, now: number): number | null {
  const current = new Date(now);
  const hour = Math.max(0, Math.min(23, policy.hour || 0));
  const minute = Math.max(0, Math.min(59, policy.minute || 0));
  const due = new Date(Date.UTC(current.getUTCFullYear(), current.getUTCMonth(), current.getUTCDate(), hour, minute));
  if (policy.scheduleType === 'daily') {
    if (due.getTime() > now) due.setUTCDate(due.getUTCDate() - 1);
  } else if (policy.scheduleType === 'weekly') {
    const weekday = Math.max(0, Math.min(6, policy.weekDay || 0));
    due.setUTCDate(due.getUTCDate() - ((due.getUTCDay() - weekday + 7) % 7));
    if (due.getTime() > now) due.setUTCDate(due.getUTCDate() - 7);
  } else if (policy.scheduleType === 'monthly') {
    const day = Math.max(1, Math.min(31, policy.monthDay || 1));
    const monthly = (month: number) => {
      const last = new Date(Date.UTC(current.getUTCFullYear(), month + 1, 0)).getUTCDate();
      return Date.UTC(current.getUTCFullYear(), month, Math.min(day, last), hour, minute);
    };
    return monthly(current.getUTCMonth()) <= now ? monthly(current.getUTCMonth()) : monthly(current.getUTCMonth() - 1);
  } else return null;
  return due.getTime();
}
export function planFreshness(plan: ApiProtectionPlan, policy: ApiPolicy | undefined, latestPoint: ApiRestorePointView | undefined, now: number): Freshness {
  if (!policy) return 'unknown';
  if (policy.composition === 'manual' || policy.composition === 'retention' || policy.scheduleType === 'manual') return 'manual';
  if (plan.scheduleEnabled === false || ['paused', 'disabled'].includes(plan.status || '') || policy.status === 'disabled') return 'paused';
  if (!latestPoint) return 'initial';
  const latest = timestamp(latestPoint.time);
  if (!latest || latest > now) return 'unknown';
  if (policy.scheduleType === 'interval') {
    if (!policy.intervalValue || policy.intervalValue <= 0) return 'unknown';
    const unit = (policy.intervalUnit || 'hours').toLowerCase();
    const multiplier = unit.startsWith('minute') ? 60000 : unit.startsWith('hour') ? 3600000 : null;
    return multiplier ? now - latest <= policy.intervalValue * multiplier ? 'meeting' : 'risk' : 'unknown';
  }
  const due = latestCalendarDue(policy, now);
  if (due === null) return 'unknown';
  if (timestamp(plan.createdAt) > due) return 'meeting';
  return latest >= due ? 'meeting' : 'risk';
}

export function taskCounts(tasks: ApiTask[]) {
  return {
    total: tasks.length,
    succeeded: tasks.filter(task => succeededTask(task.status)).length,
    failed: tasks.filter(task => failedTask(task.status)).length,
    running: tasks.filter(task => activeTask(task.status)).length,
    canceled: tasks.filter(task => canceledTask(task.status)).length,
    unknown: tasks.filter(task => !succeededTask(task.status) && !failedTask(task.status) && !activeTask(task.status) && !canceledTask(task.status)).length,
  };
}
export function tasksInRange(tasks: ApiTask[], days: number, now: number) {
  return tasks.filter(task => {
    const created = timestamp(task.createdAt);
    return created >= now - days * 86400000 && created <= now;
  });
}

export function buildOverviewModel(input: {
  cluster: Cluster | null; clusters: Cluster[]; storage: StorageRepo[]; tasks: ApiTask[];
  restorePoints: ApiRestorePointView[]; policies: ApiPolicy[]; protectionPlans: ApiProtectionPlan[];
  applications: ApiApplication[]; now?: number;
}) {
  const { cluster, clusters, storage, tasks, restorePoints, policies, protectionPlans, applications } = input;
  const now = input.now ?? Date.now();
  const apps = cluster?.apps || [];
  const plans = cluster ? protectionPlans.filter(plan => planSourceId(plan, applications) === cluster.id) : [];
  const planIds = new Set(plans.map(plan => plan.id));
  const points = cluster ? restorePoints.filter(point => point.sourceClusterId === cluster.id && availablePoint(point, now)) : [];
  const scopedTasks = cluster ? tasks.filter(task => {
    if (!liveTask(task)) return false;
    const id = planIdOf(task);
    // Recovery executes on a target cluster. Plan/restore-point source identity
    // owns its dashboard scope; clusterId alone would credit the wrong cluster.
    if (id) {
      const plan = protectionPlans.find(item => item.id === id);
      if (plan) return planIds.has(id);
    }
    const pointId = task.restorePointId || String(task.payload?.restorePointId || '');
    if (pointId) {
      const point = restorePoints.find(item => item.id === pointId);
      if (point) return point.sourceClusterId === cluster.id;
    }
    return task.clusterId === cluster.id;
  }) : [];
  const planPoints = (plan: ApiProtectionPlan) => points.filter(point => {
    if (point.protectionPlanId) return point.protectionPlanId === plan.id;
    return apps.some(app => planMatchesApp(plan, app) && pointMatchesApp(point, app, plan));
  }).sort((a, b) => timestamp(b.time) - timestamp(a.time));
  const freshness = { meeting: 0, risk: 0, initial: 0, manual: 0, paused: 0, unknown: 0 };
  const planFreshnessMap = new Map(plans.map(plan => {
    const state = planFreshness(plan, policies.find(policy => policy.id === plan.policyId), planPoints(plan)[0], now);
    freshness[state] += 1;
    return [plan.id, state];
  }));
  const configured = apps.filter(app => plans.some(plan => planMatchesApp(plan, app)) || app.isProtected);
  const namespacesWithoutPoints = configured.filter(app => !points.some(point => {
    const matchingPlans = plans.filter(plan => planMatchesApp(plan, app));
    return matchingPlans.length ? matchingPlans.some(plan => pointMatchesApp(point, app, plan)) : pointMatchesApp(point, app);
  })).length;
  const riskNamespaces = configured.filter(app => plans.some(plan => planMatchesApp(plan, app) && planFreshnessMap.get(plan.id) === 'risk')).length;
  const latestBackup = { succeeded: 0, running: 0, failed: 0, notStarted: 0, canceled: 0, unknown: 0 };
  for (const plan of plans) {
    const candidates = scopedTasks.filter(task => task.type === 'backup' && planIdOf(task) === plan.id);
    const task = selectPointedPlanTask(candidates, plan.latestSyncTaskId);
    if (!task) latestBackup[plan.latestSyncTaskId ? 'unknown' : 'notStarted'] += 1;
    else if (succeededTask(task.status)) latestBackup.succeeded += 1;
    else if (activeTask(task.status)) latestBackup.running += 1;
    else if (failedTask(task.status)) latestBackup.failed += 1;
    else if (canceledTask(task.status)) latestBackup.canceled += 1;
    else latestBackup.unknown += 1;
  }
  const plansWithPoints = plans.filter(plan => planPoints(plan).length > 0).length;
  const storageIds = new Set(plans.map(plan => plan.storageRepoId).filter(Boolean));
  const relatedStorage = storage.filter(repo => storageIds.has(repo.id));
  const targetIds = [...new Set(plans.map(plan => plan.targetClusterId).filter((id): id is string => Boolean(id)))];
  const targets = clusters.filter(item => targetIds.includes(item.id));
  const relatedUnavailable = relatedStorage.filter(repo => repo.status !== 'connected').length + [...storageIds].filter(id => !storage.some(repo => repo.id === id)).length;
  const failedLatest = latestBackup.failed;
  const recoveryTasks = scopedTasks.filter(task => ['restore', 'drill', 'takeover'].includes(task.type));
  const pointTimes = points.map(point => timestamp(point.time)).filter(Boolean);
  return {
    apps, plans, points, tasks: scopedTasks, recoveryTasks, configured: configured.length,
    totalApps: apps.length, unconfigured: Math.max(apps.length - configured.length, 0),
    activeNamespaces: apps.filter(app => ['Active', 'Running', 'Protected'].includes(app.status)).length,
    coverage: apps.length ? Math.round(100 * configured.length / apps.length) : null,
    freshness, riskNamespaces, namespacesWithoutPoints, latestBackup, plansWithPoints,
    syncRate: plans.length ? Math.round(100 * plansWithPoints / plans.length) : null,
    relatedStorage, relatedUnavailable, targets, missingTargets: targetIds.filter(id => !clusters.some(item => item.id === id)).length,
    targetOffline: targets.filter(item => item.connectionStatus === 'offline').length,
    oldestPoint: pointTimes.length ? Math.min(...pointTimes) : null,
    failedLatest, now,
    platform: {
      registered: clusters.length, online: clusters.filter(item => item.connectionStatus === 'online').length,
      offline: clusters.filter(item => item.connectionStatus === 'offline').length,
      unknownClusters: clusters.filter(item => !['online', 'offline'].includes(item.connectionStatus || '')).length,
      plans: protectionPlans.length, repositories: storage.length,
      connectedStorage: storage.filter(repo => repo.status === 'connected').length,
      unavailableStorage: storage.filter(repo => repo.status === 'warning').length,
      unknownStorage: storage.filter(repo => repo.status === 'unknown').length,
    },
  };
}
