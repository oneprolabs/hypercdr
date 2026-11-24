import assert from 'node:assert/strict';
import test from 'node:test';
import { clearBillingReturn } from './billing-return.ts';

for (const billing of ['success', 'canceled', 'return']) {
  test(`consumes ${billing} once and preserves navigation state`, () => {
    const state = { view: 'extension:license' };
    const calls = [];
    const history = { state, replaceState: (...args) => calls.push(args) };
    clearBillingReturn(history, `https://example.test/?billing=${billing}&session_id=checkout&auth=login#section`);
    assert.equal(calls.length, 1);
    assert.equal(calls[0][0], state);
    assert.equal(calls[0][2], 'https://example.test/?auth=login#section');
    clearBillingReturn(history, calls[0][2]);
    assert.equal(calls.length, 1);
  });
}

test('leaves unrelated URLs and unrecognized return values untouched', () => {
  const history = { replaceState: () => assert.fail('must not modify history') };
  clearBillingReturn(history, 'https://example.test/?session_id=other');
  clearBillingReturn(history, 'https://example.test/?billing=unknown&session_id=other');
});
