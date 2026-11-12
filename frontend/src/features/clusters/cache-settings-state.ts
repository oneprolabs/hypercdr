type CacheSettings = { rootPath: string; indexPath: string; drillPath: string; indexEnabled: boolean; indexMaxGiB: number; drillEnabled: boolean; drillMaxGiB: number };
type CacheObservation = CacheSettings & { phase: string; observedAt?: string };
type CacheConfirmation = { status: string; completedAt?: string; payload?: Record<string, unknown> };

export function confirmedCacheObservation(settings: CacheSettings, observed: CacheObservation | null | undefined, task: CacheConfirmation | null): CacheObservation | null {
  if (!observed) return null;
  const completedAt = Date.parse(task?.completedAt || '');
  const observedAt = Date.parse(observed.observedAt || '');
  if (task?.status === 'succeeded' && Number.isFinite(completedAt)
    && (!Number.isFinite(observedAt) || completedAt >= observedAt)
    && Object.keys(settings).every(key => task.payload?.[key] === settings[key as keyof CacheSettings])) {
    return { ...settings, phase: 'applied', observedAt: task.completedAt };
  }
  return observed;
}
