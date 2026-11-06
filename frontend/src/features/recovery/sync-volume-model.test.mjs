import assert from 'node:assert/strict';
import test from 'node:test';
import { syncVolumeSummary } from './sync-volume-model.ts';

const task = { id: 'sync', createdAt: '2026-11-05T02:00:00Z', payload: {} };
const event = (items, at = '2026-11-05T02:00:01Z') => ({ taskId: 'sync', createdAt: at, payload: { velero: { volumeProgress: { bytesDone: items.reduce((sum, item) => sum + (item.bytesDone || 0), 0), items } } } });
const items = [
  { name: 'a', phase: 'InProgress', knownTotal: true, bytesDone: 1024, totalBytes: 2048 },
  { name: 'b', phase: 'InProgress', knownTotal: true, bytesDone: 512, totalBytes: 1024 },
  { name: 'c', phase: 'Completed', bytesDone: 128 },
  { name: 'd', phase: 'New', knownTotal: true, totalBytes: 500 },
];

test('parallel volumes count actual starts, not queued objects or known totals', () => {
  const summary = syncVolumeSummary(task, [event(items)]);
  assert.equal(summary.started, 3);
  assert.equal(summary.syncing, 2);
  assert.equal(summary.waiting, 1);
  assert.equal(summary.completed, 1);
  assert.equal(summary.bytesDone, 1664);
  assert.equal(summary.volumes[0].percent, 50);
});

test('a later volume enters scanning without resetting existing volume progress', () => {
  const next = [...items.slice(0, 3), { ...items[3], phase: 'InProgress', knownTotal: false }];
  const summary = syncVolumeSummary(task, [event(items), event(next, '2026-11-05T02:00:02Z')]);
  assert.equal(summary.started, 4);
  assert.equal(summary.scanning, 1);
  assert.equal(summary.volumes[0].percent, 50);
  assert.equal(summary.volumes[3].percent, undefined);
});

test('estimated volume totals can change without inventing overall progress', () => {
  const summary = syncVolumeSummary(task, [event([{ ...items[0], totalBytes: 4096 }])]);
  assert.equal(summary.volumes[0].percent, 25);
  assert.equal(summary.percent, undefined);
});

test('persisted final progress survives a stage change and late older events', () => {
  const persisted = { ...task, payload: { lastStatusAt: '2026-11-05T02:00:03Z', volumeProgress: { bytesDone: 4096, items: [{ name: 'a', phase: 'Completed', bytesDone: 4096 }] } } };
  assert.equal(syncVolumeSummary(persisted, [event(items)]).bytesDone, 4096);
  assert.equal(syncVolumeSummary(persisted, [event(items)]).completed, 1);
});

test('old agents without items do not fabricate volume counts or percentages', () => {
  const summary = syncVolumeSummary({ ...task, payload: { volumeProgress: { bytesDone: 99, totalBytes: 100, knownTotal: true } } }, []);
  assert.equal(summary.bytesDone, 99);
  assert.deepEqual(summary.volumes, []);
});

test('empty phase stays waiting; a start timestamp identifies scanning', () => {
  const summary = syncVolumeSummary(task, [event([{ name: 'a' }, { name: 'b', startedAt: '2026-11-05T02:00:00Z' }])]);
  assert.equal(summary.waiting, 1);
  assert.equal(summary.scanning, 1);
  assert.equal(summary.started, 1);
});

test('foreign tasks and invalid numbers do not contaminate statistics', () => {
  const summary = syncVolumeSummary(task, [{ ...event(items), taskId: 'other' }, event([{ name: 'x', bytesDone: Infinity, totalBytes: -1 }])]);
  assert.equal(summary.bytesDone, 0);
  assert.equal(summary.volumes[0].bytesDone, 0);
});

test('queued volumes failing validation are not counted as started', () => {
  const summary = syncVolumeSummary(task, [event([{ name: 'a', phase: 'FailedValidation' }])]);
  assert.equal(summary.failed, 1);
  assert.equal(summary.started, 0);
});

test('terminal tasks retain recorded volume statistics when snapshot progress is cleared', () => {
  const completed = { ...task, status: 'succeeded', payload: { lastStatusAt: '2026-11-05T02:00:05Z', volumeProgress: null } };
  const summary = syncVolumeSummary(completed, [event([{ name: 'a', phase: 'Completed', bytesDone: 2048 }])]);
  assert.equal(summary.bytesDone, 2048);
  assert.equal(summary.completed, 1);
});

test('aggregate progress requires an explicit discovery marker and all volume sizes', () => {
  const known = [{ name: 'a', phase: 'InProgress', knownTotal: true, bytesDone: 50, totalBytes: 100 }];
  const before = event(known);
  assert.equal(syncVolumeSummary(task, [before]).percent, undefined);
  const after = event(known, '2026-11-05T02:00:02Z');
  Object.assign(after.payload.velero.volumeProgress, { discoveryComplete: true, discoveryCount: 1 });
  assert.equal(syncVolumeSummary(task, [before, after]).percent, 50);
  const missing = event([{ ...known[0], knownTotal: false }], '2026-11-05T02:00:03Z');
  Object.assign(missing.payload.velero.volumeProgress, { discoveryComplete: true, discoveryCount: 1 });
  const summary = syncVolumeSummary(task, [before, after, missing]);
  assert.equal(summary.progressStarted, true);
  assert.equal(summary.percent, undefined);
  assert.equal(summary.discoveryComplete, true);
});

test('a count mismatch never claims discovery complete', () => {
  const sample = event(items);
  Object.assign(sample.payload.velero.volumeProgress, { discoveryComplete: true, discoveryCount: 5 });
  assert.equal(syncVolumeSummary(task, [sample]).discoveryComplete, false);
});

test('a newly observed volume invalidates an older discovery scope', () => {
  const first = event([{ name: 'a', phase: 'InProgress', knownTotal: true, bytesDone: 50, totalBytes: 100 }]);
  Object.assign(first.payload.velero.volumeProgress, { discoveryComplete: true, discoveryCount: 1 });
  const later = event([...first.payload.velero.volumeProgress.items, { name: 'b', phase: 'InProgress', knownTotal: false }], '2026-11-05T02:00:02Z');
  const summary = syncVolumeSummary(task, [first, later]);
  assert.equal(summary.discoveryComplete, false);
  assert.equal(summary.progressStarted, false);
  assert.equal(summary.percent, undefined);
  assert.equal(summary.discoveryCompletedAt, '');
});

test('unknown size blocks overall percentage even after discovery completes', () => {
  const sample = event([{ name: 'a', phase: 'InProgress', bytesDone: 10, knownTotal: false }]);
  Object.assign(sample.payload.velero.volumeProgress, { discoveryComplete: true, discoveryCount: 1 });
  const summary = syncVolumeSummary(task, [sample]);
  assert.equal(summary.discoveryComplete, true);
  assert.equal(summary.percent, undefined);
  assert.equal(summary.progressStarted, false);
});

test('volume details distinguish known totals from unknown estimates', () => {
  const summary = syncVolumeSummary(task, [event([
    { name: 'a', phase: 'InProgress', bytesDone: 40, totalBytes: 100, knownTotal: true },
    { name: 'b', phase: 'InProgress', bytesDone: 10, totalBytes: 50, knownTotal: false },
  ])]);
  assert.equal(summary.volumes[0].knownTotal, true);
  assert.equal(summary.volumes[0].bytesDone, 40);
  assert.equal(summary.volumes[0].totalBytes, 100);
  assert.equal(summary.volumes[1].knownTotal, false);
  assert.equal(summary.percent, undefined);
});

test('timeline timestamps come from first matching persisted observations, not the latest poll', () => {
  const early = event([{ name: 'a', phase: 'InProgress', bytesDone: 10, totalBytes: 100, knownTotal: true }]);
  const discovered = event(early.payload.velero.volumeProgress.items, '2026-11-05T02:00:02Z');
  Object.assign(discovered.payload.velero.volumeProgress, { discoveryComplete: true, discoveryCount: 1 });
  const done = event([{ name: 'a', phase: 'Completed', bytesDone: 100, totalBytes: 100, knownTotal: true }], '2026-11-05T02:00:03Z');
  const summary = syncVolumeSummary(task, [done, discovered, early]);
  assert.equal(summary.discoveryStartedAt, '2026-11-05T02:00:01.000Z');
  assert.equal(summary.discoveryCompletedAt, '2026-11-05T02:00:02.000Z');
  assert.equal(summary.transferStartedAt, '2026-11-05T02:00:01.000Z');
  assert.equal(summary.transferCompletedAt, '2026-11-05T02:00:03.000Z');
});
