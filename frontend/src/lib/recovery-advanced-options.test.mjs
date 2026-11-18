import test from 'node:test';
import assert from 'node:assert/strict';
import { recoveryAdvancedOptions, selectedAdvancedMappings } from './recovery-advanced-options.ts';

const contents = [
  { kind: 'Deployment', name: 'app', namespace: 'one', images: ['shared', 'one-image'] },
  { kind: 'Deployment', name: 'app', namespace: 'two', images: ['shared', 'two-image'] },
  { kind: 'PersistentVolumeClaim', name: 'data', namespace: 'one', storageClasses: ['fast'] },
  { kind: 'PersistentVolumeClaim', name: 'data', namespace: 'two', storageClasses: ['slow'] },
  { kind: 'Service', name: 'web', namespace: 'one', servicePorts: [{ port: 80 }] },
  { kind: 'Service', name: 'web', namespace: 'two', servicePorts: [{ port: 80 }] },
  { kind: 'PersistentVolume', name: 'cluster-volume', storageClasses: ['unrelated'] },
];
const config = { storageClassMappings: { fast: 'target-fast', slow: 'target-slow' }, imageMappings: { 'one-image': 'replacement-one', 'two-image': 'replacement-two' }, serviceNodePortMappings: { 'one/web|80|TCP': 30080, 'two/web|80|TCP': 30081 } };

test('single selection filters all option types and excludes cluster-scoped references', () => {
  const options = recoveryAdvancedOptions(contents, ['one'], true);
  assert.deepEqual(options.storageClasses, ['fast']);
  assert.deepEqual(options.images, ['one-image', 'shared']);
  assert.deepEqual(options.servicePorts.map(port => port.key), ['one/web|80|TCP']);
  assert.deepEqual(selectedAdvancedMappings(options, config), { storageClassMappings: { fast: 'target-fast' }, imageMappings: { 'one-image': 'replacement-one' }, serviceNodePortMappings: { 'one/web|80|TCP': 30080 }, persistentDataExpected: true });
});
test('multiple selection deduplicates references but distinguishes same-name services', () => {
  const options = recoveryAdvancedOptions([...contents, ...contents], ['one', 'two'], true);
  assert.deepEqual(options.storageClasses, ['fast', 'slow']);
  assert.deepEqual(options.images, ['one-image', 'shared', 'two-image']);
  assert.deepEqual(options.servicePorts.map(port => port.key), ['one/web|80|TCP', 'two/web|80|TCP']);
});
test('deselect and reselect preserve draft mappings without submitting hidden values', () => {
  const before = structuredClone(config);
  assert.deepEqual(selectedAdvancedMappings(recoveryAdvancedOptions(contents, [], true), config), { storageClassMappings: {}, imageMappings: {}, serviceNodePortMappings: {}, persistentDataExpected: false });
  selectedAdvancedMappings(recoveryAdvancedOptions(contents, ['two'], true), config);
  assert.deepEqual(config, before);
  assert.equal(selectedAdvancedMappings(recoveryAdvancedOptions(contents, ['one'], true), config).imageMappings['one-image'], 'replacement-one');
});
test('single namespace packages retain legacy service keys', () => {
  assert.equal(recoveryAdvancedOptions(contents, ['one'], false).servicePorts[0].key, 'web|80|TCP');
});
