import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { buildPolicyInput, defaultPolicyForm } from './policy-form-model.ts';

test('policy quota rejects zero, negative, fractional and empty values without normalization', () => {
  for (const retention of [0, -1, 1.5, '', undefined, null, NaN, Infinity]) {
    assert.throws(() => buildPolicyInput({ ...defaultPolicyForm(), name: 'quota-test', retention }), /positive integer/);
  }
});

test('valid integer quotas are retained for every policy composition', () => {
  for (const composition of ['combined', 'schedule', 'retention']) {
    for (const retention of [1, 7, 100]) {
      const input = buildPolicyInput({ ...defaultPolicyForm(), name: ' quota-test ', composition, retention });
      assert.equal(input.retentionCount, retention);
      assert.equal(input.name, 'quota-test');
    }
  }
});

test('policy submission handles validation inside its error boundary', () => {
  const source = readFileSync(new URL('./policy-page.tsx', import.meta.url), 'utf8');
  const save = source.slice(source.indexOf('const savePolicy ='), source.indexOf('const togglePolicyStatus ='));
  assert.ok(save.indexOf('try {') < save.indexOf('buildPolicyInput(policyForm)'));
  assert.match(save, /catch \(error\)/);
  assert.match(save, /finally\s*\{\s*setSavingPolicy\(false\)/);
  for (const name of ['openCreatePolicy', 'openEditPolicy', 'closePolicyModal']) {
    const handler = source.slice(source.indexOf(`const ${name} =`)).split('\n  };')[0];
    assert.match(handler, /setPolicyError\(null\)/);
  }
  const edit = source.slice(source.indexOf('{policyModalOpen && editingPolicyId'));
  assert.match(edit, /role="alert"/);
});
