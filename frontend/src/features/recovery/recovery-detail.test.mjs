import assert from 'node:assert/strict';
import test from 'node:test';
import { backupRestorePointChanges, recoveryDuration, recoveryNamespaces } from './recovery-detail-model.ts';
import { setUserTimeZone } from '../../lib/date-time.ts';

test('successful backups show created and only actually deleted restore points in the user timezone', () => {
  setUserTimeZone('UTC');
  const created = { id: 'new', taskCreatedAt: '2026-11-13T00:00:00Z' };
  const old = { id: 'old', taskCreatedAt: '2026-11-12T00:00:00Z' };
  const task = { type: 'backup', status: 'succeeded', payload: { restorePointChanges: { created, deleted: [] } } };
  assert.deepEqual(backupRestorePointChanges(task), [{ label: 'Created restore point', value: 'RP-2026-11-13 00:00:00' }]);
  task.payload.restorePointChanges.deleted = [old, old, { id: 'older' }];
  assert.deepEqual(backupRestorePointChanges(task).slice(1), [{ label: 'Deleted restore point', value: 'RP-2026-11-12 00:00:00' }, { label: 'Deleted restore point', value: 'older' }]);
  for (const status of ['running', 'finalizing', 'failed']) assert.deepEqual(backupRestorePointChanges({ ...task, status }), []);
  assert.deepEqual(backupRestorePointChanges({ ...task, type: 'drill' }), []);
  assert.deepEqual(backupRestorePointChanges({ type: 'backup', status: 'succeeded' }), []);
  setUserTimeZone('Asia/Shanghai');
  assert.equal(backupRestorePointChanges(task)[0].value, 'RP-2026-11-13 08:00:00');
});

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
