import type { ApiTask } from './types';
import { preferTaskSnapshot } from './progress-snapshot.ts';

export function protectionCleanupBlockingTasks(planIds: string[], tasks: ApiTask[]): ApiTask[] {
  const selected = new Set(planIds);
  const latest = new Map<string, ApiTask>();
  for (const task of tasks) latest.set(task.id, preferTaskSnapshot(latest.get(task.id), task));
  return [...latest.values()].filter(task => selected.has(String(task.protectionPlanId || task.payload?.protectionPlanId || ''))
    && !['succeeded', 'completed', 'success', 'failed', 'canceled', 'cancelled', 'error', 'timeout', 'timed_out'].includes(task.status.toLowerCase()));
}

export function forceAbortCandidates(planIds: string[], tasks: ApiTask[]): ApiTask[] {
  const types = new Set(['backup', 'restore', 'drill', 'takeover', 'protection-cleanup', 'retention-cleanup', 'storage-sync', 'schedule-sync']);
  return protectionCleanupBlockingTasks(planIds, tasks).filter(task => types.has(task.type));
}

export function forceAbortBlockedReason(task: ApiTask): string | null {
  if (['succeeded', 'completed', 'success', 'failed', 'canceled', 'cancelled', 'error', 'timeout', 'timed_out'].includes(task.status.toLowerCase())
    || (task.completedAt && !task.completedAt.startsWith('0001-'))) return 'Task has finished';
  if (task.status.toLowerCase() === 'canceling' || task.payload?.abortRequested === true) return 'Aborting';
  if (['data_ready', 'deleting_old_point'].includes(String(task.payload?.quotaStage || ''))) return 'Restore point replacement in progress';
  return null;
}

export function forceAbortableTasks(planIds: string[], tasks: ApiTask[]): ApiTask[] {
  return forceAbortCandidates(planIds, tasks).filter(task => !forceAbortBlockedReason(task));
}
