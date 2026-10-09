import { apiDelete, apiGet, apiPost } from './client.ts';
import type { ApiList, ApiProtectionPlan } from '../features/recovery/types.ts';

export const listProtectionPlans = () => apiGet<ApiList<ApiProtectionPlan>>('/api/v1/protection-plans');
export const createProtectionPlan = (input: Record<string, unknown>) => apiPost<ApiProtectionPlan>('/api/v1/protection-plans', input);
export const activateProtectionPlan = (id: string) => apiPost<ApiProtectionPlan>(`/api/v1/protection-plans/${encodeURIComponent(id)}/activate`, {});
export const reconfigureProtectionPlanStorage = (id: string) => apiPost<ApiProtectionPlan>(`/api/v1/protection-plans/${encodeURIComponent(id)}/storage/reconfigure`, {});
export const deleteProtectionPlan = (id: string) => apiDelete<ApiProtectionPlan>(`/api/v1/protection-plans/${encodeURIComponent(id)}`);
