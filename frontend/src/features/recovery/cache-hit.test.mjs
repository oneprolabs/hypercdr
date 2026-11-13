import assert from 'node:assert/strict';
import test from 'node:test';
import { taskCacheHit } from './cache-hit-model.ts';

const stats = { version: 1, indexEnabled: true, dataEnabled: true, indexHitBytes: 30, indexReadBytes: 40, dataHitBytes: 700, dataReadBytes: 1000 };
const task = (type, values = [stats]) => ({ id: 'one', type, payload: { volumeProgress: { items: values.map(cacheStats => ({ cacheStats })) } } });
test('backup data reuse and drill logical-data rates use separate denominators', () => {
  assert.equal(taskCacheHit(task('backup')).value, 'Unavailable');
  assert.equal(taskCacheHit(task('drill')).value, '70.0%');
  assert.equal(taskCacheHit(task('drill', [stats, { ...stats, dataHitBytes: 100, dataReadBytes: 3000 }])).value, '20.0%');
});
test('disabled, mixed, zero, missing, and invalid statistics are not false percentages', () => {
  assert.equal(taskCacheHit(task('drill', [{ ...stats, dataEnabled: false }])).value, 'Disabled');
  assert.equal(taskCacheHit(task('drill', [stats, { ...stats, dataEnabled: false }])).value, 'Mixed');
  assert.equal(taskCacheHit(task('drill', [{ ...stats, dataHitBytes: 0, dataReadBytes: 0 }])).value, '0.0%');
  for (const values of [[null], [stats, null], [{ ...stats, dataHitBytes: 1001 }], [{ ...stats, version: 2 }], [{ ...stats, dataHitBytes: -1 }]]) assert.equal(taskCacheHit(task('drill', values)).value, 'Unavailable');
});
test('initial running tasks start at zero, historical missing statistics remain unavailable', () => {
  assert.equal(taskCacheHit({ id: 'one', type: 'drill', status: 'running' }).value, '0.0%');
  assert.equal(taskCacheHit({ id: 'one', type: 'drill', status: 'succeeded' }).value, 'Unavailable');
});
test('terminal empty observations retain the latest valid task statistics', () => {
  const events = [
    { taskId: 'one', createdAt: '2026-11-13T02:00:00Z', payload: { velero: task('drill').payload } },
    { taskId: 'one', createdAt: '2026-11-13T03:00:00Z', payload: { velero: { volumeProgress: { items: [] } } } },
  ];
  assert.equal(taskCacheHit({ id: 'one', type: 'drill', status: 'succeeded', payload: { volumeProgress: { items: [{ cacheStats: null }] }, lastStatusAt: '2026-11-13T04:00:00Z' } }, events).value, '70.0%');
});
test('backup uses persisted overall data reuse, independent of index cache', () => {
  const backup = { ...task('backup', [{ ...stats, indexEnabled: false }]), status: 'succeeded' };
  backup.payload.sizeMetricsV2 = { logical: { known: true, totalBytes: 1000 }, newData: { known: true, totalBytes: 100 }, reuse: { status: 'available', ratio: 0.998 } };
  assert.equal(taskCacheHit(backup).label, 'Data reuse');
  assert.equal(taskCacheHit(backup).value, '90.0%');
  backup.payload.sizeMetricsV2.newData.known = false;
  assert.equal(taskCacheHit(backup).value, '99.8%');
  backup.payload.sizeMetricsV2.reuse.status = 'unavailable';
  assert.equal(taskCacheHit(backup).value, 'Unavailable');
});
test('only matching task events contribute, newest observation wins', () => {
  const base = { id: 'one', type: 'drill' };
  const events = [
    { taskId: 'other', createdAt: '2026-11-13T03:00:00Z', payload: { velero: task('drill').payload } },
    { taskId: 'one', createdAt: '2026-11-13T02:00:00Z', payload: { velero: task('drill', [{ ...stats, dataHitBytes: 100 }]).payload } },
  ];
  assert.equal(taskCacheHit(base, events).value, '10.0%');
});
