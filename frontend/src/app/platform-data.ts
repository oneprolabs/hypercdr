import type { AppItem, Cluster, ClusterStatus } from '../features/clusters/types';
import type { ApiApplication, ApiPolicy, ApiProtectionPlan, ApiRestorePointView, ApiTask, StorageRepo } from '../features/recovery/types';
import type { ApiCluster, ApiStorageRepo } from '../features/recovery/platform-types';
import { isActiveTaskStatus } from '../features/recovery/task-status';
import { selectPointedPlanTask } from '../features/recovery/plan-task-selection';
import { mapApplicationStatus, stageOfApp } from '../features/recovery/task-ui';
import { namespacesFromPayload, taskRestorePointId } from '../features/applications/application-support';
import { restorePointNamespaces } from '../features/restore-points/restore-point-support';

export function mapClusterStatus(status: string, connectionStatus: string): ClusterStatus {
  if (connectionStatus === 'online' && status !== 'warning') return 'healthy';
  if (status === 'syncing') return 'syncing';
  return 'warning';
}

export function mapRestorePoint(raw: any): ApiRestorePointView {
  const ns: string = raw?.sourceNamespace || raw?.metadata?.sourceNamespace || '';
  const includedNamespaces = namespacesFromPayload({ ...raw?.metadata, sourceNamespace: ns });
  const storageName: string = raw?.backupStorageName || raw?.metadata?.backupStorageName || 'default';
  const time: string = raw?.completedAt || raw?.startedAt || raw?.createdAt || '';
  const pointType: 'local' | 'remote' = (raw?.pointType || 'remote').toLowerCase().includes('local') ? 'local' : 'remote';
  return {
    id: raw?.id,
    sourceClusterId: raw?.sourceClusterId,
    protectionPlanId: raw?.protectionPlanId,
    appId: raw?.appId,
    storageRepoId: raw?.storageRepoId,
    backupTaskId: raw?.backupTaskId || raw?.metadata?.backupTaskId || '',
    sourceNamespace: ns,
    taskCreatedAt: raw?.taskCreatedAt || '',
    createdAt: raw?.createdAt || '',
    title: `${storageName} · ${raw?.veleroBackupName || raw?.id?.slice(0, 8) || 'restore point'}`,
    time,
    pointType,
    status: raw?.status || 'unknown',
    sizeBytes: raw?.sizeBytes,
    completedAt: raw?.completedAt,
    expiresAt: raw?.expiresAt,
    backupStorageName: raw?.backupStorageName || raw?.metadata?.backupStorageName || '',
    veleroBackupName: raw?.veleroBackupName || '',
    includedNamespaces,
	metadata: raw?.metadata || {},
	sizeMetricsV2: raw?.sizeMetricsV2 || raw?.metadata?.sizeMetricsV2,
  };
}

export function taskPlanId(task: ApiTask): string {
  return task.protectionPlanId || String(task.payload?.protectionPlanId || '');
}

export function recoveryTaskMatchesApp(task: ApiTask, app: ApiApplication, plans: ApiProtectionPlan[], restorePoints: ApiRestorePointView[]): boolean {
  const appPlan = plans.find(item => planIncludesApp(item, app.id));
  const planID = taskPlanId(task);
  const restorePointID = taskRestorePointId(task);
  const namespace = app.namespace || app.name;
  if (!appPlan?.id || !planID) return false;
  if (planID !== appPlan.id) return false;
  const taskNamespaces = namespacesFromPayload(task.payload);
  // Plan ID is the authoritative ownership key. Namespace/restore-point data
  // may be omitted by the summary endpoint, so use those fields only when
  // they are present to disambiguate multi-namespace plans.
  if (taskNamespaces.length > 0 && namespace && !taskNamespaces.includes(namespace)) return false;
  if (!restorePointID) return true;
  const point = restorePoints.find(item => item.id === restorePointID);
  if (point) {
    if (point.protectionPlanId && point.protectionPlanId !== appPlan.id) return false;
    const pointNamespaces = restorePointNamespaces(point);
    return pointNamespaces.length === 0 || !namespace || pointNamespaces.includes(namespace);
  }
  return true;
}

export function buildAppTaskMap(tasks: ApiTask[], apps: ApiApplication[], taskTypes?: string[], restorePoints: ApiRestorePointView[] = [], plans: ApiProtectionPlan[] = []): Record<string, ApiTask> {
  const byNamespace: Record<string, ApiTask> = {};
  const sorted = [...tasks].sort((a, b) => {
    // The newest task is authoritative. Preferring active status first made an
    // older task replace a newer terminal task during refresh, causing status
    // text and details to jump between operations.
    return (b.createdAt || '').localeCompare(a.createdAt || '') || String(b.id || '').localeCompare(String(a.id || ''));
  });
  const allowedTypes = taskTypes ? new Set(taskTypes) : null;
  for (const app of apps) {
    const appPlan = plans.find(item => planIncludesApp(item, app.id));
    if (!appPlan?.id) continue;
    const recovery = Boolean(taskTypes?.some(type => ['restore', 'drill', 'takeover'].includes(type)));
    const pointedTaskID = recovery ? appPlan.latestRecoveryTaskId : appPlan.latestSyncTaskId;
    const eligible = (t: ApiTask) => {
      if (allowedTypes && !allowedTypes.has(t.type)) return false;
      if (t.payload?.archivedClusterId || t.payload?.archivedAppId || t.payload?.archivedProtectionPlanId) return false;
      if (['restore', 'drill', 'takeover'].includes(t.type)) return recoveryTaskMatchesApp(t, app, plans, restorePoints);
      if (taskPlanId(t) !== appPlan.id) return false;
      if (!t.clusterId || t.clusterId !== app.clusterId) return false;
      const taskNamespaces = namespacesFromPayload(t.payload);
      return taskNamespaces.length === 0 || taskNamespaces.includes(app.namespace);
    };
    // A persisted plan pointer is authoritative. Time ordering is retained
    // only for plans created before the pointer migration.
    const match = selectPointedPlanTask((pointedTaskID ? tasks : sorted).filter(eligible), pointedTaskID);
    if (match) byNamespace[app.namespace] = match;
  }
  return byNamespace;
}

// Task summaries can briefly lag immediately after submission. Do not replace
// an already rendered active task with an empty/older snapshot during that
// consistency window; otherwise the row falls back to Last snapshot and then
// jumps back to the progress state on the next poll.
export function mergeTaskMapKeepingActive(previous: Record<string, ApiTask>, next: Record<string, ApiTask>): Record<string, ApiTask> {
  const merged = { ...next };
  Object.entries(previous).forEach(([key, previousTask]) => {
    const nextTask = merged[key];
    if (!nextTask && isActiveTaskStatus(previousTask.status)) {
      merged[key] = previousTask;
      return;
    }
    if (nextTask && isActiveTaskStatus(previousTask.status)) {
      const previousCreated = String(previousTask.createdAt || '');
      const nextCreated = String(nextTask.createdAt || '');
      if (nextCreated < previousCreated) merged[key] = previousTask;
    }
  });
  return merged;
}

export function planIncludesApp(plan: ApiProtectionPlan, appId: string): boolean {
  return plan.appId === appId || Boolean(plan.appIds?.includes(appId));
}

export function mapApps(apps: ApiApplication[], plans: ApiProtectionPlan[], policies: ApiPolicy[], storage: ApiStorageRepo[], clusters: ApiCluster[]): AppItem[] {
  return apps.map(app => {
    const plan = plans.find(item => planIncludesApp(item, app.id));
    const protectedByState = app.protectionStatus === 'protected';
    const isProtected = Boolean(plan) || protectedByState;
    const policy = policies.find(item => item.id === plan?.policyId);
    const repo = storage.find(item => item.id === plan?.storageRepoId);
    const target = clusters.find(item => item.id === plan?.targetClusterId);
    return {
      apiId: app.id,
      clusterId: app.clusterId,
      name: app.namespace || app.name,
      namespace: app.namespace || app.name,
      status: mapApplicationStatus(app.status, isProtected),
      namespaceStatus: app.status || 'unknown',
      workloadCount: app.workloadCount || 0,
      serviceCount: app.serviceCount || 0,
      ingressCount: app.ingressCount || 0,
      configMapCount: app.configMapCount || 0,
      secretCount: app.secretCount || 0,
      pvcCount: app.pvcCount || 0,
      pvCapacityBytes: app.pvCapacityBytes || 0,
      resourceSummary: app.resourceSummary,
      labels: app.labels || {},
      protectionStatus: app.protectionStatus,
      protectionPlanId: plan?.id,
      protectionPlanCreatedAt: plan?.createdAt,
      stage: stageOfApp(app.protectionStatus, isProtected),
      policy: policy?.name,
      storage: repo?.name,
      targetCluster: target?.name,
      isProtected,
      lastBackup: isProtected ? 'synced recently' : undefined,
      tags: app.tags || [],
    };
  });
}

export function mapCluster(cluster: ApiCluster, apps: AppItem[] = []): Cluster {
  const appCount = cluster.applicationCount || cluster.namespaceCount || apps.length;
  return {
    id: cluster.id,
    name: cluster.name || 'unknown-cluster',
    clusterType: cluster.clusterType,
    cloudProvider: cluster.cloudProvider,
    cloudRegion: cluster.cloudRegion,
    cloudClusterId: cluster.cloudClusterId,
    region: cluster.connectionStatus === 'online' ? 'connected' : 'disconnected',
    version: cluster.kubeVersion || 'unknown',
    status: mapClusterStatus(cluster.status, cluster.connectionStatus),
    connectionStatus: cluster.connectionStatus || 'unknown',
    compliance: cluster.complianceScore ?? 0,
    nodes: cluster.nodeCount,
    nodeDetails: cluster.nodes || [],
    storageClasses: cluster.storageClasses || [],
    apiResources: cluster.apiResources || [],
    namespaceApis: cluster.namespaceAPIs || [],
    namespaces: cluster.namespaceCount || appCount,
    applications: appCount,
    agentVersion: cluster.agentVersion || 'pending',
    latestAgentVersion: cluster.latestAgentVersion || cluster.agentVersion || 'pending',
    agentImage: cluster.agentImage,
    agentImageDigest: cluster.agentImageDigest,
    latestAgentImage: cluster.latestAgentImage,
    latestAgentImageDigest: cluster.latestAgentImageDigest,
    agentUpgradeAvailable: Boolean(cluster.agentUpgradeAvailable),
    agentUpgradeStatus: cluster.agentUpgradeStatus,
    agentUpgradeProgress: cluster.agentUpgradeProgress,
    veleroVersion: cluster.veleroVersion || 'unknown',
    veleroStatus: cluster.veleroStatus || 'unknown',
    veleroImage: cluster.veleroImage,
    veleroImageDigest: cluster.veleroImageDigest,
    veleroServerReady: cluster.veleroServerReady,
    veleroNodeAgentDesired: cluster.veleroNodeAgentDesired,
    veleroNodeAgentReady: cluster.veleroNodeAgentReady,
    veleroNodeAgentImageDigest: cluster.veleroNodeAgentImageDigest,
    latestVeleroVersion: cluster.latestVeleroVersion,
    latestVeleroImage: cluster.latestVeleroImage,
    latestVeleroImageDigest: cluster.latestVeleroImageDigest,
    veleroUpgradeAvailable: Boolean(cluster.veleroUpgradeAvailable),
    veleroUpgradeStatus: cluster.veleroUpgradeStatus,
    veleroUpgradeProgress: cluster.veleroUpgradeProgress,
    lastSeenAt: cluster.lastSeenAt,
    role: cluster.role || 'both',
    isDefault: Boolean(cluster.isDefault),
    apps,
  };
}

export function mapStorageRepo(repo: ApiStorageRepo): StorageRepo {
  const raw = (repo.status || '').toLowerCase();
  const status: StorageRepo['status'] = ['connected', 'ready', 'active'].includes(raw)
    ? 'connected'
    : raw === 'warning'
      ? 'warning'
      : 'unknown';
  const cfg = (repo.config || {}) as Record<string, unknown>;
  const urlStyle = typeof cfg.urlStyle === 'string' ? (cfg.urlStyle as string) : 'path';
  const lastValidatedAt = repo.lastValidatedAt && new Date(repo.lastValidatedAt).getUTCFullYear() > 1 ? repo.lastValidatedAt : undefined;
  return {
    id: repo.id,
    name: repo.name,
    type: repo.type || 'S3',
    endpoint: repo.endpoint || '',
    bucket: repo.bucket || '',
    // Keep display placeholders out of editable/API state.
    region: repo.region || '',
    useTls: repo.tlsEnabled,
    status,
    updatedAt: repo.updatedAt || repo.createdAt || '',
    lastValidatedAt,
    urlStyle,
  };
}

