import { apiGet } from './client.ts';
import type { ApiList, ApiRestorePoint } from '../features/recovery/types.ts';

export function listRestorePoints(summary = true, pageSize = 500) {
  const params = new URLSearchParams();
  if (summary) params.set('view', 'summary');
  params.set('pageSize', String(pageSize));
  return apiGet<ApiList<ApiRestorePoint>>(`/api/v1/restore-points?${params}`);
}
export const getRestorePointContents = <T>(id: string) => apiGet<T>(`/api/v1/restore-points/${encodeURIComponent(id)}/contents`);
