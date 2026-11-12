import { apiDelete, apiGet, apiPost, apiPut } from './client.ts';
import type { ApiList, ApiProtectionPlan } from '../features/recovery/types.ts';

export const listProtectionPlans = () => apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans');
export const createProtectionPlan = (input: Record<string, unknown>) => apiPost<ApiProtectionPlan>('/api/v1/protection-plans', input);
export const activateProtectionPlan = (id: string) => apiPost<ApiProtectionPlan>(`/api/v1/protection-plans/${encodeURIComponent(id)}/activate`, {});
export const reconfigureProtectionPlanStorage = (id: string) => apiPost<ApiProtectionPlan>(`/api/v1/protection-plans/${encodeURIComponent(id)}/storage/reconfigure`, {});
export const deleteProtectionPlan = (id: string, force = false) => apiDelete<ApiProtectionPlan>(`/api/v1/protection-plans/${encodeURIComponent(id)}${force ? '?force=true' : ''}`);

export const updateProtectionPlan = (id: string, input: Record<string, unknown>) => apiPut<ApiProtectionPlan>(`/api/v1/protection-plans/${encodeURIComponent(id)}`, input);
