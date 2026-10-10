import test from 'node:test';
import assert from 'node:assert/strict';
import { buildOverviewModel, planFreshness, tasksInRange, taskCounts } from './overview-model.ts';
const now = Date.parse('2026-10-10T06:00:00Z');
const at = hours => new Date(now - hours * 3600000).toISOString();
const cluster = (id, apps = []) => ({ id, name: id, connectionStatus: 'online', apps });
const app = (id, namespace, clusterId = 'source') => ({ apiId: id, namespace, name: namespace, clusterId, isProtected: false, status: 'Active' });
const policy = { id: 'policy', composition: 'schedule', scheduleType: 'interval', intervalValue: 1, intervalUnit: 'hours', status: 'active' };
const plan = { id: 'plan', appId: 'app', sourceClusterId: 'source', targetClusterId: 'target', policyId: 'policy', storageRepoId: 'related', scheduleEnabled: true, createdAt: at(48) };
const point = { id: 'point', protectionPlanId: 'plan', sourceClusterId: 'source', sourceNamespace: 'payments', status: 'available', time: at(.5) };
function input(overrides = {}) {
  const source = cluster('source', [app('app', 'payments')]);
  return { cluster: source, clusters: [source, cluster('target')], applications: [{ id: 'app', namespace: 'payments', clusterId: 'source' }], protectionPlans: [plan], policies: [policy], restorePoints: [point], tasks: [], storage: [{ id: 'related', status: 'connected' }, { id: 'other', status: 'warning' }], now, ...overrides };
}

test('selected cluster storage excludes unrelated failures while platform retains them', () => {
  const model = buildOverviewModel(input());
  assert.equal(model.relatedUnavailable, 0);
  assert.equal(model.platform.unavailableStorage, 1);
  assert.equal(model.configured, 1);
  assert.equal(model.coverage, 100);
});

test('target-executed recovery belongs to its source plan, not the execution cluster', () => {
  const recovery = { id: 'restore', protectionPlanId: 'plan', clusterId: 'target', type: 'restore', status: 'succeeded', createdAt: at(1) };
  const ownTargetPlan = { ...plan, id: 'other-plan', sourceClusterId: 'target', appId: 'target-app' };
  const data = input({ tasks: [recovery, { ...recovery, id: 'other-restore', protectionPlanId: 'other-plan', clusterId: 'source' }], protectionPlans: [plan, ownTargetPlan] });
  assert.deepEqual(buildOverviewModel(data).recoveryTasks.map(task => task.id), ['restore']);
  assert.deepEqual(buildOverviewModel({ ...data, cluster: data.clusters[1] }).recoveryTasks.map(task => task.id), ['other-restore']);
});

test('legacy recovery restore-point source and archived task flags are respected', () => {
  const task = { id: 'legacy', restorePointId: 'point', clusterId: 'target', type: 'drill', status: 'succeeded', createdAt: at(1) };
  const model = buildOverviewModel(input({ tasks: [task, { ...task, id: 'archived', payload: { archivedClusterId: 'source' } }] }));
  assert.deepEqual(model.tasks.map(task => task.id), ['legacy']);
});

test('merged namespaces count independently for coverage but once for sync and freshness plans', () => {
  const source = cluster('source', [app('app', 'payments'), app('other-app', 'orders')]);
  const merged = { ...plan, appIds: ['app', 'other-app'] };
  const oldPoint = { ...point, time: at(3), includedNamespaces: ['payments', 'orders'] };
  const model = buildOverviewModel(input({ cluster: source, protectionPlans: [merged], restorePoints: [oldPoint] }));
  assert.equal(model.configured, 2);
  assert.equal(model.riskNamespaces, 2);
  assert.equal(model.freshness.risk, 1);
  assert.equal(model.plansWithPoints, 1);
  assert.equal(model.syncRate, 100);
});

test('historical failures resolved by a newer successful backup are not active issues', () => {
  const old = { id: 'old', protectionPlanId: 'plan', clusterId: 'source', type: 'backup', status: 'failed', createdAt: at(2) };
  const latest = { ...old, id: 'latest', status: 'succeeded', createdAt: at(1) };
  const newer = { ...old, id: 'unpointed', createdAt: at(.5) };
  const model = buildOverviewModel(input({ protectionPlans: [{ ...plan, latestSyncTaskId: 'latest' }], tasks: [old, latest, newer] }));
  assert.equal(model.latestBackup.failed, 0);
  assert.equal(model.latestBackup.succeeded, 1);
  assert.equal(taskCounts(tasksInRange(model.tasks, 1, now)).failed, 2);
});

test('first backup/manual/paused/unknown are distinct from overdue', () => {
  assert.equal(planFreshness(plan, policy, undefined, now), 'initial');
  assert.equal(planFreshness(plan, { ...policy, composition: 'manual' }, undefined, now), 'manual');
  assert.equal(planFreshness({ ...plan, scheduleEnabled: false }, policy, point, now), 'paused');
  assert.equal(planFreshness(plan, undefined, point, now), 'unknown');
  assert.equal(planFreshness(plan, policy, { ...point, time: 'bad' }, now), 'unknown');
});

test('daily/weekly/monthly freshness follows UTC schedule including month-end clamp', () => {
  assert.equal(planFreshness(plan, { ...policy, scheduleType: 'daily', hour: 5 }, point, now), 'meeting');
  assert.equal(planFreshness(plan, { ...policy, scheduleType: 'daily', hour: 5 }, { ...point, time: at(2) }, now), 'risk');
  assert.equal(planFreshness(plan, { ...policy, scheduleType: 'weekly', weekDay: 6, hour: 5 }, point, now), 'meeting');
  const monthNow = Date.parse('2026-02-28T12:00:00Z');
  assert.equal(planFreshness({ ...plan, createdAt: '2026-01-01T00:00:00Z' }, { ...policy, scheduleType: 'monthly', monthDay: 31, hour: 10 }, { ...point, time: '2026-02-28T11:00:00Z' }, monthNow), 'meeting');
});

test('expired and cross-plan restore points do not satisfy current plan protection', () => {
  const model = buildOverviewModel(input({ restorePoints: [{ ...point, expiresAt: at(1) }, { ...point, id: 'different', protectionPlanId: 'different-plan' }] }));
  assert.equal(model.points.length, 1);
  assert.equal(model.plansWithPoints, 0);
  assert.equal(model.freshness.initial, 1);
});

test('time range includes boundaries, excludes invalid/future dates, never changes current coverage', () => {
  const tasks = [0, 24, 25, 169].map((hours, i) => ({ id: String(i), clusterId: 'source', type: 'backup', status: 'succeeded', createdAt: at(hours) }));
  tasks.push({ ...tasks[0], id: 'future', createdAt: at(-1) }, { ...tasks[0], id: 'invalid', createdAt: 'bad' });
  assert.equal(tasksInRange(tasks, 1, now).length, 2);
  assert.equal(tasksInRange(tasks, 7, now).length, 3);
  const model = buildOverviewModel(input({ tasks, restorePoints: [{ ...point, time: at(48) }] }));
  assert.equal(model.points.length, 1);
  assert.equal(model.coverage, 100);
});

test('latest plan states are exclusive, cancellation is not failure, missing pointer is unknown', () => {
  const states = ['succeeded', 'running', 'failed', 'canceled'];
  const plans = states.map((_, i) => ({ ...plan, id: 'plan-' + i, latestSyncTaskId: 'task-' + i }));
  plans.push({ ...plan, id: 'plan-missing', latestSyncTaskId: 'missing' }, { ...plan, id: 'plan-new' });
  const tasks = states.map((status, i) => ({ id: 'task-' + i, protectionPlanId: 'plan-' + i, clusterId: 'source', type: 'backup', status, createdAt: at(1) }));
  const model = buildOverviewModel(input({ protectionPlans: plans, tasks }));
  assert.deepEqual(model.latestBackup, { succeeded: 1, running: 1, failed: 1, canceled: 1, unknown: 1, notStarted: 1 });
  assert.equal(Object.values(model.latestBackup).reduce((a, b) => a + b, 0), plans.length);
});

test('protection gaps count configured namespaces missing their own available point', () => {
  const source = cluster('source', [app('app', 'payments'), app('other-app', 'orders'), app('unprotected', 'new')]);
  const merged = { ...plan, appIds: ['app', 'other-app'] };
  const data = input({ cluster: source, protectionPlans: [merged] });
  assert.equal(buildOverviewModel(data).namespacesWithoutPoints, 1);
  assert.equal(buildOverviewModel({ ...data, restorePoints: [{ ...point, includedNamespaces: ['payments', 'orders'] }] }).namespacesWithoutPoints, 0);
  assert.equal(buildOverviewModel({ ...data, restorePoints: [{ ...point, expiresAt: at(1) }] }).namespacesWithoutPoints, 2);
  assert.equal(buildOverviewModel({ ...data, restorePoints: [{ ...point, protectionPlanId: 'other' }] }).namespacesWithoutPoints, 2);
});
