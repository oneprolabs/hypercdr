import type { ResourceSummary } from '../clusters/types';

export type PolicyComposition = 'manual' | 'combined' | 'schedule' | 'retention';

export type PolicyScheduleType = 'interval' | 'daily' | 'weekly' | 'monthly';

export type PolicyItem = {
  id: string;
  name: string;
  composition: PolicyComposition;
  type: PolicyScheduleType;
  intervalValue: number;
  intervalUnit: 'minutes' | 'hours';
  hour: number;
  minute: number;
  weekDay: number;
  monthDay: number;
  retention?: number;
  status: 'Active' | 'Disabled';
  bound: number;
};

export interface TagItem {
  id: string;
  name: string;
  createdAt: string;
}

export interface StorageRepo {
  id: string;
  name: string;
  type: string;
  endpoint: string;
  bucket: string;
  region: string;
  useTls: boolean;
  status: 'connected' | 'warning' | 'unknown';
  updatedAt: string;
  lastValidatedAt?: string;
  config?: Record<string, string | boolean>;
  urlStyle?: string;
}

export type ApiList<T> = { items: T[] };

export function listItems<T>(response: ApiList<T>): T[] {
  return Array.isArray(response.items) ? response.items : [];
}

export type ApiApplication = {
  id: string;
  clusterId: string;
  namespace: string;
  name: string;
  status: string;
  labels?: Record<string, string>;
  workloadCount: number;
  serviceCount: number;
  ingressCount: number;
  configMapCount: number;
  secretCount: number;
  pvcCount: number;
  pvCapacityBytes: number;
  resourceSummary?: ResourceSummary;
  protectionStatus?: string;
  tags?: string[];
};

export type ApiPolicy = {
  id: string;
  name: string;
  composition: string;
  scheduleType: string;
  intervalValue?: number;
  intervalUnit?: string;
  hour?: number;
  minute?: number;
  weekDay?: number;
  monthDay?: number;
  retentionCount?: number;
  status: string;
  boundCount: number;
};

export type ApiProtectionPlan = {
  id: string;
  appId: string;
  appIds?: string[];
  sourceClusterId?: string;
  scopeType?: string;
  policyId?: string;
  storageRepoId?: string;
  targetClusterId?: string;
  includedResources?: string[];
  labelSelector?: { matchLabels?: Record<string, string>; matchExpressions?: Array<{ key: string; operator: string; values?: string[] }> };
  excludedResources?: string[];
  status?: string;
  warning?: string;
  activationTask?: ApiTask;
  planStorageSize?: Record<string, any>;
  nextFireAt?: string;
  scheduleEnabled?: boolean;
  createdAt?: string;
  updatedAt?: string;
};

export type ApiTask = {
  id: string;
  clusterId: string;
  appId?: string;
  protectionPlanId?: string;
  restorePointId?: string;
  type: string;
  status: string;
  progress: number;
  commandId?: string;
  errorCode?: string;
  errorMessage?: string;
  payload?: Record<string, any>;
  createdAt?: string;
  dispatchedAt?: string;
  acceptedAt?: string;
  startedAt?: string;
  completedAt?: string;
};

export type ApiTaskEvent = {
  id: string;
  taskId: string;
  level: string;
  reason: string;
  message: string;
  payload?: Record<string, any>;
  createdAt?: string;
};

export type VolumeProgressInfo = {
  operation?: string;
  bytesDone: number;
  totalBytes: number;
  knownTotal: boolean;
  allTotalsKnown?: boolean;
  percent: number;
  speedBytesPerSecond: number;
  etaSeconds: number;
  itemCount?: number;
  runningCount?: number;
  completedCount?: number;
  failedCount?: number;
};

export type ApiTaskResponse = ApiTask | {
  task: ApiTask;
  warning?: string;
};

export type ApiTaskCancelResponse = {
  task: ApiTask;
  cancelTask?: ApiTask;
  warning?: string;
};

export type ApiRestorePoint = {
  id: string;
  sourceClusterId: string;
  scopeType?: string;
  protectionPlanId?: string;
  appId?: string;
  storageRepoId?: string;
  taskCreatedAt?: string;
  veleroBackupName: string;
  pointType: string;
  status: string;
  sizeBytes?: number;
  completedAt?: string;
  expiresAt?: string;
  sourceNamespace?: string;
  backupStorageName?: string;
  metadata?: Record<string, any>;
  createdAt: string;
};

export type ApiRestorePointView = {
  id: string;
  sourceClusterId: string;
  protectionPlanId?: string;
  appId?: string;
  storageRepoId?: string;
  backupTaskId?: string;
  sourceNamespace: string;
  taskCreatedAt?: string;
  createdAt?: string;
  title: string;
  time: string;
  pointType: 'local' | 'remote';
  status: string;
  sizeBytes?: number;
  completedAt?: string;
  expiresAt?: string;
  backupStorageName?: string;
  veleroBackupName: string;
  includedNamespaces?: string[];
};
