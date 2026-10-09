import { apiGet, apiPost } from './client.ts';
import type { ApiList, ApiTask, ApiTaskCancelResponse, ApiTaskEvent, ApiTaskResponse } from '../features/recovery/types.ts';

export interface TaskQuery {
  clusterId?: string;
  view?: 'summary';
  types?: string[];
  statuses?: string[];
  limit?: number;
}

export function listTasks(query: TaskQuery = {}) {
  const params = new URLSearchParams();
  if (query.clusterId) params.set('clusterId', query.clusterId);
  if (query.view) params.set('view', query.view);
  if (query.types?.length) params.set('types', query.types.join(','));
  if (query.statuses?.length) params.set('statuses', query.statuses.join(','));
  if (query.limit) params.set('limit', String(query.limit));
  const suffix = params.size ? `?${params}` : '';
  return apiGet<ApiList<ApiTask>>(`/api/v1/tasks${suffix}`);
}
export const getTask = (id: string) => apiGet<ApiTask>(`/api/v1/tasks/${encodeURIComponent(id)}`);
export const listTaskEvents = (id: string) => apiGet<ApiList<ApiTaskEvent>>(`/api/v1/tasks/${encodeURIComponent(id)}/events`);
export const createBackupTask = (input: Record<string, unknown>) => apiPost<ApiTaskResponse>('/api/v1/tasks/backup', input);
export const createRecoveryTask = (mode: 'restore' | 'drill' | 'takeover', input: Record<string, unknown>) => apiPost<ApiTask>(`/api/v1/tasks/${mode}`, input);
export const cancelTask = (id: string) => apiPost<ApiTaskCancelResponse>(`/api/v1/tasks/${encodeURIComponent(id)}/cancel`, {});
export const cleanupDrillTask = (id: string) => apiPost<ApiTaskResponse>(`/api/v1/tasks/${encodeURIComponent(id)}/cleanup-drill`, {});
