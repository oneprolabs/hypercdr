import { apiDelete, apiDownload, apiPost } from './client';

export type SupportBundle = { name: string; downloadUrl: string; size: number; generatedAt: string; timeZone: string };
export type SupportBundleInput = { description: string; reproducible: string; screenshotBase64: string; sinceHours: number; timeZone: string };
const bundlePath = (name: string) => `/api/v1/support-bundles/${encodeURIComponent(name)}`;
export const supportBundlesApi = {
  create: (input: SupportBundleInput) => apiPost<SupportBundle>('/api/v1/support-bundles', input),
  download: (name: string) => apiDownload(`${bundlePath(name)}/download`),
  remove: (name: string) => apiDelete<void>(bundlePath(name)),
};
