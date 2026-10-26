import assert from 'node:assert/strict';
import test from 'node:test';
import { retentionRuleSummary, validateRetentionRules } from './retention-model.ts';
import { restorePointMutationLocked, restorePointRetentionLabels } from '../recovery/restore-point-retention.ts';

test('automatic retention is optional', () => assert.deepEqual(validateRetentionRules([]), []));
test('all four periods and arbitrary ordinals are supported', () => {
  for (const period of ['daily', 'weekly', 'monthly', 'yearly']) {
    const rule = { period, ordinal: 2, keep: 3 };
    assert.deepEqual(validateRetentionRules([rule]), [rule]);
    assert.match(retentionRuleSummary(rule), /including the current/);
  }
});
test('invalid and duplicate rules are rejected', () => {
  for (const rule of [{ period: 'hourly', ordinal: 1, keep: 3 }, { period: 'daily', ordinal: 0, keep: 3 }, { period: 'daily', ordinal: 1, keep: 1.5 }]) assert.throws(() => validateRetentionRules([rule]));
  const rule = { period: 'daily', ordinal: 1, keep: 3 };
  assert.throws(() => validateRetentionRules([rule, rule]));
});
test('multiple retention reasons coexist and replacement is locked', () => {
  assert.deepEqual(restorePointRetentionLabels({ manualRetained: true, drillRetained: true, retentionReasons: [{ period: 'daily' }, { period: 'weekly' }] }), ['Manual retention', 'Drill retention', 'Daily retention', 'Weekly retention']);
  assert.equal(restorePointMutationLocked({ replacementTaskId: 'task' }), true);
  assert.deepEqual(restorePointRetentionLabels({ replacementTaskId: 'task' }), ['Pending replacement']);
  assert.deepEqual(restorePointRetentionLabels({}), []);
  assert.equal(restorePointMutationLocked({ retentionState: 'deleting' }), true);
  assert.equal(restorePointMutationLocked({ manualRetained: true }), false);
});
