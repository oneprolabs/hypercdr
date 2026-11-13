import test from 'node:test';
import assert from 'node:assert/strict';
import { drillCleanupCandidates } from '../applications/drill-cleanup-model.ts';

const drill = (id, namespace, createdAt, extra = {}) => ({ id, type: 'drill', clusterId: 'target', protectionPlanId: 'plan', status: 'succeeded', createdAt, payload: { targetNamespace: namespace }, ...extra });
test('lists all uncleaned targets, merges reused namespaces and retains failed drills', () => {
  const tasks = [drill('old', 'app', '2026-01-01'), drill('new', 'app', '2026-01-02'), drill('other', 'other', '2026-01-03', { status: 'failed' })];
  assert.deepEqual(drillCleanupCandidates(['plan'], tasks).map(item => item.task.id), ['other', 'new']);
  assert.deepEqual(drillCleanupCandidates(['another'], tasks), []);
});
test('successful cleanup hides old resources but not later drills; active cleanup is blocked', () => {
  const old = drill('old', 'app', '2026-01-01');
  const cleanup = { id: 'cleanup', type: 'protection-cleanup', clusterId: 'target', createdAt: '2026-01-02', status: 'succeeded', payload: { cleanupMode: 'drill', drillTaskId: 'old', drillNamespaces: ['app'] } };
  assert.equal(drillCleanupCandidates(['plan'], [old, cleanup]).length, 0);
  assert.equal(drillCleanupCandidates(['plan'], [old, cleanup, drill('new', 'app', '2026-01-03')]).length, 1);
  assert.equal(drillCleanupCandidates(['plan'], [old, { ...cleanup, status: 'running' }])[0].blockedReason, 'Cleaning');
});
test('recovery sharing a target blocks cleanup and clusters remain independent', () => {
  const old = drill('old', 'app', '2026-01-01');
  const active = drill('running', 'app', '2026-01-03', { type: 'restore', status: 'running' });
  assert.equal(drillCleanupCandidates(['plan'], [old, active])[0].blockedReason, 'Recovery in progress');
  assert.equal(drillCleanupCandidates(['plan'], [old, { ...active, clusterId: 'other' }])[0].blockedReason, '');
  assert.equal(drillCleanupCandidates(['plan'], [old, { ...active, type: 'takeover', status: 'succeeded' }])[0].blockedReason, 'Replaced by a newer recovery');
});
test('partial namespace reuse or cleanup does not hide other remaining resources', () => {
  const older = drill('old', 'app', '2026-01-01', { payload: { targetNamespaces: { first: 'app', second: 'other' } } });
  const newer = drill('new', 'app', '2026-01-02');
  assert.deepEqual(drillCleanupCandidates(['plan'], [older, newer]).map(item => item.namespaces), [['app'], ['other']]);
  const cleanup = { id: 'cleanup', type: 'protection-cleanup', clusterId: 'target', createdAt: '2026-01-03', status: 'succeeded', payload: { cleanupMode: 'drill', drillTaskId: 'old', drillNamespaces: ['app'] } };
  assert.deepEqual(drillCleanupCandidates(['plan'], [older, cleanup]).map(item => item.namespaces), [['other']]);
});
