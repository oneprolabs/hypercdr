import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';
import { protectionCleanupBlockingTasks, forceAbortableTasks, forceAbortCandidates, forceAbortBlockedReason } from './protection-cleanup-guard.ts';

const cleanupPage = readFileSync(new URL('../applications/application-dr-page.tsx', import.meta.url), 'utf8');
const pollingCode = cleanupPage.slice(cleanupPage.indexOf('    const pollCleanup = () => {'), cleanupPage.indexOf('    void refreshPlatformData();', cleanupPage.indexOf('    const pollCleanup = () => {')));

function cleanupPollingHarness(readPlans) {
  const timers = [];
  const messages = [];
  const generation = { current: 1 };
  const state = { stage: 'run' };
  const context = vm.createContext({
    window: { setTimeout: callback => timers.push(callback) },
    cleanupGeneration: 1, cleanupPollGeneration: generation,
    listProtectionPlans: readPlans, listItems: value => value,
    planIds: ['plan'], targetApps: [],
    setProtectionPlans() {}, setCleaningPlanIds() {}, setAppUiOverrides() {}, setSelectedConfigApps() {},
    cleanupSubmittingRef: { current: true },
    setStage: stage => { state.stage = stage; },
    refreshPlatformData: async () => {}, toast: message => messages.push(message),
  });
  vm.runInContext(pollingCode + '\npollCleanup();', context);
  return { timers, messages, generation, state };
}

test('cleanup tracking survives more than forty polls and transient request failures', async () => {
  let calls = 0;
  const harness = cleanupPollingHarness(async () => {
    calls += 1;
    if (calls === 2) throw new Error('temporary network failure');
    return calls < 45 ? [{ id: 'plan', status: 'cleanup_running' }] : [];
  });
  while (harness.timers.length && calls < 50) await harness.timers.shift()();
  assert.equal(calls, 45);
  assert.equal(harness.state.stage, 'config');
  assert.match(harness.messages.at(-1), /Cleanup completed/);
  assert.equal(harness.timers.length, 0);
});

test('cleanup polling stops on navigation and reports persisted failure', async () => {
  let calls = 0;
  const canceled = cleanupPollingHarness(async () => { calls += 1; return []; });
  canceled.generation.current += 1;
  await canceled.timers.shift()();
  assert.equal(calls, 0);
  const failed = cleanupPollingHarness(async () => [{ id: 'plan', status: 'cleanup_failed' }]);
  await failed.timers.shift()();
  assert.match(failed.messages.at(-1), /Cleanup failed/);
  assert.equal(failed.timers.length, 0);
});

test('any executing task in any selected plan blocks cleanup, not only the latest backup', () => {
  for (const type of ['backup', 'drill', 'restore', 'takeover', 'storage-sync', 'schedule-sync', 'retention-cleanup']) {
    for (const status of ['queued', 'dispatched', 'accepted', 'running', 'finalizing', 'canceling']) {
      const active = { id: 'active', protectionPlanId: 'plan-two', type, status };
      const completed = { id: 'completed', protectionPlanId: 'plan-two', type, status: 'succeeded' };
      assert.deepEqual(protectionCleanupBlockingTasks(['plan-one', 'plan-two'], [active, completed]), [active]);
      assert.deepEqual(protectionCleanupBlockingTasks(['plan-one'], [active]), []);
    }
  }
});

test('zero completion timestamps do not disable active tasks and blocked tasks remain visible', () => {
  for (const completedAt of [undefined, '', '0001-01-01T00:00:00Z', '0001-01-01T08:05:43+08:05']) {
    const task = { id: 'active', protectionPlanId: 'plan', type: 'backup', status: 'running', completedAt };
    assert.equal(forceAbortBlockedReason(task), null);
    assert.deepEqual(forceAbortableTasks(['plan'], [task]), [task]);
  }
  for (const patch of [{ status: 'canceling' }, { payload: { abortRequested: true } }, { payload: { quotaStage: 'data_ready' } }, { payload: { quotaStage: 'deleting_old_point' } }]) {
    const task = { id: 'active', protectionPlanId: 'plan', type: 'backup', status: 'running', ...patch };
    assert.ok(forceAbortBlockedReason(task));
    assert.deepEqual(forceAbortCandidates(['plan'], [task]), [task]);
    assert.deepEqual(forceAbortableTasks(['plan'], [task]), []);
  }
  assert.equal(forceAbortBlockedReason({ status: 'running', completedAt: '2026-11-13T01:00:00Z' }), 'Task has finished');
});

test('terminal tasks do not block and delayed snapshots do not re-enable cleanup', () => {
  const older = { id: 'task', protectionPlanId: 'plan', type: 'drill', status: 'running', payload: { lastStatusAt: '2026-11-05T01:00:00Z' } };
  const newer = { ...older, payload: { lastStatusAt: '2026-11-05T01:00:05Z' } };
  assert.deepEqual(protectionCleanupBlockingTasks(['plan'], [newer, older]), [newer]);
  assert.deepEqual(protectionCleanupBlockingTasks(['plan'], [older, { ...newer, status: 'succeeded' }]), []);
});

test('force abort targets current sync, drill and cleanup tasks but not completed or stopping tasks', () => {
  for (const type of ['backup', 'drill', 'restore', 'takeover', 'protection-cleanup', 'retention-cleanup', 'storage-sync', 'schedule-sync']) {
    const task = { id: 'task', protectionPlanId: 'plan', type, status: 'running' };
    assert.deepEqual(forceAbortableTasks(['plan'], [task]), [task]);
    assert.deepEqual(forceAbortableTasks(['other'], [task]), []);
    assert.deepEqual(forceAbortableTasks(['plan'], [{ ...task, status: 'failed' }]), []);
    assert.deepEqual(forceAbortableTasks(['plan'], [{ ...task, status: 'canceling' }]), []);
    assert.deepEqual(forceAbortableTasks(['plan'], [task, { ...task, status: 'failed', completedAt: '2026-11-12T04:00:00Z' }]), []);
  }
});
