import assert from 'node:assert/strict';
import test from 'node:test';
import { selectPointedPlanTask } from './plan-task-selection.ts';

test('stable pointer wins over misleading timestamps and statuses', () => {
  const pointed = { id: 'task-current', createdAt: '2026-08-27T01:00:00Z', status: 'running' };
  const future = { id: 'task-future', createdAt: '2036-08-27T01:00:00Z', status: 'failed' };
  assert.equal(selectPointedPlanTask([future, pointed], pointed.id), pointed);
});

test('missing pointed task does not fall back to another operation', () => {
  assert.equal(selectPointedPlanTask([{ id: 'task-old', createdAt: '2026-08-27T01:00:00Z' }], 'task-current'), undefined);
});

test('legacy plan without a pointer has deterministic compatibility fallback', () => {
  assert.equal(selectPointedPlanTask([
    { id: 'task-old', createdAt: '2026-08-27T01:00:00Z' },
    { id: 'task-new', createdAt: '2026-08-27T02:00:00Z' },
  ])?.id, 'task-new');
});
