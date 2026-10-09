import { apiGet, apiPost } from './client';

export interface InventoryRequest {
  scope: 'summary' | 'capabilities' | 'namespaceResources';
  namespace?: string;
  includeDetails?: boolean;
  reason?: string;
  includeRecentVeleroObjects?: boolean;
}

export interface InventorySubmission {
  status: string;
  warning?: string;
  requestId: string;
}

export interface InventoryRequestStatus {
  status: string;
  message?: string;
  errorCode?: string;
}

export function requestClusterInventory(clusterId: string, input: InventoryRequest) {
  return apiPost<InventorySubmission>(`/api/v1/clusters/${encodeURIComponent(clusterId)}/inventory/request`, input);
}

export function getInventoryRequestStatus(clusterId: string, requestId: string) {
  return apiGet<InventoryRequestStatus>(`/api/v1/clusters/${encodeURIComponent(clusterId)}/inventory/requests/${encodeURIComponent(requestId)}`);
}
