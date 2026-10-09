import { apiPost, apiUpload } from '../../api/client';
import type { RegistrationType } from './cluster-registration-choices';

export type RegistrationUpload = { id: string; contexts: Array<{ name: string; apiServer: string; isCurrent?: boolean }> };
export type RegistrationInspection = { clusterName: string; storageClasses: string[]; defaultStorageClass?: string; gates: Array<{ id: string; label: string; status: string; detail: string }> };
export type RegistrationTask = { id: string; status: string; progress?: number; errorMessage?: string; clusterId?: string };

export async function uploadRegistrationKubeconfig<T extends RegistrationUpload>(file: File, clusterType: RegistrationType): Promise<T> {
  const body = new FormData();
  body.append('kubeconfig', file);
  body.append('clusterType', clusterType);
  const path = '/api/v1/cluster-registrations/kubeconfigs';
  return apiUpload<T>(path, body);
}
export function inspectRegistrationCluster<T extends RegistrationInspection>(sessionId: string, context: string, clusterType: RegistrationType): Promise<T> {
  return apiPost<T>('/api/v1/cluster-registrations/inspections', { sessionId, context, clusterType });
}
export function startRegistrationTask<T extends RegistrationTask>(sessionId: string, context: string, storageClass: string, clusterType: RegistrationType, idempotencyKey: string): Promise<T> {
  return apiPost<T>('/api/v1/cluster-registrations/tasks', { sessionId, context, storageClass, idempotencyKey, clusterType });
}
export async function cancelRegistrationTask<T extends RegistrationTask>(taskId: string): Promise<T> {
  const response = await apiPost<{ task: T }>(`/api/v1/tasks/${encodeURIComponent(taskId)}/cancel`, {});
  return response.task;
}
