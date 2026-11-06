import type { ApiTask } from './types';

export function preferTaskSnapshot(current: ApiTask | undefined, incoming: ApiTask): ApiTask {
  if (!current || current.id !== incoming.id) return incoming;
  const terminal = (task: ApiTask) => ['succeeded', 'failed', 'canceled', 'cancelled'].includes(task.status);
  if (terminal(current) && !terminal(incoming)) return current;
  const timestamp = (task: ApiTask) => Date.parse(String(task.payload?.lastStatusAt || task.completedAt || task.startedAt || task.createdAt)) || 0;
  return timestamp(current) > timestamp(incoming) ? current : incoming;
}

export function transferPercent(progress: Record<string, unknown>): number | undefined {
  const total = Number(progress.totalBytes);
  const done = Number(progress.bytesDone);
  if (!progress.knownTotal || !progress.allTotalsKnown || !Number.isFinite(total) || total <= 0 || !Number.isFinite(done) || done < 0) return undefined;
  return Math.max(0, Math.min(100, done * 100 / total));
}
