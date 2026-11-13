import type { ApiTask } from '../recovery/types.ts';

const active = (task: ApiTask) => ['queued', 'dispatched', 'accepted', 'running', 'syncing', 'finalizing', 'canceling'].includes(task.status);

export function drillNamespaces(task: ApiTask): string[] {
  const values = [task.payload?.targetNamespace, ...Object.values(task.payload?.targetNamespaces || {})];
  return [...new Set(values.filter((value): value is string => typeof value === 'string' && Boolean(value.trim())).map(value => value.trim()))].sort();
}

export type DrillCleanupCandidate = { task: ApiTask; namespaces: string[]; blockedReason: string };

export function drillCleanupCandidates(planIds: string[], tasks: ApiTask[]): DrillCleanupCandidate[] {
  const ordered = [...new Map(tasks.map(task => [task.id, task])).values()].sort((first, second) => second.createdAt.localeCompare(first.createdAt) || second.id.localeCompare(first.id));
  const seen = new Set<string>();
  const candidates: DrillCleanupCandidate[] = [];
  for (const task of ordered) {
    if (task.type !== 'drill' || !planIds.includes(task.protectionPlanId || '')) continue;
    const namespaces = drillNamespaces(task).filter(namespace => !seen.has(`${task.clusterId}:${namespace}`));
    if (!namespaces.length) continue;
    const keys = namespaces.map(namespace => `${task.clusterId}:${namespace}`);
    keys.forEach(key => seen.add(key));
    const related = ordered.filter(item => item.clusterId === task.clusterId && item.type === 'protection-cleanup' && item.payload?.cleanupMode === 'drill' && (
      item.payload?.drillTaskId === task.id || (item.createdAt >= task.createdAt && namespaces.some(namespace => (item.payload?.drillNamespaces || []).includes(namespace)))
    ));
    const remainingNamespaces = namespaces.filter(namespace => !related.some(item => item.status === 'succeeded' && (item.payload?.drillNamespaces || drillNamespaces(task)).includes(namespace)));
    if (!remainingNamespaces.length) continue;
    const busy = ordered.some(item => item.clusterId === task.clusterId && active(item) && ['drill', 'restore', 'takeover'].includes(item.type) && drillNamespaces(item).some(namespace => namespaces.includes(namespace)));
    const replaced = ordered.some(item => item.clusterId === task.clusterId && item.createdAt > task.createdAt && ['restore', 'takeover'].includes(item.type) && drillNamespaces(item).some(namespace => remainingNamespaces.includes(namespace)));
    candidates.push({ task, namespaces: remainingNamespaces, blockedReason: related.some(active) ? 'Cleaning' : busy ? 'Recovery in progress' : replaced ? 'Replaced by a newer recovery' : '' });
  }
  return candidates;
}
