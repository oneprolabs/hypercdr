import { apiGet, apiPatch, apiPut } from './client.ts';
import type { ApiApplication, ApiList, TagItem } from '../features/recovery/types.ts';

export const listApplications = (summary = false, clusterId = '') => {
  const query = new URLSearchParams();
  if (summary) query.set('view', 'summary');
  if (clusterId) query.set('clusterId', clusterId);
  return apiGet<ApiList<ApiApplication>>(`/api/v1/applications${query.size ? `?${query}` : ''}`);
};
export const listTags = () => apiGet<ApiList<TagItem>>('/api/v1/tags');
export const updateApplicationProtection = (id: string, protectionStatus: string) => apiPatch<ApiApplication>(`/api/v1/applications/${encodeURIComponent(id)}`, { protectionStatus });
export const replaceApplicationTags = (id: string, tagIds: string[]) => apiPut<ApiApplication>(`/api/v1/applications/${encodeURIComponent(id)}/tags`, { tagIds });
