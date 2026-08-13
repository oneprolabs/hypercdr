import type { Cluster } from './types';
import type { ApiProtectionPlan } from '../recovery/types';

export type DRRelationshipStatus = 'healthy' | 'warning' | 'configuring' | 'failed';

export type DRRelationship = {
  id: string;
  sourceClusterId: string;
  targetClusterId: string;
  appIds: string[];
  appNames: string[];
  planIds: string[];
  status: DRRelationshipStatus;
};

export type DRClusterSummary = {
  protectedApps: number;
  outboundApps: number;
  inboundApps: number;
  outboundRelationships: number;
  inboundRelationships: number;
};

export type DRTopologyModel = {
  relationships: DRRelationship[];
  summaries: Record<string, DRClusterSummary>;
};

export function orderClustersForTopology(clusters: Cluster[], model: DRTopologyModel): Cluster[] {
  const originalIndex = new Map(clusters.map((cluster, index) => [cluster.id, index]));
  const flowScore = new Map(clusters.map(cluster => [cluster.id, 0]));
  for (const relationship of model.relationships) {
    const weight = Math.max(1, relationship.appIds.length);
    flowScore.set(relationship.sourceClusterId, (flowScore.get(relationship.sourceClusterId) || 0) + weight);
    flowScore.set(relationship.targetClusterId, (flowScore.get(relationship.targetClusterId) || 0) - weight);
  }
  return [...clusters].sort((left, right) => {
    const scoreDifference = (flowScore.get(right.id) || 0) - (flowScore.get(left.id) || 0);
    return scoreDifference || (originalIndex.get(left.id) || 0) - (originalIndex.get(right.id) || 0);
  });
}

const statusRank: Record<DRRelationshipStatus, number> = { healthy: 0, configuring: 1, warning: 2, failed: 3 };

function relationshipStatus(status?: string): DRRelationshipStatus | null {
  switch ((status || '').trim().toLowerCase()) {
    case 'ready': return 'healthy';
    case 'ready_with_warning': return 'warning';
    case 'configuring': return 'configuring';
    case 'configuration_failed':
    case 'cleanup_failed': return 'failed';
    case 'cleaning': return null;
    default: return 'configuring';
  }
}

export function buildDRTopology(clusters: Cluster[], plans: ApiProtectionPlan[]): DRTopologyModel {
  const clusterById = new Map(clusters.map(cluster => [cluster.id, cluster]));
  const summaries: Record<string, DRClusterSummary> = {};
  const outboundApps = new Map<string, Set<string>>();
  const inboundApps = new Map<string, Set<string>>();
  const grouped = new Map<string, DRRelationship>();

  for (const cluster of clusters) {
    summaries[cluster.id] = { protectedApps: 0, outboundApps: 0, inboundApps: 0, outboundRelationships: 0, inboundRelationships: 0 };
    outboundApps.set(cluster.id, new Set());
    inboundApps.set(cluster.id, new Set());
  }

  for (const plan of plans) {
    const sourceClusterId = plan.sourceClusterId || '';
    const targetClusterId = plan.targetClusterId || '';
    if (!sourceClusterId || !targetClusterId || sourceClusterId === targetClusterId || !clusterById.has(sourceClusterId) || !clusterById.has(targetClusterId)) continue;
    const status = relationshipStatus(plan.status);
    if (!status) continue;
    const sourceCluster = clusterById.get(sourceClusterId)!;
    const candidateIds = Array.from(new Set([plan.appId, ...(plan.appIds || [])].filter(Boolean)));
    const apps = candidateIds
      .map(appId => ({ appId, app: sourceCluster.apps.find(item => item.apiId === appId) }))
      .filter(item => Boolean(item.app));
    if (apps.length === 0) continue;
    const id = `${sourceClusterId}->${targetClusterId}`;
    const relationship = grouped.get(id) || { id, sourceClusterId, targetClusterId, appIds: [], appNames: [], planIds: [], status };
    const appIds = new Set(relationship.appIds);
    const appNames = new Set(relationship.appNames);
    for (const { appId, app } of apps) {
      appIds.add(appId);
      appNames.add(app?.namespace || app?.name || appId);
      outboundApps.get(sourceClusterId)?.add(appId);
      inboundApps.get(targetClusterId)?.add(`${sourceClusterId}:${appId}`);
    }
    relationship.appIds = [...appIds].sort();
    relationship.appNames = [...appNames].sort();
    relationship.planIds = Array.from(new Set([...relationship.planIds, plan.id]));
    if (statusRank[status] > statusRank[relationship.status]) relationship.status = status;
    grouped.set(id, relationship);
  }

  const relationships = [...grouped.values()].sort((a, b) => a.id.localeCompare(b.id));
  for (const relationship of relationships) {
    summaries[relationship.sourceClusterId].outboundRelationships++;
    summaries[relationship.targetClusterId].inboundRelationships++;
  }
  for (const cluster of clusters) {
    const outbound = outboundApps.get(cluster.id)?.size || 0;
    summaries[cluster.id].protectedApps = outbound;
    summaries[cluster.id].outboundApps = outbound;
    summaries[cluster.id].inboundApps = inboundApps.get(cluster.id)?.size || 0;
  }
  return { relationships, summaries };
}
