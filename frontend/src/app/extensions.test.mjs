import assert from 'node:assert/strict';
import test from 'node:test';
import { validateFrontendModules } from './extensions.ts';

const moduleDefinition = (id, view, order) => ({
  id,
  view,
  navigation: { label: id, description: id, icon: () => null, order, group: 'settings' },
  component: () => null,
});

test('frontend modules are ordered without mutating the caller array', () => {
  const input = [moduleDefinition('second', 'extension:second', 20), moduleDefinition('first', 'extension:first', 10)];
  const result = validateFrontendModules(input);
  assert.deepEqual(result.map(item => item.id), ['first', 'second']);
  assert.deepEqual(input.map(item => item.id), ['second', 'first']);
});

test('duplicate module identifiers and views are rejected', () => {
  assert.throws(() => validateFrontendModules([
    moduleDefinition('duplicate', 'extension:first', 10),
    moduleDefinition('duplicate', 'extension:second', 20),
  ]), /Duplicate HyperCDR module id/);
  assert.throws(() => validateFrontendModules([
    moduleDefinition('first', 'extension:duplicate', 10),
    moduleDefinition('second', 'extension:duplicate', 20),
  ]), /Duplicate HyperCDR module view/);
});
