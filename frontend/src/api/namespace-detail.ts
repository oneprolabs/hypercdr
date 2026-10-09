import { apiGet } from './client';
import type { ApiList, ApiRestorePoint, ApiTask } from '../features/recovery/types';

// Keep the existing query limits and summary semantics. Mapping and tab state
// belong to the detail hook, while paths and transport types live here.
export function listNamespaceRestorePoints(planId: string) {
  return apiGet<ApiList<ApiRestorePoint>>(`/api/v1/restore-points?protectionPlanId=${encodeURIComponent(planId)}&pageSize=500`);
}

export function listNamespaceTasks() {
  return apiGet<ApiList<ApiTask>>('/api/v1/tasks?view=summary&types=backup,restore,drill,takeover,retention-cleanup,protection-cleanup&limit=500');
}
