import test from 'node:test';
import assert from 'node:assert/strict';
import { operationOutcome, operationSummary } from '../applications/task-operation-model.ts';

test('request acceptance is not execution completion and original failures do not describe cleanup', () => {
  for (const status of ['queued', 'dispatched', 'accepted']) assert.equal(operationOutcome('cleanup', { operationTask: { status }, failed: false }).label, 'Queued');
  assert.equal(operationOutcome('cleanup', { operationTask: { status: 'running' }, targetTask: { status: 'failed' } }).label, 'Cleaning');
  assert.equal(operationOutcome('cleanup', { operationTask: { status: 'succeeded' } }).label, 'Cleaned');
  assert.equal(operationOutcome('cleanup', { operationTask: { status: 'failed' } }).retryable, true);
});
test('abort completion comes from explicit confirmation, not any terminal target', () => {
  assert.equal(operationOutcome('abort', { operationTask: { status: 'succeeded' }, targetTask: { status: 'running' } }).label, 'Confirming result');
  assert.equal(operationOutcome('abort', { targetTask: { status: 'failed', errorCode: 'TASK_ABORTED_BY_USER' } }).label, 'Aborted');
  for (const status of ['succeeded', 'failed', 'canceled']) {
    const outcome = operationOutcome('abort', { failed: true, targetTask: { status } });
    assert.equal(outcome.label, 'Task already finished');
    assert.equal(outcome.retryable, false);
  }
});
test('status transport errors are not operation failures or retryable submissions', () => {
  const outcome = operationOutcome('cleanup', { unavailable: true, operationTask: { status: 'running' } });
  assert.equal(outcome.label, 'Status unavailable');
  assert.equal(outcome.retryable, false);
  assert.equal(outcome.terminal, false);
  assert.equal(operationOutcome('abort', { submitting: true }).label, 'Submitting');
  assert.equal(operationSummary('cleanup', { first: { operationTask: { status: 'succeeded' } }, second: { operationTask: { status: 'running' } }, third: { failed: true } }), '1 completed · 1 in progress · 1 failed');
});
