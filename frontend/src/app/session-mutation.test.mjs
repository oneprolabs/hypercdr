import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sessionMutationScope } from './session-mutation.ts';

test('late account response cannot resurrect a signed-out session', async () => {
  const owner = { current: 'session-one' };
  const apply = sessionMutationScope(owner, owner.current);
  let complete;
  const response = new Promise(resolve => { complete = resolve; });
  const state = { session: 'session-one', theme: 'dark', notices: [] };
  const pending = response.then(() => apply(() => {
    state.session = 'session-one'; state.theme = 'light'; state.notices.push('saved');
  }));
  owner.current = ''; state.session = null;
  complete();
  assert.equal(await pending, false);
  assert.deepEqual(state, { session: null, theme: 'dark', notices: [] });
});

test('old preference errors and finally handlers cannot affect a new login', () => {
  const owner = { current: 'session-one' };
  const old = sessionMutationScope(owner, owner.current);
  owner.current = 'session-two';
  const state = { pending: true, toast: '' };
  assert.equal(old(() => { state.toast = 'old error'; }), false);
  assert.equal(old(() => { state.pending = false; }), false);
  assert.deepEqual(state, { pending: true, toast: '' });
  const current = sessionMutationScope(owner, owner.current);
  assert.equal(current(() => { state.pending = false; }), true);
  assert.equal(state.pending, false);
});

test('anonymous mutation scopes never publish account state', () => {
  let called = false;
  assert.equal(sessionMutationScope({ current: '' }, '')(() => { called = true; }), false);
  assert.equal(called, false);
});
