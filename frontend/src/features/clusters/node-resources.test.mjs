import assert from 'node:assert/strict';
import test from 'node:test';
import { nodeResource, resourceQuantity } from './node-resources.ts';

test('Kubernetes quantities retain their units', () => {
  assert.equal(resourceQuantity('8000m'), 8);
  assert.equal(resourceQuantity('16Gi'), 16 * 1024 ** 3);
  assert.equal(resourceQuantity('16384000Ki'), 16384000 * 1024);
  assert.equal(resourceQuantity('invalid'), undefined);
});
test('CPU usage uses cores and physical capacity', () => {
  const value = nodeResource('cpu', { cpu: '8' }, { cpuCores: '1.2' });
  assert.equal(value.text, '1.2 / 8 cores');
  assert.equal(value.percent, 15);
});
test('unknown usage never becomes zero', () => {
  const value = nodeResource('memory', { memory: '16Gi' });
  assert.equal(value.text, '— / 16.0 GiB');
  assert.equal(value.percent, undefined);
  assert.equal(nodeResource('cpu', { cpu: '8' }, { cpuCores: '0' }).percent, 0);
});
test('storage uses measured root filesystem rather than ephemeral capacity', () => {
  const value = nodeResource('storage', { 'ephemeral-storage': '900Gi' }, { storageCapacityBytes: String(200 * 1024 ** 3), storageUsedBytes: String(42 * 1024 ** 3) });
  assert.equal(value.text, '42.0 / 200.0 GiB');
  assert.equal(value.percent, 21);
  assert.equal(nodeResource('storage', { 'ephemeral-storage': '900Gi' }).total, undefined);
});
test('invalid and zero capacities do not manufacture percentages', () => {
  assert.equal(nodeResource('cpu', { cpu: '0' }, { cpuCores: '1' }).percent, undefined);
  assert.equal(nodeResource('memory', { memory: '16Gi' }, { memoryBytes: '-1' }).used, undefined);
});
