import test from 'node:test';
import assert from 'node:assert/strict';
import { createModuleLoader, warmPageModules } from './module-loader.ts';

test('navigation and warmup share one request and retain the resolved module', async () => {
  let requests = 0;
  const value = { default: 'page' };
  const module = createModuleLoader(async () => { requests++; return value; });
  assert.equal(module.isReady(), false);
  const first = module.load();
  assert.equal(module.load(), first);
  assert.equal(await first, value);
  assert.equal(await module.load(), value);
  assert.equal(module.isReady(), true);
  assert.equal(module.getValue(), value);
  assert.equal(requests, 1);
});

test('failed warmup does not poison later navigation', async () => {
  let requests = 0;
  const module = createModuleLoader(async () => {
    if (++requests === 1) throw new Error('temporary network failure');
    return 'page';
  });
  await assert.rejects(module.load());
  assert.equal(module.isReady(), false);
  assert.equal(await module.load(), 'page');
});

test('warmup is sequential and stops after a session change', async () => {
  let current = true;
  const loaded = [];
  await warmPageModules([
    async () => { loaded.push('clusters'); current = false; },
    async () => { loaded.push('applications'); },
  ], () => current);
  assert.deepEqual(loaded, ['clusters']);
});

test('one optional preload failure does not block other pages', async () => {
  const loaded = [];
  await warmPageModules([
    async () => { throw new Error('optional page unavailable'); },
    async () => { loaded.push('applications'); },
  ], () => true);
  assert.deepEqual(loaded, ['applications']);
});
