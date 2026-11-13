import type { ApiTask } from '../recovery/types.ts';

export type OperationMode = 'abort' | 'cleanup';
export type OperationResult = { message: string; failed: boolean; operationTask?: ApiTask; targetTask?: ApiTask; submitting?: boolean; unavailable?: boolean; acceptedAt?: number };
export const taskFinished = (task?: ApiTask) => Boolean(task && ['succeeded', 'failed', 'canceled', 'cancelled'].includes(task.status));

export function operationOutcome(mode: OperationMode, result: OperationResult) {
  const task = result.operationTask;
  if (result.submitting) return { label: 'Submitting', tone: 'progress', retryable: false, terminal: false };
  if (result.unavailable) return { label: 'Status unavailable', tone: 'neutral', retryable: false, terminal: false };
  if (mode === 'abort' && taskFinished(result.targetTask)) {
    if (result.targetTask?.errorCode === 'TASK_ABORTED_BY_USER' || result.targetTask?.payload?.abortConfirmed === true) return { label: 'Aborted', tone: 'success', retryable: false, terminal: true };
    return { label: 'Task already finished', tone: 'neutral', retryable: false, terminal: true };
  }
  if (task?.status === 'succeeded') return { label: mode === 'abort' ? 'Confirming result' : 'Cleaned', tone: mode === 'abort' ? 'progress' : 'success', retryable: false, terminal: mode !== 'abort' };
  if (result.failed || taskFinished(task)) return { label: mode === 'abort' ? 'Abort failed' : 'Cleanup failed', tone: 'error', retryable: true, terminal: true };
  if (!task || ['queued', 'dispatched', 'accepted'].includes(task.status)) return { label: 'Queued', tone: 'progress', retryable: false, terminal: false };
  return { label: mode === 'abort' ? 'Aborting' : 'Cleaning', tone: 'progress', retryable: false, terminal: false };
}

export function operationSummary(mode: OperationMode, results: Record<string, OperationResult>) {
  const outcomes = Object.values(results).map(result => operationOutcome(mode, result));
  return [
    [outcomes.filter(item => item.terminal && !item.retryable).length, 'completed'],
    [outcomes.filter(item => !item.terminal).length, 'in progress'],
    [outcomes.filter(item => item.retryable).length, 'failed'],
  ].filter(([count]) => count).map(([count, label]) => `${count} ${label}`).join(' · ');
}
