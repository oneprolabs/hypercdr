import assert from 'node:assert/strict';
import test from 'node:test';
import { apiGet, apiUpload, apiDownload, ensureApiResponse, ApiRequestError } from './client.ts';
import { writeStoredAuthSession } from '../auth/session.ts';

function setup(t) {
 const storage = new Map();
 const events = [];
 t.mock.method(globalThis, 'fetch', async () => new Response('{}'));
 const previousStorage = globalThis.localStorage;
 const previousWindow = globalThis.window;
 globalThis.localStorage = { getItem: key => storage.get(key), setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key) };
 globalThis.window = { dispatchEvent: event => events.push(event.type) };
 t.after(() => { globalThis.localStorage = previousStorage; globalThis.window = previousWindow; });
 const signIn = token => writeStoredAuthSession({ user: { email: 'test' }, session: { token, expiresAt: new Date(Date.now() + 60000).toISOString() } });
 signIn('session-1');
 return { events, signIn };
}

test('multipart uploads keep browser boundary and expire only the requesting session', async t => {
 const { events } = setup(t);
 globalThis.fetch.mock.mockImplementation(async (_path, options) => {
  assert.equal(options.headers.Authorization, 'Bearer session-1');
  assert.equal(options.headers['Content-Type'], undefined);
  assert.ok(options.body instanceof FormData);
  return new Response(JSON.stringify({ error: 'session_expired', message: 'Sign in again' }), { status: 401, headers: { 'x-request-id': 'req-1' } });
 });
 await assert.rejects(apiUpload('/api/v1/cluster-registrations/kubeconfigs', new FormData()), error => error instanceof ApiRequestError && error.code === 'session_expired' && error.requestId === 'req-1');
 assert.deepEqual(events, ['hypercdr:auth-expired']);
});

test('late 401 from an old session does not sign out a newly signed-in user', async t => {
 const { events, signIn } = setup(t);
 signIn('session-2');
 await assert.rejects(ensureApiResponse(new Response('{}', { status: 401 }), '/api/v1/clusters', 'session-1'));
 assert.deepEqual(events, []);
});

test('invalid password response does not expire a signed-in session', async t => {
 const { events } = setup(t);
 await assert.rejects(ensureApiResponse(new Response('{}', { status: 401 }), '/api/v1/auth/login', 'session-1'));
 assert.deepEqual(events, []);
});

test('binary downloads retain the shared session and error contract', async t => {
 setup(t);
 globalThis.fetch.mock.mockImplementation(async (_path, options) => {
  assert.equal(options.headers.Authorization, 'Bearer session-1');
  return new Response('log contents');
 });
 assert.equal(await (await apiDownload('/api/v1/diagnostic-logs/export')).text(), 'log contents');
});

test('simultaneous GETs share work only within the same session', async t => {
 const { signIn } = setup(t);
 const releases = [];
 globalThis.fetch.mock.mockImplementation((_path, options) => new Promise(resolve => releases.push(() => resolve(new Response(JSON.stringify({ token: options.headers.Authorization }))))));
 const first = apiGet('/api/v1/clusters');
 const duplicate = apiGet('/api/v1/clusters');
 signIn('session-2');
 const next = apiGet('/api/v1/clusters');
 assert.equal(releases.length, 2);
 releases.forEach(release => release());
 assert.deepEqual(await first, await duplicate);
 assert.equal((await next).token, 'Bearer session-2');
});
