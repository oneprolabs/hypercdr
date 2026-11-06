import type { ApiTask, ApiTaskEvent } from './types';

export type SyncVolume = {
  id: string;
  label: string;
  context: string;
  state: 'Waiting' | 'Scanning' | 'Syncing' | 'Completed' | 'Failed' | 'Canceled';
  started: boolean;
  bytesDone: number;
  totalBytes: number;
  knownTotal: boolean;
  percent?: number;
};

const nonnegative = (value: unknown): number => {
  const numeric = Number(value);
  return Number.isFinite(numeric) ? Math.max(0, numeric) : 0;
};

export function syncVolumeSummary(task: ApiTask, events: ApiTaskEvent[]) {
  const samples = events.filter(event => !event.taskId || event.taskId === task.id)
    .map(event => ({ at: Date.parse(event.createdAt) || 0, progress: event.payload?.velero?.volumeProgress }));
  if (task.payload?.volumeProgress) samples.push({ at: Date.parse(String(task.payload.lastStatusAt || task.completedAt || task.createdAt)) || 0, progress: task.payload.volumeProgress });
  samples.sort((left, right) => left.at - right.at);
  const progress = samples.filter(sample => sample.progress && typeof sample.progress === 'object').at(-1)?.progress;
  const items = Array.isArray(progress?.items) ? progress.items : [];
  const volumes: SyncVolume[] = items.filter((item: unknown) => item && typeof item === 'object').map((item: Record<string, unknown>, index: number) => {
    const bytesDone = nonnegative(item.bytesDone);
    const totalBytes = nonnegative(item.totalBytes);
    const phase = String(item.phase || '').toLowerCase();
    const started = Boolean(item.startedAt) || bytesDone > 0 || ['inprogress', 'completed'].includes(phase);
    const state: SyncVolume['state'] = phase === 'completed' ? 'Completed'
      : ['failed', 'failedvalidation', 'partiallyfailed'].includes(phase) ? 'Failed'
      : ['canceled', 'cancelled', 'canceling'].includes(phase) ? 'Canceled'
      : !started ? 'Waiting' : !item.knownTotal || totalBytes <= 0 ? 'Scanning' : 'Syncing';
    return {
      id: `${item.kind || 'volume'}/${item.name || index}`,
      label: item.volume ? [item.pod, item.volume].filter(Boolean).join(' / ') : String(item.name || `Volume ${index + 1}`),
      context: [item.namespace, item.pod, item.volume ? item.name : undefined].filter(Boolean).join(' / '),
      state, started, bytesDone, totalBytes, knownTotal: item.knownTotal === true && totalBytes > 0,
      percent: state === 'Syncing' ? Math.min(100, bytesDone * 100 / totalBytes) : undefined,
    };
  }).sort((left, right) => left.id.localeCompare(right.id));
  const count = (state: SyncVolume['state']) => volumes.filter(volume => volume.state === state).length;
  const completeScope = (sample: Record<string, unknown>) => sample.discoveryComplete === true && Array.isArray(sample.items) && Number(sample.discoveryCount) === sample.items.length;
  const coversCurrentScope = (sample: Record<string, unknown>) => {
    const scopeItems = Array.isArray(sample.items) ? sample.items : [];
    return volumes.every(volume => scopeItems.some((item: Record<string, unknown>, index: number) => item && `${item.kind || 'volume'}/${item.name || index}` === volume.id));
  };
  const ready = (sample: Record<string, unknown>) => completeScope(sample) && Array.isArray(sample.items) && sample.items.length > 0 && sample.items.every((item: Record<string, unknown>) => item && typeof item === 'object' && item.knownTotal === true && nonnegative(item.totalBytes) > 0);
  const progressStarted = samples.some(sample => sample.progress && ready(sample.progress) && coversCurrentScope(sample.progress));
  const discoveryComplete = samples.some(sample => sample.progress && completeScope(sample.progress) && coversCurrentScope(sample.progress));
  const totalsReady = Boolean(progress && ready(progress));
  const totalBytes = volumes.reduce((total, volume) => total + volume.totalBytes, 0);
  const bytesDone = progress ? nonnegative(progress.bytesDone) : nonnegative(task.payload?.progressMetrics?.syncedBytes);
  const observed = samples.filter(sample => sample.progress && typeof sample.progress === 'object');
  const timestamp = (at?: number) => at && at > 0 ? new Date(at).toISOString() : '';
  return {
    discoveryStartedAt: timestamp(observed[0]?.at),
    discoveryCompletedAt: timestamp(observed.find(sample => completeScope(sample.progress) && coversCurrentScope(sample.progress))?.at),
    transferStartedAt: timestamp(observed.find(sample => Number(sample.progress.bytesDone) > 0 || Array.isArray(sample.progress.items) && sample.progress.items.some((item: Record<string, unknown>) => item && (item.startedAt || ['InProgress', 'Completed'].includes(String(item.phase)))))?.at),
    transferCompletedAt: timestamp(observed.find(sample => Array.isArray(sample.progress.items) && sample.progress.items.length > 0 && sample.progress.items.every((item: Record<string, unknown>) => item?.phase === 'Completed'))?.at),
    lastObservedAt: timestamp(observed.at(-1)?.at),
    volumes,
    bytesDone, totalBytes, discoveryComplete, progressStarted,
    percent: totalsReady && totalBytes > 0 ? Math.min(100, bytesDone * 100 / totalBytes) : undefined,
    started: volumes.filter(volume => volume.started).length,
    waiting: count('Waiting'), scanning: count('Scanning'), syncing: count('Syncing'), completed: count('Completed'), failed: count('Failed'), canceled: count('Canceled'),
  };
}
