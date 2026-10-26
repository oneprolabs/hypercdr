import { apiGet } from './client';
import type { ApiList, ApiRestorePoint, ApiTask } from '../features/recovery/types';

// Scope before limiting, so busy neighboring plans cannot hide this namespace’s tasks.
export function listNamespaceRestorePoints(planId: string, appId?: string, clusterId?: string) {
  if (!planId && !appId) throw new Error('A protection plan or application is required to load restore points.');
  const scope = planId ? `protectionPlanId=${encodeURIComponent(planId)}` : `appId=${encodeURIComponent(appId || '')}&clusterId=${encodeURIComponent(clusterId || '')}`;
  return apiGet<ApiList<ApiRestorePoint>>(`/api/v1/restore-points?${scope}&pageSize=500`);
}

export function listNamespaceTasks(planId: string) {
  return apiGet<ApiList<ApiTask>>(`/api/v1/tasks?protectionPlanId=${encodeURIComponent(planId)}&view=summary&types=backup,restore,drill,takeover,retention-cleanup,protection-cleanup&limit=500`);
}
