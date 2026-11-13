import assert from 'node:assert/strict';
import test from 'node:test';
import { createTaskDetailLoader } from './task-detail-loader.ts';

test('prefetch shares in-flight reads and expires without caching failures', async () => {
  let taskReads = 0;
  let eventReads = 0;
  let clock = 0;
  let release;
  const loader = createTaskDetailLoader(() => { taskReads++; return new Promise(resolve => { release = resolve; }); }, async () => { eventReads++; return []; }, () => clock);
  loader.prefetch('one');
  const pending = loader.task('one');
  assert.equal(loader.task('one'), pending);
  await Promise.resolve();
  release({ id: 'one' });
  await pending;
  assert.deepEqual(loader.task.peek('one'), { id: 'one' });
  await loader.events('one');
  assert.equal(taskReads, 1);
  assert.equal(eventReads, 1);
  clock = 1001;
  assert.deepEqual(loader.task.peek('one'), { id: 'one' });
  const refreshed = loader.task('one');
  await Promise.resolve();
  release({ id: 'one', status: 'succeeded' });
  assert.equal((await refreshed).status, 'succeeded');
  assert.equal(taskReads, 2);
  let attempts = 0;
  const failing = createTaskDetailLoader(async () => { if (++attempts === 1) throw Error('network'); return 'ok'; }, async () => []);
  await assert.rejects(failing.task('one'));
  assert.equal(await failing.task('one'), 'ok');
});

test('loaders are page scoped, task scoped, and bounded', async () => {
  let reads = 0;
  const load = async id => { reads++; return id; };
  const first = createTaskDetailLoader(load, load);
  const second = createTaskDetailLoader(load, load);
  await first.task('one');
  await second.task('one');
  assert.equal(reads, 2);
  for (let index = 0; index < 21; index++) await first.task(String(index));
  await first.task('one');
  assert.equal(reads, 24);
});
