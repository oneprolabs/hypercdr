import assert from 'node:assert/strict';
import test from 'node:test';
import { browserTimeZone, formatLocalDateTime, parseUTCInstant, setUserTimeZone } from './date-time.ts';

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

test('offset tag timestamps follow UTC, another timezone and the browser preference', t => {
  t.after(() => setUserTimeZone());
  const value = '2026-11-12T14:49:09.28812+08:00';
  setUserTimeZone('UTC');
  assert.equal(formatLocalDateTime(value), '2026-11-12 06:49:09');
  setUserTimeZone('America/New_York');
  assert.equal(formatLocalDateTime(value), '2026-11-12 01:49:09');
  setUserTimeZone(browserTimeZone);
  const browserResult = formatLocalDateTime(value);
  setUserTimeZone();
  assert.equal(formatLocalDateTime(value), browserResult);
  assert.equal(value, '2026-11-12T14:49:09.28812+08:00');
});

test('empty and malformed tag timestamps have a safe empty display', () => {
  for (const value of [undefined, '', 'not-a-date']) assert.equal(formatLocalDateTime(value), '');
});
