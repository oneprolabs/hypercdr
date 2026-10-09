import { apiGet } from './client.ts';
import type { ApiList, ApiPolicy } from '../features/recovery/types.ts';

export const listPolicies = () => apiGet<ApiList<ApiPolicy>>('/api/v1/policies');
