import assert from 'node:assert/strict';
import test from 'node:test';
import { confirmedCacheObservation } from './cache-settings-state.ts';

const settings = { rootPath: '/cache', indexPath: '/cache/index', drillPath: '/cache/drill', indexEnabled: true, indexMaxGiB: 5, drillEnabled: true, drillMaxGiB: 62 };
const observed = { ...settings, phase: 'applying', observedAt: '2026-11-11T08:00:00Z' };
const task = { status: 'succeeded', completedAt: '2026-11-11T08:00:05Z', payload: settings };

test('reopening either entry uses confirmed configuration ahead of delayed heartbeat', () => {
  assert.equal(confirmedCacheObservation(settings, observed, task).phase, 'applied');
  assert.equal(confirmedCacheObservation(settings, { ...observed, drillMaxGiB: 60 }, task).drillMaxGiB, 62);
});

test('does not mask offline state, active tasks, changed settings or later observations', () => {
  assert.equal(confirmedCacheObservation(settings, null, task), null);
  for (const status of ['running', 'failed']) {
    assert.equal(confirmedCacheObservation(settings, observed, { ...task, status }), observed);
  }
  assert.equal(confirmedCacheObservation({ ...settings, drillMaxGiB: 63 }, observed, task), observed);
  const newer = { ...observed, observedAt: '2026-11-11T08:00:10Z' };
  assert.equal(confirmedCacheObservation(settings, newer, task), newer);
});
