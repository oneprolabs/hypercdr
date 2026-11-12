import assert from 'node:assert/strict';
import test from 'node:test';
import { protectionCleanupBlockingTasks, forceAbortableTasks } from './protection-cleanup-guard.ts';

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
