import type { ApiTask, ApiTaskEvent } from './types.ts';

type CacheStats = { version: number; indexEnabled: boolean; dataEnabled: boolean; indexHitBytes: number; indexReadBytes: number; dataHitBytes: number; dataReadBytes: number };
const validStats = (value: unknown): value is CacheStats => {
  if (!value || typeof value !== 'object') return false;
  const stats = value as CacheStats;
  return stats.version === 1 && typeof stats.indexEnabled === 'boolean' && typeof stats.dataEnabled === 'boolean'
    && [stats.indexHitBytes, stats.indexReadBytes, stats.dataHitBytes, stats.dataReadBytes].every(value => Number.isSafeInteger(value) && value >= 0)
    && stats.indexHitBytes <= stats.indexReadBytes && stats.dataHitBytes <= stats.dataReadBytes;
};

export function taskCacheHit(task: ApiTask, events: ApiTaskEvent[] = []) {
  const backup = task.type === 'backup';
  if (backup) {
    const label = 'Data reuse';
    const metrics = task.payload?.sizeMetricsV2;
    const logical = metrics?.logical;
    const newData = metrics?.newData;
    if (logical?.known === true && newData?.known === true && Number.isSafeInteger(logical.totalBytes) && logical.totalBytes > 0 && Number.isSafeInteger(newData.totalBytes) && newData.totalBytes >= 0) {
      return { label, value: `${(Math.max(0, 1 - newData.totalBytes / logical.totalBytes) * 100).toFixed(1)}%`, title: 'Reused data / total backup data, including resource metadata. Independent of index caching.' };
    }
    if (metrics?.reuse?.status === 'available' && typeof metrics.reuse.ratio === 'number' && Number.isFinite(metrics.reuse.ratio) && metrics.reuse.ratio >= 0 && metrics.reuse.ratio <= 1) {
      return { label, value: `${(metrics.reuse.ratio * 100).toFixed(1)}%`, title: 'Data reuse reported for this backup. Not index cache hit rate.' };
    }
    return { label, value: ['queued', 'pending', 'running', 'accepted', 'dispatched', 'syncing', 'finalizing'].includes(task.status) ? '0.0%' : 'Unavailable', title: 'Waiting for backup data reuse statistics.' };
  }
  const label = 'Data cache hit';
  const samples = events.filter(event => event.taskId === task.id).map(event => ({ at: event.createdAt, progress: event.payload?.velero?.volumeProgress }));
  const observation = task.payload?.backupObservation;
  if (observation?.details?.volumeProgress) samples.push({ at: observation.observedAt || task.completedAt || task.createdAt || '', progress: observation.details.volumeProgress });
  if (task.payload?.volumeProgress) samples.push({ at: String(task.payload.lastStatusAt || task.completedAt || task.createdAt || ''), progress: task.payload.volumeProgress });
  samples.sort((first, second) => Date.parse(first.at || '') - Date.parse(second.at || ''));
  const progress = samples.filter(sample => Array.isArray(sample.progress?.items) && sample.progress.items.length > 0 && sample.progress.items.every((item: Record<string, unknown>) => item && validStats(item.cacheStats))).at(-1)?.progress;
  const items: Record<string, unknown>[] = Array.isArray(progress?.items) ? progress.items : [];
  const unavailable = { label, value: 'Unavailable', title: 'This task has no complete cache statistics.' };
  if (!items.length) {
    if (['queued', 'pending', 'running', 'accepted', 'dispatched', 'syncing', 'finalizing'].includes(task.status)) return { label, value: '0.0%', title: 'Waiting for cache read statistics.' };
    return unavailable;
  }
  const stats = items.map(item => item.cacheStats as CacheStats);
  const enabled = stats.map(item => item.dataEnabled);
  if (enabled.every(value => !value)) return { label: 'Data cache', value: 'Disabled', title: 'Persistent cache was not active for this task.' };
  if (!enabled.every(Boolean)) return { label, value: 'Mixed', title: 'Cache was active for only some volumes. A task-wide hit rate is unavailable.' };
  const hit = stats.reduce((sum, item) => sum + item.dataHitBytes, 0);
  const total = stats.reduce((sum, item) => sum + item.dataReadBytes, 0);
  if (!Number.isSafeInteger(hit) || !Number.isSafeInteger(total)) return unavailable;
  return { label, value: `${(total > 0 ? hit * 100 / total : 0).toFixed(1)}%`, title: 'Cached logical file bytes / logical file bytes read during restore. Excludes indexes and metadata.' };
}
