import { namespacesFromPayload } from '../applications/application-support';
import { isActiveTaskStatus, isFailedStatus, isSucceededStatus } from '../recovery/task-status';
import type { ApiRestorePoint, ApiTask } from '../recovery/types';

export function restorePointIsScheduled(point: Pick<ApiRestorePoint, 'metadata'>) {
  const scheduled = point.metadata?.scheduled;
  return scheduled === true || scheduled === 'true';
}

export function restorePointListStatus(point: ApiRestorePoint) {
  const retentionState = typeof point.metadata?.retentionState === 'string' ? point.metadata.retentionState : '';
  if (retentionState === 'deleting' || retentionState === 'pending_delete') return 'deleting';
  if (retentionState === 'delete_failed') return 'delete_failed';
  return point.status || 'available';
}

export function restorePointNamespaces(point: { sourceNamespace?: string; includedNamespaces?: string[]; metadata?: Record<string, any> }): string[] {
  const includedNamespaces = 'includedNamespaces' in point && Array.isArray(point.includedNamespaces) ? point.includedNamespaces : [];
  if (includedNamespaces.length) return includedNamespaces;
  const metadataNamespaces = namespacesFromPayload({ ...point.metadata, sourceNamespace: point.sourceNamespace || point.metadata?.sourceNamespace });
  return metadataNamespaces.length ? metadataNamespaces : [point.sourceNamespace].filter(Boolean);
}

export function taskMatchesRestorePoint(task: ApiTask, restorePointId: string): boolean {
  return task.restorePointId === restorePointId
    || String(task.payload?.restorePointId || '') === restorePointId
    || String(task.payload?.archivedRestorePointId || '') === restorePointId;
}

export function latestTaskForRestorePoint(tasks: ApiTask[], restorePointId: string): ApiTask | undefined {
  return [...tasks]
    .filter(item => taskMatchesRestorePoint(item, restorePointId) && ['restore', 'drill', 'takeover'].includes(item.type))
    .sort((a, b) => (b.createdAt || '').localeCompare(a.createdAt || ''))[0];
}

export function taskStatusLabel(status?: string) {
  if (isSucceededStatus(status)) return 'Succeeded';
  if (isFailedStatus(status)) return 'Failed';
  if (isActiveTaskStatus(status)) return 'Running';
  return status || 'Unknown';
}
