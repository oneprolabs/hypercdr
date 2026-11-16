import test from 'node:test';
import assert from 'node:assert/strict';
import { mergeNamespaceResourceCategories } from './namespace-resource-summary.ts';

test('merged plan includes all four namespaces and preserves same-name PVCs without mutating inventory', () => {
  const members = ['nextcloud', 'redis', 'rustfs', 'wordpress'].map((namespace, index) => ({
    namespace,
    categories: [
      { key: 'workloads', label: 'Workloads', total: index + 1, items: [{ kind: 'Deployment', count: index + 1 }] },
      { key: 'storage', label: 'Storage', total: 1, items: [{ kind: 'PersistentVolumeClaim', count: 1, resources: [{ name: 'data', fields: { CAPACITY: '5Gi' } }] }] },
    ],
  }));
  const before = structuredClone(members);
  const categories = mergeNamespaceResourceCategories(members);
  assert.equal(categories.find(category => category.key === 'workloads').total, 10);
  const storage = categories.find(category => category.key === 'storage');
  assert.equal(storage.total, 4);
  assert.equal(storage.items[0].count, 4);
  assert.deepEqual(storage.items[0].resources.map(resource => resource.namespace), members.map(member => member.namespace));
  assert.deepEqual(members, before);
});

test('empty and single namespace inventories remain valid', () => {
  assert.deepEqual(mergeNamespaceResourceCategories([]), []);
  assert.equal(mergeNamespaceResourceCategories([{ namespace: 'demo', categories: [{ key: 'config', label: 'Config', total: 2 }] }])[0].total, 2);
});
