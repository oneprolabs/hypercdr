import assert from 'node:assert/strict';
import test from 'node:test';
import { recoveryDuration, recoveryNamespaces } from './recovery-detail-model.ts';

test('elapsed duration advances only for active tasks, terminal results remain fixed', () => {
  const task = { status: 'running', createdAt: '2026-11-13T00:00:00Z' };
  assert.equal(recoveryDuration(task, Date.parse('2026-11-13T01:02:03Z')), '1h 2m 3s');
  for (const status of ['succeeded', 'failed', 'cancelled']) {
    assert.equal(recoveryDuration({ ...task, status, completedAt: '2026-11-13T00:02:04Z' }, Date.parse('2026-11-14T00:00:00Z')), '2m 4s');
    assert.equal(recoveryDuration({ ...task, status }), '—');
  }
  assert.equal(recoveryDuration({ ...task, createdAt: 'invalid' }), '—');
  assert.equal(recoveryDuration({ ...task, status: 'failed', completedAt: '0001-01-01T00:00:00Z' }), '—');
});

test('namespace mappings distinguish source keys and target values without duplicates', () => {
  const task = { payload: { sourceNamespace: 'app', sourceNamespaces: ['app', 'db'], targetNamespace: 'app-drill', targetNamespaces: { app: 'app-drill', db: 'db-drill' } } };
  assert.deepEqual(recoveryNamespaces(task, 'source'), ['app', 'db']);
  assert.deepEqual(recoveryNamespaces(task, 'target'), ['app-drill', 'db-drill']);
  assert.deepEqual(recoveryNamespaces({ payload: { targetNamespaces: ['one', 'two'] } }, 'target'), ['one', 'two']);
  assert.deepEqual(recoveryNamespaces({}), []);
});
