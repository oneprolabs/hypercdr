import assert from 'node:assert/strict';
import test from 'node:test';
import { formatLocalDateTime, parseUTCInstant, setUserTimeZone } from './date-time.ts';

test('timestamps without an explicit zone are parsed as UTC', () => {
  assert.equal(parseUTCInstant('2026-08-27T02:39:26')?.toISOString(), '2026-08-27T02:39:26.000Z');
});

test('one UTC task instant follows the selected page timezone', () => {
  setUserTimeZone('UTC');
  assert.equal(formatLocalDateTime('2026-08-27T02:39:26Z'), '2026-08-27 02:39:26');
  setUserTimeZone('Asia/Shanghai');
  assert.equal(formatLocalDateTime('2026-08-27T02:39:26Z'), '2026-08-27 10:39:26');
  setUserTimeZone('America/New_York');
  assert.equal(formatLocalDateTime('2026-08-27T02:39:26Z'), '2026-08-26 22:39:26');
});
