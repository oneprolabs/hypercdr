import { apiGet } from './client.ts';
import type { ApiList } from '../features/recovery/types.ts';
import type { ApiStorageRepo } from '../features/recovery/platform-types.ts';

export const listStorageRepositories = () => apiGet<ApiList<ApiStorageRepo>>('/api/v1/storage-repositories');
