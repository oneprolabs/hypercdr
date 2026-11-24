import test from 'node:test';
import assert from 'node:assert/strict';
import { clusterViewKey, readClusterView, saveClusterView, filterClusters } from './cluster-view.ts';

test('view preference defaults safely and persists list and cards', () => {
  const values = new Map();
  const storage = { getItem: key => values.get(key), setItem: (key, value) => values.set(key, value) };
  assert.equal(readClusterView(storage), 'cards');
  saveClusterView(storage, 'list');
  assert.equal(readClusterView(storage), 'list');
  saveClusterView(storage, 'cards');
  assert.equal(readClusterView(storage), 'cards');
  values.set(clusterViewKey, 'invalid');
  assert.equal(readClusterView(storage), 'cards');
  const denied = { getItem() { throw new Error('denied'); }, setItem() { throw new Error('denied'); } };
  assert.equal(readClusterView(denied), 'cards');
  assert.doesNotThrow(() => saveClusterView(denied, 'list'));
});

test('both views share name/id search and connection filters without mutation', () => {
  const rows = [{ id: 'abc-1', name: 'Source', connectionStatus: 'online' }, { id: 'def-2', name: 'Target' }, { id: 'ghi-3', name: 'Source West', connectionStatus: 'offline' }];
  assert.deepEqual(filterClusters(rows, ' SOURCE ', 'all'), [rows[0], rows[2]]);
  assert.deepEqual(filterClusters(rows, 'DEF', 'offline'), [rows[1]]);
  assert.deepEqual(filterClusters(rows, 'source', 'online'), [rows[0]]);
  assert.deepEqual(filterClusters(rows, 'missing', 'all'), []);
  assert.equal(rows.length, 3);
});
