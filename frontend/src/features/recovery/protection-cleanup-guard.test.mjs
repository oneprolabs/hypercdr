import assert from 'node:assert/strict';
import test from 'node:test';
import { protectionCleanupBlockingTasks, forceAbortableTasks, forceAbortCandidates, forceAbortBlockedReason } from './protection-cleanup-guard.ts';

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
