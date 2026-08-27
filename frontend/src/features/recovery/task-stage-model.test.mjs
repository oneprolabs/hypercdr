import assert from 'node:assert/strict';
import test from 'node:test';
import { groupTaskEventsByStage, keyTaskEvents, latestVisibleTaskEvent, reachedTaskStages } from './task-stage-model.ts';

const recoveryStages = [
  ['preparing_restore', 'Preparing Restore'],
  ['restoring_resources', 'Restoring Kubernetes Resources'],
  ['restoring_data', 'Restoring Persistent Data'],
  ['waiting_for_workloads', 'Waiting for Workloads'],
  ['application_validation', 'Validating Application'],
  ['finalizing_drill', 'Finalizing Drill'],
];

function task(status = 'running', overrides = {}) {
  return {
    id: 'task-current', type: 'drill', status, progress: 10, clusterId: 'target',
    payload: { recoveryStages: recoveryStages.map(([id, name], index) => ({ id, name, status: index === 0 ? 'succeeded' : index === 1 ? 'running' : 'pending' })) },
    ...overrides,
  };
}

function event(id, reason, createdAt, overrides = {}) {
  return { id, taskId: 'task-current', level: 'info', reason, message: reason, createdAt, ...overrides };
}

test('groups each recovery event once and keeps future stages not started', () => {
  const groups = groupTaskEventsByStage(task(), [
    event('1', 'storage_preflight_started', '2026-08-26T01:00:00Z'),
    event('2', 'accepted', '2026-08-26T01:00:01Z'),
    event('3', 'restore_progress', '2026-08-26T01:00:02Z'),
  ]);
  assert.equal(groups[0].status, 'completed');
  assert.equal(groups[2].status, 'in_progress');
  assert.equal(groups[3].status, 'not_started');
  assert.equal(groups[3].events.length, 0);
  assert.deepEqual(groups.flatMap(group => group.events.map(item => item.id)).sort(), ['1', '2', '3']);
  assert.equal(groups.filter(group => group.currentEventId).length, 1);
  assert.equal(groups[2].currentEventId, '3');
});

test('filters events from another task before selecting the current event', () => {
  const groups = groupTaskEventsByStage(task(), [
    event('mine', 'restore_progress', '2026-08-26T01:00:02Z'),
    event('other', 'application_ready', '2026-08-26T01:00:03Z', { taskId: 'task-other' }),
  ]);
  assert.equal(groups[2].currentEventId, 'mine');
  assert.equal(groups.flatMap(group => group.events).some(item => item.id === 'other'), false);
});

test('opens the stage containing a failed event and leaves later stages not started', () => {
  const failedStages = recoveryStages.map(([id, name], index) => ({ id, name, status: index < 3 ? 'succeeded' : index === 3 ? 'failed' : 'pending' }));
  const groups = groupTaskEventsByStage(task('failed', { payload: { recoveryStages: failedStages } }), [
    event('1', 'restore_progress', '2026-08-26T01:00:00Z'),
    event('2', 'application_readiness_failed', '2026-08-26T01:00:01Z', { level: 'error', payload: { errorCode: 'RESTORE_WORKLOAD_IMAGE_PULL_FAILED' } }),
  ]);
  assert.equal(groups[3].status, 'failed');
  assert.equal(groups[3].currentEventId, '2');
  assert.equal(groups[4].status, 'not_started');
});

test('classifies volume and Velero stall failures in persistent data restoration', () => {
  for (const reason of ['RESTORE_VOLUME_FILESYSTEM_READ_ONLY', 'RESTORE_VOLUME_DATA_PATH_FAILED', 'RESTORE_VOLUME_PROGRESS_STALLED', 'RESTORE_VELERO_STALLED']) {
    const failedStages = recoveryStages.map(([id, name], index) => ({ id, name, status: index < 2 ? 'succeeded' : index === 2 ? 'failed' : 'pending' }));
    const groups = groupTaskEventsByStage(task('failed', { payload: { recoveryStages: failedStages } }), [
      event('1', 'accepted', '2026-08-26T01:00:00Z'),
      event('2', reason, '2026-08-26T01:00:01Z', { level: 'error' }),
    ]);
    assert.equal(groups[0].status, 'completed', reason);
    assert.equal(groups[2].status, 'failed', reason);
    assert.equal(groups[2].currentEventId, '2', reason);
    assert.equal(groups[3].status, 'not_started', reason);
  }
});

test('collapses progress updates to the newest payload while preserving stable order', () => {
  const events = keyTaskEvents([
    event('2', 'restore_progress', '2026-08-26T01:00:02Z', { payload: { percent: 20 } }),
    event('1', 'restore_progress', '2026-08-26T01:00:01Z', { payload: { percent: 10 } }),
    event('3', 'application_readiness_check_started', '2026-08-26T01:00:03Z'),
  ]);
  assert.equal(events.length, 2);
  assert.equal(events[0].id, '1');
  assert.equal(events[0].payload.percent, 20);
  assert.equal(latestVisibleTaskEvent(events).id, '3');
});

test('derives compact sync stages from the same ordered event stream', () => {
  const syncTask = { id: 'task-current', type: 'backup', status: 'running', progress: 42, clusterId: 'source' };
  const groups = groupTaskEventsByStage(syncTask, [
    event('1', 'accepted', '2026-08-26T01:00:00Z'),
    event('2', 'backup_progress', '2026-08-26T01:00:01Z'),
  ]);
  assert.deepEqual(groups.map(group => group.status), ['completed', 'in_progress', 'not_started', 'not_started']);
  assert.equal(groups[1].currentEventId, '2');
});

test('marks sync finalization completed when persisted task outcome is successful', () => {
  const syncTask = { id: 'task-current', type: 'backup', status: 'succeeded', progress: 100, clusterId: 'source' };
  const groups = groupTaskEventsByStage(syncTask, [
    event('1', 'accepted', '2026-08-26T01:00:00Z'),
    event('2', 'backup_progress', '2026-08-26T01:00:01Z'),
    event('3', 'backup_completed', '2026-08-26T01:00:02Z'),
  ]);
  assert.deepEqual(groups.map(group => group.status), ['completed', 'completed', 'completed', 'completed']);
});

test('timeline reveals stages only after execution reaches them', () => {
  const groups = groupTaskEventsByStage(task(), [
    event('1', 'accepted', '2026-08-26T01:00:00Z'),
    event('2', 'restore_progress', '2026-08-26T01:00:01Z'),
  ]);
  const reached = reachedTaskStages(groups);
  assert.deepEqual(reached.map(group => group.id), ['preparing_restore', 'restoring_resources', 'restoring_data']);
  assert.equal(reached.some(group => group.status === 'not_started'), false);
});
