import assert from 'node:assert/strict';
import test from 'node:test';
import { preferTaskSnapshot, transferPercent } from './progress-snapshot.ts';
import './sync-volume-model.test.mjs';

test('byte progress does not reset when percent is missing or rounded to zero', () => {
  for (const percent of [undefined, 0, 99]) {
    assert.equal(transferPercent({ knownTotal: true, allTotalsKnown: true, bytesDone: 80, totalBytes: 100, percent }), 80);
  }
});

test('unknown or incomplete totals are indeterminate rather than zero percent', () => {
  for (const patch of [{ allTotalsKnown: false }, { knownTotal: false }, { totalBytes: 0 }, { bytesDone: NaN }]) {
    assert.equal(transferPercent({ knownTotal: true, allTotalsKnown: true, bytesDone: 80, totalBytes: 100, ...patch }), undefined);
  }
});

test('valid denominator changes are not hidden behind a monotonic percentage', () => {
  assert.equal(transferPercent({ knownTotal: true, allTotalsKnown: true, bytesDone: 80, totalBytes: 200 }), 40);
});

for (const type of ['backup', 'drill']) {
  test(`${type} rejects delayed snapshots and preserves terminal state`, () => {
    const older = { id: 'task', type, status: 'running', payload: { lastStatusAt: '2026-11-05T01:00:00Z' } };
    const newer = { ...older, payload: { lastStatusAt: '2026-11-05T01:00:02Z' } };
    assert.equal(preferTaskSnapshot(newer, older), newer);
    assert.equal(preferTaskSnapshot(older, newer), newer);
    const nextStage = { ...newer, progress: 0, payload: { lastStatusAt: '2026-11-05T01:00:03Z', stage: 'finalizing' } };
    assert.equal(preferTaskSnapshot(newer, nextStage), nextStage);
    const completed = { ...newer, status: 'succeeded' };
    assert.equal(preferTaskSnapshot(completed, newer), completed);
  });
}
