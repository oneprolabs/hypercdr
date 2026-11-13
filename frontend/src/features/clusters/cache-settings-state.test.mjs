import assert from 'node:assert/strict';
import test from 'node:test';
import { cacheDirectoryConfirmed, cacheDisableConfirmed, confirmedCacheObservation } from './cache-settings-state.ts';

const settings = { rootPath: '/cache', indexPath: '/cache/index', drillPath: '/cache/drill', indexEnabled: true, indexMaxGiB: 5, drillEnabled: true, drillMaxGiB: 62 };
const observed = { ...settings, phase: 'applying', observedAt: '2026-11-11T08:00:00Z' };
const task = { status: 'succeeded', completedAt: '2026-11-11T08:00:05Z', payload: settings };

test('each enabled cache requires its own typed yes before disabling', () => {
  for (const answer of ['', 'y', 'no', 'confirm']) assert.equal(cacheDisableConfirmed(true, false, answer), false);
  assert.equal(cacheDisableConfirmed(true, false, 'yes'), true);
  assert.equal(cacheDisableConfirmed(true, false, ' YES '), true);
  assert.equal(cacheDisableConfirmed(false, false, ''), true);
  assert.equal(cacheDisableConfirmed(false, true, ''), true);
  assert.equal(cacheDisableConfirmed(true, true, ''), true);
  assert.equal(cacheDisableConfirmed(true, false, 'yes') && cacheDisableConfirmed(true, false, ''), false);
});

test('changing a directory needs fresh confirmation, unchanged directories do not', () => {
  assert.equal(cacheDirectoryConfirmed('/cache/index', '/cache/index', ''), true);
  assert.equal(cacheDirectoryConfirmed('/cache/index', '/new/index', ''), false);
  assert.equal(cacheDirectoryConfirmed('/cache/index', '/new/index', 'confirm'), false);
  assert.equal(cacheDirectoryConfirmed('/cache/index', '/new/index', 'yes'), true);
  assert.equal(cacheDirectoryConfirmed('/cache/drill', '/new/drill', ''), false);
});

test('reopening either entry uses confirmed configuration ahead of delayed heartbeat', () => {
  assert.equal(confirmedCacheObservation(settings, observed, task).phase, 'applied');
  assert.equal(confirmedCacheObservation(settings, { ...observed, drillMaxGiB: 60 }, task).drillMaxGiB, 62);
});

test('task success does not wait for heartbeat, while newer observations remain authoritative', () => {
  assert.equal(confirmedCacheObservation(settings, null, task).phase, 'applied');
  for (const status of ['running', 'failed']) {
    assert.equal(confirmedCacheObservation(settings, observed, { ...task, status }), observed);
  }
  assert.equal(confirmedCacheObservation({ ...settings, drillMaxGiB: 63 }, observed, task), observed);
  const newer = { ...observed, observedAt: '2026-11-11T08:00:10Z' };
  assert.equal(confirmedCacheObservation(settings, newer, task), newer);
});

test('new heartbeat can change enabled state after success without accepting delayed old state', () => {
  const disabled = { ...observed, phase: 'applied', drillEnabled: false };
  assert.equal(confirmedCacheObservation(settings, disabled, task).drillEnabled, true);
  const newer = { ...disabled, observedAt: '2026-11-11T08:00:10Z' };
  assert.equal(confirmedCacheObservation(settings, newer, task).drillEnabled, false);
  assert.equal(confirmedCacheObservation(settings, null, { ...task, status: 'running' }), null);
});
