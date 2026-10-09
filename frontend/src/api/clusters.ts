import { apiGet, apiPost } from './client.ts';
import type { ApiList, ApiTask, ApiTaskResponse } from '../features/recovery/types.ts';
import type { ApiCluster } from '../features/recovery/platform-types.ts';

export const listClusters = () => apiGet<ApiList<ApiCluster>>('/api/v1/clusters');
export const listClusterIdentities = () => apiGet<ApiList<Pick<ApiCluster, 'id' | 'isDefault'>>>('/api/v1/clusters?view=summary');
export const setClusterDefault = (id: string) => apiPost<ApiCluster>(`/api/v1/clusters/${encodeURIComponent(id)}/default`, {});
export const requestClusterUnregister = (id: string, input: { deleteVelero: boolean; deleteNamespace: boolean; deleteBackupData: boolean; reason: string }) => apiPost<ApiTaskResponse>(`/api/v1/clusters/${encodeURIComponent(id)}/unregister`, input);
export const upgradeClusterAgent = (id: string) => apiPost<ApiTask>(`/api/v1/clusters/${encodeURIComponent(id)}/agent/upgrade`, {});
export const upgradeClusterVelero = (id: string) => apiPost<ApiTask>(`/api/v1/clusters/${encodeURIComponent(id)}/velero/upgrade`, {});
