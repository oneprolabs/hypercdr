import assert from 'node:assert/strict';
import test from 'node:test';
import { loadPlatformSnapshot, loadApplicationActivitySnapshot, loadTopologySnapshot } from './platform-snapshots.ts';
import { writeStoredAuthSession } from '../auth/session.ts';
import { scopedResourceSetter } from './resource-scope.ts';
import { updateApplicationProtection } from '../api/applications.ts';

const deferred = () => { let resolve; const promise = new Promise(done => { resolve = done; }); return { promise, resolve }; };
const response = body => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });

function setup(t) {
  const storage = new Map();
  const oldStorage = globalThis.localStorage;
  const oldWindow = globalThis.window;
  globalThis.localStorage = { getItem: key => storage.get(key), setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key) };
  globalThis.window = { dispatchEvent: () => {} };
  t.after(() => { globalThis.localStorage = oldStorage; globalThis.window = oldWindow; });
  const signIn = token => writeStoredAuthSession({ user: { email: 'snapshot-test' }, session: { token, expiresAt: new Date(Date.now() + 60000).toISOString() } });
  signIn('snapshot-session-1');
  t.mock.method(globalThis, 'fetch', async () => response({ items: [] }));
  return signIn;
}

test('session replacement during pointed-task lookup discards the entire old snapshot', async t => {
  const signIn = setup(t);
  const started = deferred();
  const pending = deferred();
  let owner = 'snapshot-session-1';
  const headers = [];
  globalThis.fetch.mock.mockImplementation(async (path, options) => {
    headers.push(options.headers.Authorization);
    if (path === '/api/v1/protection-plans') return response({ items: [{ id: 'plan', latestSyncTaskId: 'pointed-task' }] });
    if (path === '/api/v1/tasks/pointed-task') { started.resolve(); return pending.promise; }
    return response({ items: [] });
  });
  const snapshot = loadApplicationActivitySnapshot(() => owner === 'snapshot-session-1');
  await started.promise;
  owner = 'snapshot-session-2';
  signIn(owner);
  pending.resolve(response({ id: 'pointed-task', status: 'running' }));
  assert.equal(await snapshot, null);
  assert.ok(headers.every(header => header === 'Bearer snapshot-session-1'));
});

test('scope expiry during the initial batch prevents dependent task lookups', async t => {
  setup(t);
  const started = deferred();
  const pending = deferred();
  const paths = [];
  let current = true;
  globalThis.fetch.mock.mockImplementation(async path => {
    paths.push(path);
    if (path === '/api/v1/protection-plans') return response({ items: [{ latestRecoveryTaskId: 'old-task' }] });
    if (path.startsWith('/api/v1/tasks?')) { started.resolve(); return pending.promise; }
    return response({ items: [] });
  });
  const snapshot = loadApplicationActivitySnapshot(() => current);
  await started.promise;
  current = false;
  pending.resolve(response({ items: [] }));
  assert.equal(await snapshot, null);
  assert.equal(paths.some(path => path === '/api/v1/tasks/old-task'), false);
});

test('late partial cluster results cannot populate an expired session', async t => {
  setup(t);
  const pending = deferred();
  let current = true;
  const partial = [];
  globalThis.fetch.mock.mockImplementation(async path => path === '/api/v1/clusters' ? pending.promise : response({ items: [] }));
  const snapshot = loadPlatformSnapshot(() => current, items => partial.push(items));
  current = false;
  pending.resolve(response({ items: [{ id: 'old-cluster', name: 'Old workspace' }] }));
  assert.equal(await snapshot, null);
  assert.deepEqual(partial, []);
});

test('persisted plan pointers hydrate tasks omitted by the bounded summary query', async t => {
  setup(t);
  const paths = [];
  globalThis.fetch.mock.mockImplementation(async path => {
    paths.push(path);
    if (path === '/api/v1/protection-plans') return response({ items: [{ id: 'plan', latestSyncTaskId: 'recent', latestRecoveryTaskId: 'older-recovery' }] });
    if (path.startsWith('/api/v1/tasks?')) return response({ items: [{ id: 'recent', status: 'running' }] });
    if (path === '/api/v1/tasks/older-recovery') return response({ id: 'older-recovery', status: 'succeeded' });
    return response({ items: [] });
  });
  const snapshot = await loadApplicationActivitySnapshot(() => true);
  assert.deepEqual(snapshot.tasks.map(task => task.id), ['recent', 'older-recovery']);
  assert.equal(paths.includes('/api/v1/tasks/recent'), false);
  assert.equal(snapshot.plans[0].latestRecoveryTaskId, 'older-recovery');
});

test('a missing pointed task preserves the remaining successful snapshot', async t => {
  setup(t);
  globalThis.fetch.mock.mockImplementation(async path => {
    if (path === '/api/v1/protection-plans') return response({ items: [{ id: 'plan', latestSyncTaskId: 'missing' }] });
    if (path === '/api/v1/tasks/missing') return new Response('{"error":"resource_not_found"}', { status: 404 });
    return response({ items: [] });
  });
  const snapshot = await loadApplicationActivitySnapshot(() => true);
  assert.deepEqual(snapshot.tasks, []);
  assert.equal(snapshot.plans[0].id, 'plan');
});

test('inactive scopes do not launch platform, activity, or topology requests', async t => {
  setup(t);
  assert.equal(await loadPlatformSnapshot(() => false), null);
  assert.equal(await loadApplicationActivitySnapshot(() => false), null);
  assert.equal(await loadTopologySnapshot(() => false), null);
  assert.equal(globalThis.fetch.mock.callCount(), 0);
});

test('a late mutation response cannot update the resource state of a replacement session', async t => {
  const signIn = setup(t);
  const pending = deferred();
  globalThis.fetch.mock.mockImplementation(async (_path, options) => {
    assert.equal(options.headers.Authorization, 'Bearer snapshot-session-1');
    assert.equal(options.method, 'PATCH');
    return pending.promise;
  });
  const owner = { current: 'snapshot-session-1' };
  let state = ['previous-user'];
  const setOldResources = scopedResourceSetter(owner, owner.current, update => { state = typeof update === 'function' ? update(state) : update; });
  const mutation = updateApplicationProtection('application', 'protected').then(app => setOldResources([app.id]));
  owner.current = 'snapshot-session-2';
  signIn(owner.current);
  state = ['current-user'];
  pending.resolve(response({ id: 'old-user-application', protectionStatus: 'protected' }));
  await mutation;
  assert.deepEqual(state, ['current-user']);
});

test('session-bound setters preserve functional updates for the owning session', () => {
  const owner = { current: 'current-session' };
  let state = ['existing'];
  const setResources = scopedResourceSetter(owner, owner.current, update => { state = typeof update === 'function' ? update(state) : update; });
  setResources(previous => [...previous, 'new']);
  assert.deepEqual(state, ['existing', 'new']);
  owner.current = '';
  setResources(['late-after-signout']);
  assert.deepEqual(state, ['existing', 'new']);
});
