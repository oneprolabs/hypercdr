import type { ApiTask } from './types';

export function isActiveTaskStatus(status: string | undefined): boolean {
  return ['queued', 'dispatched', 'accepted', 'running', 'syncing', 'finalizing', 'canceling'].includes((status || '').toLowerCase());
}

export function isCompletedTaskStatus(status: string | undefined): boolean {
  return ['succeeded', 'completed', 'success'].includes((status || '').toLowerCase());
}

export function isSucceededStatus(status?: string) {
  return status === 'succeeded' || status === 'completed';
}

export function isFailedStatus(status?: string) {
  return status === 'failed' || status === 'canceled' || status === 'cancelled' || status === 'error' || status === 'timeout' || status === 'timed_out';
}

export function taskHasWarning(task?: ApiTask) {
  return Boolean(task?.errorCode || task?.errorMessage);
}
